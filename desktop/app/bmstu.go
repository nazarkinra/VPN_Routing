package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"vpnrouting/desktop/bmstuvpn"
	"vpnrouting/desktop/core"
	"vpnrouting/desktop/netenv"
)

// Туннель OpenVPN МГТУ (аналог BmstuManager.java, службы bmstu-vpn и bmstu-vpn-hook.sh на Pi).
//
// Xray отправляет bmstu.ru в локальный SOCKS-прокси. Прокси работает всё время, пока включён VPN:
// при поднятом туннеле соединения идут через OpenVPN, иначе напрямую. Поэтому подъём и падение
// туннеля не требуют перезапуска Xray.

const (
	probeHost = "bmstu.ru"
	// После отказа в доступе — повтор не раньше чем через 5 минут, чтобы не заблокировали учётную запись.
	retryAuth = 5 * time.Minute
	retryMin  = 30 * time.Second
	retryMax  = 5 * time.Minute
	// Сервер МГТУ допускает одну сессию на учётную запись и держит упавшую сессию до своего таймаута.
	reconnectAfterDown = 30 * time.Second
	retryAfterDrop     = 90 * time.Second
	dropWindow         = 3 * time.Minute
)

type BmstuState string

const (
	BmstuOff        BmstuState = "off"
	BmstuConnecting BmstuState = "connecting"
	BmstuUp         BmstuState = "up"
	BmstuWaiting    BmstuState = "waiting"
	// BmstuError — VPN МГТУ не включился (ошибка запуска ядра, нет сети); туннель не работает.
	BmstuError BmstuState = "error"
)

// BmstuStatus — состояние туннеля для интерфейса.
type BmstuStatus struct {
	State   BmstuState `json:"state"`
	Message string     `json:"message"`
	// UpSince — момент подъёма туннеля, мс Unix; 0 — не поднят.
	UpSince  int64  `json:"upSince"`
	BytesIn  int64  `json:"bytesIn"`
	BytesOut int64  `json:"bytesOut"`
	LocalIP  string `json:"localIp"`
}

// Bmstu — менеджер туннеля. Действия выполняются по очереди в своём потоке.
type Bmstu struct {
	store *Store
	work  chan func()
	quit  chan struct{}

	mu         sync.Mutex
	state      BmstuState
	message    string
	current    *bmstuvpn.Tunnel
	lastTunnel *bmstuvpn.Tunnel
	upSince    time.Time
	proxy      *bmstuvpn.Proxy

	// Только в рабочем потоке
	tunnel       *bmstuvpn.Tunnel
	pending      *time.Timer
	pendingGen   int
	backoff      time.Duration
	active       bool
	lastDropAt   time.Time
	authFailures int
}

func newBmstu(store *Store) *Bmstu {
	b := &Bmstu{store: store, work: make(chan func(), 64), quit: make(chan struct{}),
		state: BmstuOff, backoff: retryMin}
	// Сокеты клиента OpenVPN и прямой выход прокси — мимо туннеля, через физическую сеть
	bmstuvpn.SetDialControl(netenv.BindControl)
	go func() {
		for {
			select {
			case f := <-b.work:
				f()
			case <-b.quit:
				return
			}
		}
	}()
	return b
}

func (b *Bmstu) run(f func()) {
	select {
	case b.work <- f:
	case <-b.quit:
	}
}

func (b *Bmstu) setState(s BmstuState, msg string) {
	b.mu.Lock()
	b.state, b.message = s, msg
	b.mu.Unlock()
	logf("bmstu %s: %s", s, msg)
}

