// Package core — платформенно-независимая логика: разбор ссылок, правила маршрутизации,
// исключения, конфиг Xray, проверка .ovpn и подписки. Перенос одноимённых классов Android-версии.
package core

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	ProtoVLESS     = "vless"
	ProtoHysteria2 = "hysteria2"
)

var (
	SupportedNetworks = []string{"tcp", "raw", "ws", "grpc", "httpupgrade", "xhttp"}
	SupportedSecurity = []string{"none", "tls", "reality"}
)

// ProxyLink — ссылка на сервер: vless:// (аналог parse_vless из update-vpn.py) или hysteria2:// (hy2://).
type ProxyLink struct {
	Protocol string
	// VLESS: UUID; Hysteria2: строка авторизации.
	UUID        string
	Server      string
	Port        int
	Security    string
	Network     string
	Flow        string
	SNI         string
	PBK         string
	SID         string
	FP          string
	Path        string
	Host        string
	ServiceName string
	Mode        string
	Name        string
	// ALPN для TLS, через запятую: «h2,http/1.1».
	ALPN string
	// Шифрование VLESS: «none» или ключ ML-KEM.
	Encryption string
	// JSON-объект extra для xhttp (xmux, паддинг) или пусто.
	Extra string
	// JSON-объект finalmask (параметр fm) или пусто.
	Finalmask string
	// Проверять сертификат по этому имени вместо SNI (vcn).
	VerifyName string
	// SHA-256 сертификата сервера в hex, через запятую (pcs / pinSHA256).
	PinnedCert   string
	Obfs         string
	ObfsPassword string
}

func (p *ProxyLink) IsHysteria() bool { return p.Protocol == ProtoHysteria2 }

// IsSupportedScheme — поддерживает ли приложение ссылку с такой схемой.
func IsSupportedScheme(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "vless://") || strings.HasPrefix(t, "hysteria2://") || strings.HasPrefix(t, "hy2://")
}

var spaces = regexp.MustCompile(`\s+`)

func decode(s string) string {
	// Как URLDecoder.decode в Java: «+» — пробел
	v, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return v
}

func missing(protocol string) error {
	if protocol == ProtoHysteria2 {
		return errors.New("Отсутствуют обязательные параметры (пароль, сервер).")
	}
	return errors.New("Отсутствуют обязательные параметры (uuid, server, port).")
}

