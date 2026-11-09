// Package bmstuvpn — клиент OpenVPN МГТУ внутри приложения.
//
// На Raspberry Pi bmstu.ru шёл через отдельный интерфейс bmstu0 (служба OpenVPN).
// На Android второй VPN поднять нельзя, поэтому протокол OpenVPN работает в памяти
// приложения (minivpn), а поверх туннеля — собственный стек TCP/IP (gVisor netstack).
// Xray отправляет в туннель соединения к bmstu.ru через локальный SOCKS5-прокси.
package bmstuvpn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ooni/minivpn/pkg/config"
	"github.com/ooni/minivpn/pkg/tunnel"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Пакет keepalive OpenVPN (ping): без него сервер разрывает простаивающий туннель.
var pingMagic = []byte{0x2a, 0x18, 0x7b, 0xf3, 0x64, 0x1e, 0xb4, 0xcb, 0x07, 0xed, 0x2d, 0x0a, 0x98, 0x1f, 0xc7, 0x48}

// Сообщение OCC EXIT (occ_magic + OCC_EXIT=6, src/openvpn/occ.c): клиент уходит, сервер сразу
// закрывает сессию. Иначе он держит её до своего ping-restart, а сервер МГТУ разрешает одну сессию
// на учётную запись — повторный вход в это время получает AUTH_FAILED.
var occExit = []byte{0x28, 0x7f, 0x34, 0x6b, 0xd4, 0xef, 0x7a, 0x81, 0x2d, 0x56, 0xb8, 0xd3, 0xaf, 0xc5, 0x45, 0x9c, 6}

// writeWithTimeout пишет пакет в туннель, не дольше d. Крайний срок записи у TUN не ставится:
// он действовал бы и на обычный трафик.
func writeWithTimeout(vtun *tunnel.TUN, pkt []byte, d time.Duration) error {
	done := make(chan error, 1)
	go func() {
		_, err := vtun.Write(pkt)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return errors.New("запись в туннель не прошла за " + d.String())
	}
}

const (
	handshakeTimeout = 40 * time.Second
	pingInterval     = 10 * time.Second
	probeInterval    = 60 * time.Second
	probeTimeout     = 15 * time.Second
	probeMaxFailures = 3
	// С запасом под заголовки OpenVPN: в .ovpn университета mssfix 1400
	innerMTU = 1360
)

// Listener получает событие о падении туннеля (реализуется на Java).
type Listener interface {
	OnDown(reason string)
}

// Tunnel — один сеанс OpenVPN. После Stop или падения создаётся новый.
type Tunnel struct {
	listener Listener
	log      *ringLog

	mu      sync.Mutex
	cancel  context.CancelFunc
	vtun    *tunnel.TUN
	tnet    *netstack.Net
	closers []func()
	up      bool
	down    atomic.Bool
	started bool

	// Байты IP-пакетов через туннель (для статистики, как в OpenVPN Connect)
	bytesIn  atomic.Int64
	bytesOut atomic.Int64
}

// BytesIn — получено из туннеля, байт.
func (t *Tunnel) BytesIn() int64 { return t.bytesIn.Load() }

// BytesOut — отправлено в туннель, байт.
func (t *Tunnel) BytesOut() int64 { return t.bytesOut.Load() }

// NewTunnel создаёт туннель. listener может быть nil.
func NewTunnel(listener Listener) *Tunnel {
	return &Tunnel{listener: listener, log: newRingLog(200)}
}

