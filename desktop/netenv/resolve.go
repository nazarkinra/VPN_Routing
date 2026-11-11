package netenv

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Адрес VPN-сервера (аналог ServerResolver.java). Порядок, работающий и в корпоративных сетях, где
// обычный DNS по UDP 53 на внешние серверы закрыт (Wi-Fi МГТУ):
//  1. адрес в ключе уже IP — берётся как есть, без DNS;
//  2. DNS физической сети, не дольше 4 с;
//  3. DoH по IP (HTTPS 443): Cloudflare, затем Google — им не нужен DNS для своего имени;
//  4. DNS по TCP 53 (Google, Яндекс) — если сеть подменяет HTTPS-сертификаты и DoH не проходит.
// Все запросы идут через физическую сеть, мимо туннеля.

const (
	systemTimeout = 4 * time.Second
	dohTimeout    = 5 * time.Second
)

// DoHURLs — DoH по IP-адресу, ответ в JSON (Cloudflare и Google отвечают одинаково).
var DoHURLs = []string{
	"https://1.1.1.1/dns-query?type=A&name=",
	"https://8.8.8.8/resolve?type=A&name=",
}

// TCPDNS — DNS-серверы для запросов по TCP.
var TCPDNS = []string{"8.8.8.8", "77.88.8.8"}

// IsIPLiteral — адрес уже IP.
func IsIPLiteral(host string) bool {
	_, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil
}

// ResolveServer возвращает IP и чем он получен (для журнала и сообщений об ошибке).
func ResolveServer(ctx context.Context, host string) (ip, method string, err error) {
	h := strings.Trim(host, "[]")
	if IsIPLiteral(h) {
		a, _ := netip.ParseAddr(h)
		return a.String(), "IP из ключа", nil
	}
	var errs []string

	if dns := networkDNS(); dns != "" {
		if ip, err := lookupVia(ctx, h, "udp", dns, systemTimeout); err == nil {
			return ip, "DNS сети", nil
		} else {
			errs = append(errs, "DNS сети: "+shortErr(err))
		}
	} else if ip, err := lookupSystem(ctx, h); err == nil {
		// DNS сети неизвестен — системный резолвер
		return ip, "DNS системы", nil
	} else {
		errs = append(errs, "DNS системы: "+shortErr(err))
	}

	for _, base := range DoHURLs {
		u, _ := url.Parse(base)
		if ip, err := doh(ctx, base+url.QueryEscape(h)); err == nil {
			return ip, "DoH " + u.Host, nil
		} else {
			errs = append(errs, "DoH "+u.Host+": "+shortErr(err))
		}
	}
	// DNS по TCP: не зависит от сертификатов (сеть с проверкой HTTPS ломает DoH)
	for _, server := range TCPDNS {
		if ip, err := lookupVia(ctx, h, "tcp", server, dohTimeout); err == nil {
			return ip, "DNS по TCP " + server, nil
		} else {
			errs = append(errs, "TCP "+server+": "+shortErr(err))
		}
	}
	return "", "", fmt.Errorf("Не удалось разрешить имя сервера '%s' (%s). Проверьте подключение к интернету.",
		host, strings.Join(errs, "; "))
}

func networkDNS() string {
	if u := Current(); u != nil {
		return u.DNS
	}
	return ""
}

// lookupVia — запрос A к одному серверу по UDP или TCP, сокет привязан к физическому интерфейсу.
func lookupVia(ctx context.Context, host, network, server string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: timeout, Control: BindControl}
		// Go сам выбирает формат: для TCP-соединения запрос идёт с длиной в начале (RFC 1035, 4.2.2)
		return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
	}}
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", errors.New("нет адреса")
	}
	return ips[0].String(), nil
}

func lookupSystem(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, systemTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return a.IP.String(), nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0].IP.String(), nil
	}
	return "", errors.New("нет адреса")
}

func doh(ctx context.Context, u string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: dohTimeout, Control: BindControl}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: dohTimeout,
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return "", err
	}
	ip, err := ParseDoHJSON(body)
	if err == nil && ip == "" {
		err = errors.New("нет адреса")
	}
	return ip, err
}

// ParseDoHJSON — первый IPv4 из ответа DoH в формате JSON; "" — записей A нет.
func ParseDoHJSON(body []byte) (string, error) {
	var r struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	for _, a := range r.Answer {
		if ip, err := netip.ParseAddr(a.Data); a.Type == 1 && err == nil && ip.Is4() {
			return ip.String(), nil
		}
	}
	return "", nil
}

func shortErr(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout {
			return "нет ответа"
		}
		if dnsErr.IsNotFound {
			return "имя не найдено"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "нет ответа"
	}
	s := err.Error()
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}