// ParseLink разбирает ссылку vless:// или hysteria2:// (hy2://).
func ParseLink(raw string) (*ProxyLink, error) {
	u := spaces.ReplaceAllString(raw, "")
	if u == "" {
		return nil, errors.New("Ссылка не указана.")
	}
	lower := strings.ToLower(u)
	var protocol, rest string
	switch {
	case strings.HasPrefix(lower, "vless://"):
		protocol, rest = ProtoVLESS, u[len("vless://"):]
	case strings.HasPrefix(lower, "hysteria2://"):
		protocol, rest = ProtoHysteria2, u[len("hysteria2://"):]
	case strings.HasPrefix(lower, "hy2://"):
		protocol, rest = ProtoHysteria2, u[len("hy2://"):]
	default:
		return nil, errors.New("Ссылка должна начинаться с vless:// или hysteria2://")
	}

	name := ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		name = decode(rest[i+1:])
		rest = rest[:i]
	}
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndexByte(rest, '@')
	if at <= 0 {
		return nil, missing(protocol)
	}
	user := decode(rest[:at])
	hostPort := rest[at+1:]

	var server, portText string
	if strings.HasPrefix(hostPort, "[") {
		end := strings.IndexByte(hostPort, ']')
		if end < 0 {
			return nil, missing(protocol)
		}
		server = hostPort[1:end]
		if end+1 < len(hostPort) && hostPort[end+1] == ':' {
			portText = hostPort[end+2:]
		}
	} else {
		colon := strings.LastIndexByte(hostPort, ':')
		if colon > 0 {
			server, portText = hostPort[:colon], hostPort[colon+1:]
		} else {
			server = hostPort
		}
	}
	// У Hysteria2 порт необязателен (443)
	if portText == "" && protocol == ProtoHysteria2 {
		portText = "443"
	}
	if strings.ContainsAny(portText, ",-") {
		return nil, errors.New("Смена портов (port hopping) пока не поддерживается.")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || user == "" || server == "" || port <= 0 || port > 65535 {
		return nil, missing(protocol)
	}

	q := map[string]string{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		k, v := pair, ""
		if eq := strings.IndexByte(pair, '='); eq >= 0 {
			k, v = pair[:eq], pair[eq+1:]
		}
		k, v = decode(k), decode(v)
		if _, ok := q[k]; !ok { // как parse_qs()[0]: берётся первое значение
			q[k] = v
		}
	}
	get := func(key, def string) string {
		if v := q[key]; v != "" {
			return v
		}
		return def
	}

	hy2 := protocol == ProtoHysteria2
	p := &ProxyLink{
		Protocol:     protocol,
		UUID:         user,
		Server:       strings.ToLower(server),
		Port:         port,
		Security:     get("security", "none"),
		Network:      get("type", "tcp"),
		Flow:         get("flow", ""),
		SNI:          get("sni", get("peer", "")),
		PBK:          get("pbk", ""),
		SID:          get("sid", ""),
		Path:         get("path", "/"),
		Host:         get("host", ""),
		ServiceName:  get("serviceName", ""),
		Mode:         get("mode", ""),
		Name:         name,
		ALPN:         get("alpn", ""),
		Encryption:   get("encryption", "none"),
		Extra:        get("extra", ""),
		Finalmask:    get("fm", ""),
		VerifyName:   get("vcn", ""),
		PinnedCert:   normalizePin(get("pcs", get("pinSHA256", ""))),
		Obfs:         get("obfs", ""),
		ObfsPassword: get("obfs-password", ""),
	}
	if hy2 {
		p.Security, p.Network, p.FP = "tls", "quic", get("fp", "")
		if p.Obfs != "" && p.Obfs != "salamander" {
			return nil, fmt.Errorf("Обфускация '%s' не поддерживается: salamander.", p.Obfs)
		}
		if p.Obfs == "salamander" && p.ObfsPassword == "" {
			return nil, errors.New("Для salamander в ссылке нужен obfs-password.")
		}
		return p, nil
	}
	p.FP = get("fp", "chrome")
	if !contains(SupportedNetworks, p.Network) {
		return nil, fmt.Errorf("Транспорт '%s' не поддерживается: %s.", p.Network, strings.Join(SupportedNetworks, ", "))
	}
	if !contains(SupportedSecurity, p.Security) {
		return nil, fmt.Errorf("Тип защиты '%s' не поддерживается: %s.", p.Security, strings.Join(SupportedSecurity, ", "))
	}
	if p.Security == "reality" && p.PBK == "" {
		return nil, errors.New("Для Reality в ссылке нужен открытый ключ (pbk).")
	}
	return p, nil
}

// «AB:CD:…» или «abcd…» → «abcd…» (Xray ждёт hex без разделителей).
func normalizePin(pin string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(pin))
}

// Title — название для списка серверов.
func (p *ProxyLink) Title() string {
	if p.Name != "" {
		return p.Name
	}
	return p.Server
}

// Subtitle — «VLESS · Reality · TCP», «Hysteria2 · QUIC».
func (p *ProxyLink) Subtitle() string {
	if p.IsHysteria() {
		if p.Obfs != "" {
			return "Hysteria2 · QUIC · Salamander"
		}
		return "Hysteria2 · QUIC"
	}
	sec := ""
	switch p.Security {
	case "none":
	case "tls":
		sec = " · TLS"
	default:
		sec = " · " + strings.ToUpper(p.Security[:1]) + p.Security[1:]
	}
	return "VLESS" + sec + " · " + strings.ToUpper(p.Network)
}

// Describe — короткое описание без UUID и ключей.
func (p *ProxyLink) Describe() string {
	where := fmt.Sprintf("%s:%d", p.Server, p.Port)
	if strings.Contains(p.Server, ":") {
		where = fmt.Sprintf("[%s]:%d", p.Server, p.Port)
	}
	title := ""
	if p.Name != "" {
		title = p.Name + " — "
	}
	kind := p.Network + ", " + p.Security
	if p.IsHysteria() {
		kind = "hysteria2"
	}
	return title + where + " (" + kind + ")"
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
