// Package netenv — физическая сеть компьютера: интерфейс с маршрутом по умолчанию (мимо TUN),
// его DNS и привязка собственных сокетов приложения к нему.
//
// Это замена того, что на Android даёт система: VpnService.protect() и исключение приложения из VPN
// (addDisallowedApplication), а также ConnectivityManager с DNS текущей сети.
package netenv

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Uplink — физический интерфейс с маршрутом по умолчанию.
type Uplink struct {
	Index int
	Name  string
	// DNS — первый IPv4 DNS-сервер интерфейса ("" — не определился).
	DNS string
}

// Same — тот же интерфейс и тот же DNS.
func (u *Uplink) Same(o *Uplink) bool {
	if u == nil || o == nil {
		return u == o
	}
	return u.Index == o.Index && u.Name == o.Name && u.DNS == o.DNS
}

var (
	current atomic.Pointer[Uplink]
	tunName atomic.Pointer[string]
	// tunDNS — адрес DNS внутри туннеля: он не DNS физической сети.
	tunDNS atomic.Pointer[string]
)

// SetTun задаёт имя интерфейса TUN и его DNS, чтобы не принимать их за физическую сеть.
func SetTun(name, dns string) {
	tunName.Store(&name)
	tunDNS.Store(&dns)
}

func tunNameValue() string {
	if p := tunName.Load(); p != nil {
		return *p
	}
	return ""
}

func isTunDNS(ip string) bool {
	p := tunDNS.Load()
	return p != nil && *p == ip
}

// Refresh определяет физический интерфейс заново и запоминает его для привязки сокетов.
func Refresh() (*Uplink, error) {
	u, err := detect(tunNameValue())
	if err != nil {
		return nil, err
	}
	current.Store(u)
	return u, nil
}

// Current — последний найденный физический интерфейс или nil.
func Current() *Uplink { return current.Load() }

// BindControl — net.Dialer.Control: сокет уходит через физический интерфейс, а не в TUN.
// Соединения к loopback не привязываются. Если интерфейс неизвестен, сокет не трогается.
func BindControl(network, address string, c syscall.RawConn) error {
	u := Current()
	if u == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip, err := netip.ParseAddr(host)
	if err == nil && ip.IsLoopback() {
		return nil
	}
	v6 := err == nil && ip.Is6() && !ip.Is4In6()
	var serr error
	if cerr := c.Control(func(fd uintptr) { serr = bind(fd, v6, u) }); cerr != nil {
		return cerr
	}
	return serr
}

// Resolver — резолвер мимо туннеля (подписки и прочие запросы приложения): DNS физической сети,
// затем Яндекс и Google по TCP — обычный DNS по UDP 53 на внешние серверы в корпоративных сетях закрыт.
var Resolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var lastErr error
		if dns := networkDNS(); dns != "" {
			d := net.Dialer{Timeout: 3 * time.Second, Control: BindControl}
			if c, err := d.DialContext(ctx, network, net.JoinHostPort(dns, "53")); err == nil {
				return c, nil
			} else {
				lastErr = err
			}
		}
		for _, s := range TCPDNS {
			d := net.Dialer{Timeout: 4 * time.Second, Control: BindControl}
			c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s, "53"))
			if err == nil {
				return c, nil
			}
			lastErr = err
		}
		return nil, lastErr
	},
}

// LookupIP — IP по имени мимо туннеля (DNS сети, DoH, DNS по TCP). IP-адрес возвращается как есть.
func LookupIP(ctx context.Context, host string) (string, error) {
	ip, _, err := ResolveServer(ctx, host)
	return ip, err
}

// Dialer — соединения мимо туннеля.
func Dialer() *net.Dialer {
	return &net.Dialer{Timeout: 15 * time.Second, Control: BindControl, Resolver: Resolver}
}

// HTTPClient — HTTP мимо туннеля (подписки), как запросы Android-приложения, исключённого из VPN.
func HTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = Dialer().DialContext
	return &http.Client{Transport: tr, Timeout: 40 * time.Second}
}

// HasIPv6 — в системе включён IPv6 (есть хотя бы один IPv6-адрес не на loopback).
func HasIPv6() bool {
	ifs, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || i.Name == tunNameValue() {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() == nil {
				return true
			}
		}
	}
	return false
}