// Start подключается к серверу и ждёт завершения рукопожатия (до 40 секунд).
//
//	ovpn      — текст .ovpn (используются remote/port/proto/cipher/auth/compress и <ca>)
//	remoteIP  — IP сервера OpenVPN (имя разрешает приложение)
//	probeAddr — "ip:порт" сайта за туннелем для проверки связи ("" — без проверки)
//	proxy     — прокси, который после подъёма туннеля начнёт вести соединения через него
func (t *Tunnel) Start(ovpn, username, password, remoteIP, probeAddr string, proxy *Proxy) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return errors.New("туннель уже запускался")
	}
	t.started = true
	if proxy == nil {
		return errors.New("не передан прокси")
	}

	opts, err := parseOptions(ovpn, username, password, remoteIP)
	if err != nil {
		return err
	}
	t.log.Infof("подключение к %s:%s/%s, шифр %s", opts.Remote, opts.Port, opts.Proto, opts.Cipher)

	cfg := config.NewConfig(config.WithOpenVPNOptions(opts), config.WithLogger(t.log))
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	hsCtx, hsCancel := context.WithTimeout(ctx, handshakeTimeout)
	defer hsCancel()
	vtun, err := tunnel.Start(hsCtx, &net.Dialer{Timeout: 15 * time.Second, Control: protectControl}, cfg)
	if err != nil {
		cancel()
		if strings.Contains(err.Error(), "AUTH_FAILED") {
			return err // причина от сервера уже в тексте ошибки
		}
		return fmt.Errorf("%v%s", err, t.log.authHint())
	}
	t.vtun = vtun
	t.closers = append(t.closers, func() { _ = vtun.Close() })

	local, err := netip.ParseAddr(vtun.LocalAddr().String())
	if err != nil {
		t.closeLocked()
		return fmt.Errorf("сервер не выдал адрес в туннеле: %q", vtun.LocalAddr().String())
	}
	dev, tnet, err := netstack.CreateNetTUN([]netip.Addr{local}, nil, innerMTU)
	if err != nil {
		t.closeLocked()
		return fmt.Errorf("netstack: %w", err)
	}
	t.tnet = tnet
	t.closers = append(t.closers, func() { _ = dev.Close() })

	// Пакеты: туннель OpenVPN -> netstack
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := vtun.Read(buf)
			if err != nil {
				t.fail("туннель закрыт: " + err.Error())
				return
			}
			t.bytesIn.Add(int64(n))
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			if _, err := dev.Write([][]byte{pkt}, 0); err != nil && ctx.Err() == nil {
				t.log.Warnf("netstack write: %v", err)
			}
		}
	}()
	// Пакеты: netstack -> туннель OpenVPN
	go func() {
		batch := dev.BatchSize()
		bufs := make([][]byte, batch)
		for i := range bufs {
			bufs[i] = make([]byte, 65535)
		}
		sizes := make([]int, batch)
		for {
			n, err := dev.Read(bufs, sizes, 0)
			if err != nil {
				if ctx.Err() == nil {
					t.fail("netstack: " + err.Error())
				}
				return
			}
			for i := 0; i < n; i++ {
				pkt := make([]byte, sizes[i])
				copy(pkt, bufs[i][:sizes[i]])
				t.bytesOut.Add(int64(sizes[i]))
				if _, err := vtun.Write(pkt); err != nil {
					t.fail("запись в туннель: " + err.Error())
					return
				}
			}
		}
	}()
	go t.keepalive(ctx, vtun)
	if probeAddr != "" {
		go t.probe(ctx, probeAddr)
	}

	proxy.s.attach(tnet)
	t.closers = append(t.closers, func() { proxy.s.detach(tnet) })
	t.up = true
	t.log.Infof("туннель поднят: адрес %s", local)
	return nil
}

func (t *Tunnel) keepalive(ctx context.Context, vtun *tunnel.TUN) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ping := make([]byte, len(pingMagic))
			copy(ping, pingMagic)
			if err := writeWithTimeout(vtun, ping, 5*time.Second); err != nil && ctx.Err() == nil {
				t.fail("keepalive: " + err.Error())
				return
			}
		}
	}
}

// probe проверяет связь через туннель: minivpn не сообщает о падении сеанса (например, при смене
// ключей через reneg-sec), поэтому соединение к сайту университета — признак, что туннель жив.
func (t *Tunnel) probe(ctx context.Context, addr string) {
	failures := 0
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		conn, err := t.tnet.DialContext(pctx, "tcp", addr)
		cancel()
		if err == nil {
			_ = conn.Close()
			failures = 0
			continue
		}
		if ctx.Err() != nil {
			return
		}
		failures++
		t.log.Warnf("проверка %s через туннель не прошла (%d/%d): %v", addr, failures, probeMaxFailures, err)
		if failures >= probeMaxFailures {
			t.fail("нет ответа через туннель")
			return
		}
	}
}

// fail фиксирует падение туннеля и один раз сообщает о нём.
func (t *Tunnel) fail(reason string) {
	if !t.down.CompareAndSwap(false, true) {
		return
	}
	t.log.Warnf("туннель упал: %s", reason)
	go func() {
		t.Stop()
		if t.listener != nil {
			t.listener.OnDown(reason)
		}
	}()
}

// Stop закрывает туннель и прокси. Повторный вызов безопасен.
func (t *Tunnel) Stop() {
	t.down.Store(true)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sendExitLocked()
	t.closeLocked()
}

// sendExitLocked сообщает серверу о выходе (OCC EXIT), если туннель был поднят.
func (t *Tunnel) sendExitLocked() {
	if !t.up || t.vtun == nil {
		return
	}
	t.up = false
	for i := 0; i < 2; i++ { // UDP: пакет может потеряться, отправляем дважды
		msg := make([]byte, len(occExit))
		copy(msg, occExit)
		if err := writeWithTimeout(t.vtun, msg, 300*time.Millisecond); err != nil {
			return
		}
	}
	time.Sleep(150 * time.Millisecond) // дать пакетам уйти до закрытия сокета
	t.log.Infof("серверу отправлено уведомление о выходе")
}

func (t *Tunnel) closeLocked() {
	if t.cancel != nil {
		t.cancel()
	}
	for i := len(t.closers) - 1; i >= 0; i-- {
		t.closers[i]()
	}
	t.closers = nil
}

// IsAlive — туннель поднят и не падал.
func (t *Tunnel) IsAlive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.up && !t.down.Load()
}

// LocalIP — адрес, выданный сервером в туннеле.
func (t *Tunnel) LocalIP() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vtun == nil {
		return ""
	}
	return t.vtun.LocalAddr().String()
}

// Log — последние строки журнала туннеля (для диагностики).
func (t *Tunnel) Log() string {
	return t.log.String()
}

// IsAuthError — ошибка означает неверный логин или пароль.
func IsAuthError(message string) bool {
	return strings.Contains(message, "AUTH_FAILED") || strings.Contains(message, "неверный логин")
}