// reportError — VPN МГТУ не включился: показать причину на вкладке МГТУ.
func (b *Bmstu) reportError(msg string) {
	done := make(chan struct{})
	b.run(func() {
		b.setState(BmstuError, msg)
		close(done)
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// Status — снимок для экрана (статистика как в OpenVPN Connect).
func (b *Bmstu) Status() BmstuStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := BmstuStatus{State: b.state, Message: b.message}
	if t := b.current; t != nil {
		st.BytesIn, st.BytesOut, st.LocalIP = t.BytesIn(), t.BytesOut(), t.LocalIP()
		if !b.upSince.IsZero() {
			st.UpSince = b.upSince.UnixMilli()
		}
	}
	return st
}

// Log — журнал текущего или последнего туннеля.
func (b *Bmstu) Log() string {
	b.mu.Lock()
	t := b.current
	if t == nil {
		t = b.lastTunnel
	}
	b.mu.Unlock()
	if t == nil {
		return ""
	}
	return t.Log()
}

// EnsureProxy — прокси для конфига Xray: порт и случайные логин/пароль. Один на время работы VPN.
func (b *Bmstu) EnsureProxy() (*bmstuvpn.Proxy, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.proxy == nil {
		p, err := bmstuvpn.NewProxy()
		if err != nil {
			return nil, err
		}
		b.proxy = p
	}
	return b.proxy, nil
}

// Sync приводит туннель в соответствие с настройками.
func (b *Bmstu) Sync() {
	b.run(func() {
		b.active = true
		if !b.store.BmstuEnabled() {
			b.stopTunnel()
			b.setState(BmstuOff, "Выключен")
		} else if b.tunnel == nil && b.pending == nil {
			b.connect()
		}
	})
}

// Restart — переподключиться (сменилась сеть, логин или пароль).
func (b *Bmstu) Restart() {
	b.run(func() {
		b.active = true
		b.stopTunnel()
		b.backoff = retryMin
		b.connect()
	})
}

// ApplyOvpn устанавливает новый .ovpn. Если туннель с ним не поднимется — возвращает прежний.
func (b *Bmstu) ApplyOvpn(text string) {
	b.run(func() {
		b.store.InstallBmstuOvpn(text)
		if !b.active || !b.store.BmstuEnabled() || !b.store.HasBmstuCredentials() {
			b.mu.Lock()
			s := b.state
			b.mu.Unlock()
			b.setState(s, "Новый файл .ovpn сохранён")
			return
		}
		b.stopTunnel()
		errText := b.tryConnect()
		if errText == "" {
			return
		}
		b.store.RestoreBmstuOvpn()
		auth := bmstuvpn.IsAuthError(errText)
		delay := retryMin
		reason := errText
		if auth {
			delay = retryAuth
			reason = lowerFirst(authText(errText))
		}
		b.setState(BmstuWaiting, "Новый файл .ovpn не подошёл: "+reason+
			". Возвращён прежний, повтор через "+formatDelay(delay)+".")
		b.schedule(delay)
	})
}

// StopAll останавливает туннель и прокси (VPN выключается).
func (b *Bmstu) StopAll() {
	// Прокси закрывается сразу, а не в очереди: иначе при быстром перезапуске VPN
	// запоздавшая задача закрыла бы уже новый прокси, указанный в конфиге Xray
	b.mu.Lock()
	if b.proxy != nil {
		b.proxy.Close()
		b.proxy = nil
	}
	b.mu.Unlock()
	done := make(chan struct{})
	b.run(func() {
		b.active = false
		b.stopTunnel()
		b.setState(BmstuOff, "Выключен")
		close(done)
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (b *Bmstu) shutdown() {
	b.StopAll()
	close(b.quit)
}

// ---------- В рабочем потоке ----------

func (b *Bmstu) connect() {
	b.cancelPending()
	if !b.active {
		return
	}
	switch {
	case !b.store.BmstuEnabled():
		b.setState(BmstuOff, "Выключен")
		return
	case !b.store.HasBmstuOvpn():
		b.setState(BmstuOff, "Импортируйте файл профиля .ovpn")
		return
	case !b.store.HasBmstuCredentials():
		b.setState(BmstuOff, "Укажите логин и пароль")
		return
	}
	errText := b.tryConnect()
	if errText == "" {
		return
	}
	auth := bmstuvpn.IsAuthError(errText)
	// Первый отказ подряд — скорее всего, сервер ещё держит прежнюю сессию: один повтор через 90 с.
	// Дальше — раз в 5 минут, как на Pi, чтобы не заблокировали учётную запись.
	firstAuth := auth && b.authFailures == 0
	if auth {
		b.authFailures++
	}
	afterDrop := !b.lastDropAt.IsZero() && time.Since(b.lastDropAt) < dropWindow
	delay := b.backoff
	if firstAuth {
		delay = retryAfterDrop
	} else if auth {
		delay = retryAuth
	}
	b.lastDropAt = time.Time{}
	if !auth {
		b.backoff = min(b.backoff*2, retryMax)
	}
	var text string
	switch {
	case firstAuth && afterDrop:
		text = "Сервер ещё держит прежнюю сессию"
	case auth:
		text = authText(errText)
	default:
		text = "Не подключается: " + errText
	}
	b.setState(BmstuWaiting, text+". Повтор через "+formatDelay(delay)+".")
	b.schedule(delay)
}

// tryConnect — одна попытка подключения. Возвращает "" при успехе или текст ошибки.
func (b *Bmstu) tryConnect() string {
	b.setState(BmstuConnecting, "Подключение…")
	cfg, err := core.ParseOvpn(b.store.BmstuOvpn())
	if err != nil {
		return err.Error()
	}
	if _, err := netenv.Refresh(); err != nil {
		return "нет подключения к сети"
	}
	ctx := context.Background()
	remoteIP, err := netenv.LookupIP(ctx, cfg.RemoteHost)
	if err != nil {
		return err.Error()
	}
	probe := ""
	if ip, err := netenv.LookupIP(ctx, probeHost); err == nil {
		probe = ip + ":443"
	} else {
		logf("bmstu probe host: %v", err)
	}
	proxy, err := b.EnsureProxy()
	if err != nil {
		return err.Error()
	}
	l := &downListener{b: b}
	t := bmstuvpn.NewTunnel(l)
	l.owner = t
	if err := t.Start(cfg.Text, b.store.BmstuUser(), b.store.BmstuPassword(), remoteIP, probe, proxy); err != nil {
		b.mu.Lock()
		b.lastTunnel = t
		b.mu.Unlock()
		logf("bmstu tunnel log:\n%s", t.Log())
		t.Stop()
		if msg := err.Error(); msg != "" {
			return msg
		}
		return fmt.Sprintf("%T", err)
	}
	b.tunnel = t
	b.mu.Lock()
	b.current, b.lastTunnel, b.upSince = t, t, time.Now()
	b.mu.Unlock()
	b.backoff = retryMin
	b.authFailures = 0
	b.setState(BmstuUp, "Подключён")
	return ""
}

// downListener — сообщение туннеля о падении. Помнит свой туннель: старые туннели не влияют на текущий.
type downListener struct {
	b     *Bmstu
	owner *bmstuvpn.Tunnel
}

func (l *downListener) OnDown(reason string) {
	which := l.owner
	l.b.run(func() { l.b.onDown(which, reason) })
}

func (b *Bmstu) onDown(which *bmstuvpn.Tunnel, reason string) {
	if which == nil || which != b.tunnel {
		return
	}
	logf("bmstu tunnel log:\n%s", which.Log())
	b.tunnel = nil
	b.mu.Lock()
	b.current, b.upSince = nil, time.Time{}
	b.mu.Unlock()
	if !b.active {
		return
	}
	b.lastDropAt = time.Now()
	b.setState(BmstuWaiting, "Туннель упал ("+reason+"). Переподключение через "+formatDelay(reconnectAfterDown)+"…")
	b.schedule(reconnectAfterDown)
}

func (b *Bmstu) stopTunnel() {
	b.cancelPending()
	if b.tunnel != nil {
		b.tunnel.Stop()
		b.tunnel = nil
	}
	b.mu.Lock()
	b.current, b.upSince = nil, time.Time{}
	b.mu.Unlock()
}

func (b *Bmstu) schedule(d time.Duration) {
	b.cancelPending()
	gen := b.pendingGen
	b.pending = time.AfterFunc(d, func() {
		b.run(func() {
			if gen == b.pendingGen {
				b.pending = nil
				b.connect()
			}
		})
	})
}

func (b *Bmstu) cancelPending() {
	b.pendingGen++
	if b.pending != nil {
		b.pending.Stop()
		b.pending = nil
	}
}

// authText — отказ сервера (AUTH_FAILED). Если сервер назвал причину, показываем её.
func authText(e string) string {
	reason := ""
	if i := strings.Index(e, "AUTH_FAILED:"); i >= 0 {
		reason = strings.TrimSpace(e[i+len("AUTH_FAILED:"):])
	}
	if reason == "" {
		return "Сервер отказал во входе. Проверьте логин и пароль и не подключена ли эта учётная " +
			"запись с другого устройства"
	}
	return "Сервер отказал во входе: " + reason
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

func formatDelay(d time.Duration) string {
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%d с", sec)
	}
	return fmt.Sprintf("%d мин", sec/60)
}
