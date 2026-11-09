package bmstuvpn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Proxy — локальный SOCKS5 для Xray. Работает всё время, пока включён VPN: при поднятом туннеле
// соединения идут через OpenVPN, иначе напрямую (как bmstu.ru на Pi, когда туннель опущен).
type Proxy struct {
	s *socksServer
}

// NewProxy запускает прокси на 127.0.0.1 со случайным портом, логином и паролем.
func NewProxy() (*Proxy, error) {
	s, err := startSocks(newRingLog(50))
	if err != nil {
		return nil, err
	}
	return &Proxy{s: s}, nil
}

// Port — порт прокси.
func (p *Proxy) Port() int { return p.s.port }

// User и Password — случайные учётные данные: другие приложения телефона не смогут
// воспользоваться прокси.
func (p *Proxy) User() string { return p.s.user }

func (p *Proxy) Password() string { return p.s.pass }

// ViaTunnel — соединения сейчас идут через туннель МГТУ.
func (p *Proxy) ViaTunnel() bool {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	return p.s.tnet != nil
}

// Close останавливает прокси.
func (p *Proxy) Close() { p.s.Close() }

// socksServer — минимальный SOCKS5 (только CONNECT, вход по логину и паролю) на 127.0.0.1.
type socksServer struct {
	ln   net.Listener
	log  *ringLog
	port int
	user string
	pass string

	mu sync.Mutex
	// tnet — стек TCP/IP поверх туннеля OpenVPN или nil (тогда соединения идут напрямую).
	tnet  *netstack.Net
	conns map[net.Conn]struct{}
	// tunnelConns — соединения через туннель: закрываются, когда туннель опускается.
	tunnelConns map[net.Conn]struct{}
}

// attach направляет новые соединения в туннель.
func (s *socksServer) attach(tnet *netstack.Net) {
	s.mu.Lock()
	s.tnet = tnet
	s.mu.Unlock()
}

// detach возвращает прямой выход, если туннель tnet всё ещё текущий, и рвёт его соединения.
func (s *socksServer) detach(tnet *netstack.Net) {
	s.mu.Lock()
	if s.tnet != tnet {
		s.mu.Unlock()
		return
	}
	s.tnet = nil
	conns := s.tunnelConns
	s.tunnelConns = map[net.Conn]struct{}{}
	s.mu.Unlock()
	for c := range conns {
		_ = c.Close()
	}
}

func (s *socksServer) dial(ctx context.Context, target string) (net.Conn, error) {
	s.mu.Lock()
	tnet := s.tnet
	s.mu.Unlock()
	if tnet == nil {
		// Приложение исключено из своего VPN: это прямой выход через Wi-Fi или мобильную сеть
		d := net.Dialer{Control: protectControl}
		return d.DialContext(ctx, "tcp", target)
	}
	c, err := tnet.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.tnet == tnet {
		s.tunnelConns[c] = struct{}{}
	} else {
		// Туннель опустился, пока шло подключение
		s.mu.Unlock()
		_ = c.Close()
		return nil, errors.New("туннель опущен")
	}
	s.mu.Unlock()
	return c, nil
}

func randomToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func startSocks(log *ringLog) (*socksServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &socksServer{
		ln:          ln,
		log:         log,
		port:        ln.Addr().(*net.TCPAddr).Port,
		user:        randomToken(),
		pass:        randomToken(),
		conns:       map[net.Conn]struct{}{},
		tunnelConns: map[net.Conn]struct{}{},
	}
	go s.serve()
	return s, nil
}

func (s *socksServer) Close() {
	_ = s.ln.Close()
	s.mu.Lock()
	all := s.conns
	s.conns = map[net.Conn]struct{}{}
	s.tunnelConns = map[net.Conn]struct{}{}
	s.tnet = nil
	s.mu.Unlock()
	for c := range all {
		_ = c.Close()
	}
}

func (s *socksServer) track(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
		delete(s.tunnelConns, c)
	}
}

func (s *socksServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

var errSocks = errors.New("socks: bad request")

func (s *socksServer) handle(c net.Conn) {
	s.track(c, true)
	defer func() {
		s.track(c, false)
		_ = c.Close()
	}()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))

	target, err := s.handshake(c)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	remote, err := s.dial(ctx, target)
	cancel()
	if err != nil {
		s.log.Warnf("socks: %s: %v", target, err)
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
		return
	}
	s.track(remote, true)
	defer func() {
		s.track(remote, false)
		_ = remote.Close()
	}()
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, c)
		closeWrite(remote)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, remote)
		closeWrite(c)
		done <- struct{}{}
	}()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	} else {
		_ = c.Close()
	}
}

// handshake: приветствие (метод 0x02 — логин/пароль), проверка учётных данных, запрос CONNECT.
func (s *socksServer) handshake(c net.Conn) (string, error) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[0] != 5 {
		return "", errSocks
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", errSocks
	}
	hasUserPass := false
	for _, m := range methods {
		if m == 2 {
			hasUserPass = true
		}
	}
	if !hasUserPass {
		_, _ = c.Write([]byte{5, 0xff})
		return "", errSocks
	}
	if _, err := c.Write([]byte{5, 2}); err != nil {
		return "", err
	}

	// RFC 1929
	ver := make([]byte, 2)
	if _, err := io.ReadFull(c, ver); err != nil || ver[0] != 1 {
		return "", errSocks
	}
	user := make([]byte, ver[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return "", errSocks
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(c, plen); err != nil {
		return "", errSocks
	}
	pass := make([]byte, plen[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return "", errSocks
	}
	okUser := subtle.ConstantTimeCompare(user, []byte(s.user)) == 1
	okPass := subtle.ConstantTimeCompare(pass, []byte(s.pass)) == 1
	if !okUser || !okPass {
		_, _ = c.Write([]byte{1, 1})
		return "", errSocks
	}
	if _, err := c.Write([]byte{1, 0}); err != nil {
		return "", err
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 {
		return "", errSocks
	}
	if req[1] != 1 { // только CONNECT
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", errSocks
	}
	var host string
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return "", errSocks
		}
		host = net.IP(ip).String()
	case 4:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(c, ip); err != nil {
			return "", errSocks
		}
		host = net.IP(ip).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return "", errSocks
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return "", errSocks
		}
		// Имя разрешается DNS обычной сети, как на Pi (через домашний роутер)
		addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), string(name))
		if err != nil || len(addrs) == 0 {
			_, _ = c.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
			return "", errSocks
		}
		host = addrs[0].IP.String()
		for _, a := range addrs {
			if a.IP.To4() != nil {
				host = a.IP.String()
				break
			}
		}
	default:
		_, _ = c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", errSocks
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(c, portBytes); err != nil {
		return "", errSocks
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes)))), nil
}
