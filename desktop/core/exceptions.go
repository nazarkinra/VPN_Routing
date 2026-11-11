package core

import (
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Приведение исключений к единому виду (аналог normalize из add_exception.sh и Exceptions.java):
// адрес сайта сводится к имени хоста, кириллица — к punycode, IP — к подсети.

var (
	domainRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	labelRE  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	ipv4RE   = regexp.MustCompile(`^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$`)
	ipv6RE   = regexp.MustCompile(`^[0-9a-f:.]+$`)
)

// IsCIDR — значение является IP-адресом или подсетью.
func IsCIDR(value string) bool {
	_, ok := ParseCIDR(value)
	return ok
}

// ParseCIDR — подсеть в каноническом виде (биты хоста обнуляются, как ip_network(strict=False)).
func ParseCIDR(text string) (string, bool) {
	addr, prefix := text, -1
	if i := strings.IndexByte(text, '/'); i >= 0 {
		addr = text[:i]
		n, err := strconv.Atoi(text[i+1:])
		if err != nil {
			return "", false
		}
		prefix = n
	}
	v4 := ipv4RE.MatchString(addr)
	v6 := !v4 && strings.Contains(addr, ":") && ipv6RE.MatchString(addr)
	if !v4 && !v6 {
		return "", false
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return "", false
	}
	if v6 && ip.Is4In6() {
		return "", false
	}
	if prefix < 0 {
		prefix = ip.BitLen()
	}
	p, err := ip.Prefix(prefix)
	if err != nil {
		return "", false
	}
	return p.String(), true
}

// NormalizeException возвращает подсеть (1.2.3.0/24) или домен (example.com, .example).
func NormalizeException(raw string) (string, error) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return "", errors.New("Укажите домен или IP-адрес.")
	}
	if c, ok := ParseCIDR(text); ok {
		return c, nil
	}
	if strings.Contains(text, "://") {
		u, err := url.Parse(text)
		if err != nil {
			text = ""
		} else {
			text = u.Hostname()
		}
	} else {
		text = strings.SplitN(text, "/", 2)[0]
		if !strings.HasPrefix(text, "[") {
			text = strings.SplitN(text, ":", 2)[0]
		}
	}
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = text[1 : len(text)-1]
	}
	if c, ok := ParseCIDR(text); ok {
		return c, nil
	}

	// Домен верхнего уровня можно указать с точкой в начале: .example
	prefix := ""
	if strings.HasPrefix(text, ".") {
		prefix = "."
	}
	host := strings.TrimRight(text[len(prefix):], ".")
	a, err := idna.Punycode.ToASCII(host)
	if err != nil {
		return "", errors.New("Некорректный домен: " + raw)
	}
	host = strings.ToLower(a)
	ok := domainRE.MatchString(host)
	if prefix != "" {
		ok = labelRE.MatchString(host)
	}
	if !ok {
		return "", errors.New("Некорректный домен или IP: " + raw)
	}
	return prefix + host, nil
}
