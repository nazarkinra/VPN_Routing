package core

import (
	"encoding/json"
	"strings"
)

// Конфиг Xray для ПК. Как и на Android, одно ядро Xray принимает трафик из TUN, определяет домен
// по TLS/HTTP (sniffing) и раскладывает его по правилам sing-box с Raspberry Pi. Отличие от Android:
// TUN создаёт само ядро (Wintun на Windows, /dev/net/tun на Linux) и само прописывает системные
// маршруты, а собственные соединения ядра привязываются к физическому интерфейсу
// (autoOutboundsInterface) — аналог исключения приложения из своего VPN.

const (
	// TunAddress — адрес интерфейса TUN, TunDNS — DNS внутри туннеля (запросы к нему перехватываются).
	TunAddress  = "172.19.0.1"
	TunDNS      = "172.19.0.2"
	TunAddress6 = "fdfe:dcba:9876::1"
	// MTU 1400 — с запасом для корпоративных сетей и туннелей поверх них (как в Android 1.4)
	MTU = 1400
	// BmstuDomain — домены, которые идут через VPN МГТУ.
	BmstuDomain = "bmstu.ru"
	// DelayTestURL — адрес проверки задержки (как в клиентах Xray).
	DelayTestURL = "https://www.gstatic.com/generate_204"
)

// Builtins — встроенные домены напрямую без отключённых пользователем на вкладке «Напрямую».
type Builtins struct {
	Direct []string
	Tunnel []string
}

// AllBuiltins — все встроенные правила.
func AllBuiltins() Builtins { return Builtins{Direct: DirectDomains, Tunnel: DirectTunnelDomains} }

// Режимы маршрутизации по приложениям.
const (
	// AppsAll — все приложения идут по правилам (по умолчанию).
	AppsAll = ""
	// AppsInclude — по правилам идут только выбранные приложения, остальные — напрямую.
	AppsInclude = "include"
	// AppsExclude — выбранные приложения идут напрямую, остальные — по правилам.
	AppsExclude = "exclude"
)

// AppRules — маршрутизация по приложениям: режим и имена процессов (telegram.exe, firefox).
type AppRules struct {
	Mode  string
	Names []string
}

// BmstuRoute — локальный SOCKS-прокси туннеля МГТУ.
type BmstuRoute struct {
	Port     int
	User     string
	Password string
}

// TunOptions — параметры интерфейса TUN, зависящие от системы.
type TunOptions struct {
	// Name — имя интерфейса (адаптера Wintun на Windows).
	Name string
	// IPv6 — заворачивать ли IPv6 в туннель (если в системе IPv6 выключен, адаптер без него не стартует).
	IPv6 bool
	// SetDNS — назначить адаптеру DNS TunDNS (Windows).
	SetDNS bool
	// DNSToGateway — перевести системный резолвер на TUN через resolvectl (Linux с systemd-resolved).
	DNSToGateway bool
	// BlockDNSLeak — фильтры WFP: DNS остальных программ только через TUN (Windows).
	BlockDNSLeak bool
	// LogFile — журнал ошибок ядра ("" — в stderr).
	LogFile string
	Debug   bool
}

type obj = map[string]any
type arr = []any

// BuildConfig собирает конфиг Xray.
//
//	link       — разобранная ссылка на сервер
//	serverIP   — IP сервера (имя разрешено заранее, как в update-vpn)
//	localDNS   — DNS текущей сети (роутер или провайдер) для российских доменов, может быть ""
//	exceptions — исключения пользователя (домены и подсети)
//	bmstu      — прокси туннеля МГТУ или nil, если VPN МГТУ выключен
//
//	apps       — маршрутизация по приложениям (какие программы идут по правилам, а какие напрямую)
//
// link == nil — режим «только МГТУ»: основного сервера нет, bmstu.ru идёт в туннель университета,
// всё остальное (и DNS) — напрямую.
func BuildConfig(link *ProxyLink, serverIP, localDNS string, exceptions []string, builtins Builtins, bmstu *BmstuRoute, apps AppRules, tun TunOptions) (string, error) {
	corpOnly := link == nil
	var userDomains, userCIDRs []string
	for _, item := range exceptions {
		if IsCIDR(item) {
			userCIDRs = append(userCIDRs, item)
		} else {
			userDomains = append(userDomains, item)
		}
	}

	logCfg := obj{"loglevel": "warning", "access": "none"}
	if tun.Debug {
		logCfg = obj{"loglevel": "info", "dnsLog": true}
	}
	if tun.LogFile != "" {
		logCfg["error"] = tun.LogFile
	}

	// Первый outbound — маршрут по умолчанию: proxy (как final: proxy), без основного VPN — direct
	var outbounds arr
	if !corpOnly {
		proxy, err := proxyOutbound(link, serverIP)
		if err != nil {
			return "", err
		}
		outbounds = append(outbounds, proxy)
	}
	outbounds = append(outbounds,
		obj{"tag": "direct", "protocol": "freedom",
			"streamSettings": obj{"sockopt": obj{"domainStrategy": "UseIPv4"}}},
		obj{"tag": "block", "protocol": "blackhole"},
		obj{"tag": "dns-out", "protocol": "dns", "settings": obj{"nonIPQuery": "reject"}},
	)
	if bmstu != nil {
		outbounds = append(outbounds, bmstuOutbound(bmstu))
	}

	dns := dnsConfig(localDNS, builtins.Direct, userDomains)
	if corpOnly {
		dns = directDNSConfig(localDNS)
	}
	root := obj{
		"log":       logCfg,
		"dns":       dns,
		"inbounds":  arr{tunInbound(tun)},
		"outbounds": outbounds,
		"routing":   routing(localDNS, builtins, userDomains, userCIDRs, bmstu != nil, corpOnly, apps),
	}
	b, err := json.MarshalIndent(root, "", "  ")
	return string(b), err
}

// BuildPingConfig — конфиг для замера задержки сервера: только выход на сервер, без TUN и правил.
func BuildPingConfig(link *ProxyLink, serverIP string) (string, error) {
	proxy, err := proxyOutbound(link, serverIP)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(obj{"log": obj{"loglevel": "none"}, "outbounds": arr{proxy}})
	return string(b), err
}

func tunInbound(t TunOptions) obj {
	gateway := arr{TunAddress + "/30"}
	routes := arr{"0.0.0.0/0"}
	if t.IPv6 {
		gateway = append(gateway, TunAddress6+"/126")
		routes = append(routes, "::/0")
	}
	settings := obj{
		"name":                   t.Name,
		"mtu":                    MTU,
		"gateway":                gateway,
		"autoSystemRoutingTable": routes,
		// Сокеты самого Xray (к VPS и напрямую) привязываются к физическому интерфейсу
		"autoOutboundsInterface": "auto",
	}
	if t.SetDNS {
		settings["dns"] = arr{TunDNS}
	}
	if t.DNSToGateway {
		settings["autoSystemDnsToGateway"] = true
	}
	if t.BlockDNSLeak {
		settings["autoSystemWfpBlockLeak"] = arr{"dns"}
	}
	return obj{
		"tag":      "tun-in",
		"port":     0,
		"protocol": "tun",
		"settings": settings,
		// Аналог sniff в sing-box: домен берётся из TLS SNI / HTTP Host, адрес назначения не меняется
		"sniffing": obj{
			"enabled":      true,
			"destOverride": arr{"http", "tls", "quic"},
			"routeOnly":    true,
		},
	}
}

func bmstuOutbound(r *BmstuRoute) obj {
	return obj{
		"tag":      "bmstu",
		"protocol": "socks",
		"settings": obj{"servers": arr{obj{
			"address": "127.0.0.1",
			"port":    r.Port,
			"users":   arr{obj{"user": r.User, "pass": r.Password}},
		}}},
	}
}

func dnsServer(address string) obj {
	return obj{"address": address, "timeoutMs": DNSTimeoutMs}
}

// dnsConfig — как в Android 1.4: напрямую по UDP 53 запросы идут только к DNS, выданному сетью (в
// корпоративных сетях вроде Wi-Fi МГТУ внешний UDP 53 закрыт). Запасные — по TCP и DoH.
func dnsConfig(localDNS string, direct, userDomains []string) obj {
	domains := domainList(direct, userDomains)
	var servers arr
	// Российские домены и исключения — через DNS текущей сети (как local-dns на Pi), затем запасные.
	// Без доменов такие серверы стали бы общими, поэтому при пустом списке они не добавляются
	if len(domains) > 0 {
		if localDNS != "" {
			s := dnsServer(localDNS)
			s["port"], s["domains"], s["skipFallback"] = 53, domains, true
			servers = append(servers, s)
		}
		for _, address := range FallbackLocalDNS {
			s := dnsServer(address)
			s["domains"], s["skipFallback"] = domains, true
			servers = append(servers, s)
		}
	}
	// Остальное — через VPN: Google DNS по TCP, затем DoH Cloudflare
	for _, address := range ProxyDNS {
		servers = append(servers, dnsServer(address))
	}
	return obj{"tag": "dns-internal", "queryStrategy": "UseIPv4", "servers": servers}
}

// directDNSConfig — DNS без основного VPN: все домены через DNS сети, затем запасные (TCP и DoH напрямую).
func directDNSConfig(localDNS string) obj {
	var servers arr
	if localDNS != "" {
		s := dnsServer(localDNS)
		s["port"] = 53
		servers = append(servers, s)
	}
	for _, address := range FallbackLocalDNS {
		servers = append(servers, dnsServer(address))
	}
	return obj{"tag": "dns-internal", "queryStrategy": "UseIPv4", "servers": servers}
}

func rule(kv ...any) obj {
	r := obj{"type": "field"}
	for i := 0; i+1 < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}

func toArr(items []string) arr {
	a := make(arr, 0, len(items))
	for _, s := range items {
		a = append(a, s)
	}
	return a
}

func routing(localDNS string, builtins Builtins, userDomains, userCIDRs []string, bmstu, corpOnly bool, apps AppRules) obj {
	var rules arr
	// Запросы DNS-модуля к DNS сети идут напрямую, к 8.8.8.8 — через VPN (правило по умолчанию)
	// (у запасных серверов «+local»: они и так идут мимо правил)
	if localDNS != "" {
		rules = append(rules, rule("inboundTag", arr{"dns-internal"}, "ip", arr{localDNS}, "outboundTag", "direct"))
	}
	// hijack-dns: любой DNS-запрос из туннеля обрабатывает DNS-модуль Xray. DNS запрашивает системный
	// резолвер, а не сама программа, поэтому он одинаков для всех приложений
	rules = append(rules, rule("inboundTag", arr{"tun-in"}, "port", "53", "outboundTag", "dns-out"))
	if !corpOnly {
		// Локальная сеть и VPN-протоколы — напрямую у любых приложений
		rules = append(rules, rule("ip", toArr(PrivateCIDRs), "outboundTag", "direct"))
		rules = append(rules, rule("port", DirectPorts, "outboundTag", "direct"))
	}

	// Правила, которые действуют на приложения «по нашим правилам»
	var policy arr
	// Сайты МГТУ — через туннель университета; домен известен из HTTP и TLS, поэтому только TCP
	if bmstu {
		policy = append(policy, rule("domain", arr{"domain:" + BmstuDomain}, "network", "tcp", "outboundTag", "bmstu"))
		// QUIC к сайтам МГТУ отклоняется: прокси туннеля только TCP, браузер перейдёт на TCP
		policy = append(policy, rule("domain", arr{"domain:" + BmstuDomain}, "network", "udp", "port", "443", "outboundTag", "block"))
	}
	// Без основного VPN всё остальное и так идёт напрямую: остальные правила не нужны
	if !corpOnly {
		// Правило с пустым списком доменов не добавляется (все встроенные могли быть удалены)
		if tunnels := domainList(builtins.Tunnel, nil); len(tunnels) > 0 {
			policy = append(policy, rule("domain", tunnels, "outboundTag", "direct"))
		}
		if direct := domainList(builtins.Direct, userDomains); len(direct) > 0 {
			policy = append(policy, rule("domain", direct, "outboundTag", "direct"))
		}
		if len(userCIDRs) > 0 {
			policy = append(policy, rule("ip", toArr(userCIDRs), "outboundTag", "direct"))
		}
		// QUIC к зарубежным ресурсам отклоняется: приложения сразу переходят на TCP
		policy = append(policy, rule("network", "udp", "port", "443", "outboundTag", "block"))
	}

	// Маршрутизация по приложениям (процессам)
	names := processNames(apps.Names)
	switch {
	case apps.Mode == AppsExclude && len(names) > 0:
		// Выбранные — целиком напрямую (и мимо туннеля МГТУ), остальные — по правилам
		rules = append(rules, rule("process", names, "outboundTag", "direct"))
		rules = append(rules, policy...)
	case apps.Mode == AppsInclude && len(names) > 0:
		// По правилам — только выбранные: у каждого правила условие «процесс из списка»; выбранные, не
		// попавшие ни под одно правило, — в маршрут по умолчанию; все остальные приложения — напрямую
		for _, r := range policy {
			r.(obj)["process"] = names
			rules = append(rules, r)
		}
		def := "direct"
		if !corpOnly {
			def = "proxy"
		}
		rules = append(rules, rule("process", names, "network", "tcp,udp", "outboundTag", def))
		rules = append(rules, rule("network", "tcp,udp", "outboundTag", "direct"))
	default:
		rules = append(rules, policy...)
	}
	return obj{"domainStrategy": "AsIs", "rules": rules}
}

// processNames — имена процессов для правила Xray. Xray сравнивает имя без «.exe» с учётом регистра,
// поэтому к имени добавляется вариант в нижнем регистре (имя, введённое вручную, может отличаться).
func processNames(names []string) arr {
	seen := map[string]bool{}
	var a arr
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		for _, v := range []string{n, strings.ToLower(n)} {
			if !seen[v] {
				seen[v] = true
				a = append(a, v)
			}
		}
	}
	return a
}

// domain:example.com в Xray — сам домен и все поддомены (как domain_suffix в sing-box).
func domainList(builtin, extra []string) arr {
	all := append(append([]string{}, builtin...), extra...)
	var a arr
	for _, name := range Compact(all) {
		a = append(a, "domain:"+name)
	}
	return a
}

// VLESS-выход: то же, что update_xray в update-vpn.py.
func proxyOutbound(p *ProxyLink, serverIP string) (obj, error) {
	if p.IsHysteria() {
		return hysteriaOutbound(p, serverIP), nil
	}
	enc := p.Encryption
	if enc == "" {
		enc = "none"
	}
	user := obj{"id": p.UUID, "encryption": enc}
	if p.Flow != "" {
		user["flow"] = p.Flow
	}
	stream := obj{"network": p.Network, "security": p.Security}
	fp := p.FP
	if fp == "" {
		fp = "chrome"
	}
	switch p.Security {
	case "reality":
		sn := p.SNI
		if sn == "" {
			sn = p.Server
		}
		stream["realitySettings"] = obj{"show": false, "fingerprint": fp, "serverName": sn,
			"publicKey": p.PBK, "shortId": p.SID, "spiderX": ""}
	case "tls":
		tls := obj{"serverName": firstNonEmpty(p.SNI, p.Host, p.Server), "fingerprint": fp}
		if p.ALPN != "" {
			tls["alpn"] = alpnList(p.ALPN)
		}
		applyCertChecks(tls, p)
		stream["tlsSettings"] = tls
	}
	// Параметр fm: фрагментация TLS Hello и другие маски поверх транспорта
	if fm := jsonObject(p.Finalmask); fm != nil {
		stream["finalmask"] = fm
	}
	switch p.Network {
	case "ws", "httpupgrade", "xhttp":
		path := p.Path
		if path == "" {
			path = "/"
		}
		t := obj{"path": path}
		if host := firstNonEmpty(p.Host, p.SNI); host != "" {
			t["host"] = host
		}
		if p.Network == "xhttp" {
			if p.Mode != "" {
				t["mode"] = p.Mode
			}
			// extra: xmux, паддинг и прочие настройки xhttp от панели
			if extra := jsonObject(p.Extra); extra != nil {
				t["extra"] = extra
			}
		}
		stream[p.Network+"Settings"] = t
	case "grpc":
		stream["grpcSettings"] = obj{"serviceName": p.ServiceName}
	}

	out := obj{
		"tag":            "proxy",
		"protocol":       "vless",
		"settings":       obj{"vnext": arr{obj{"address": serverIP, "port": p.Port, "users": arr{user}}}},
		"streamSettings": stream,
	}
	// Mux несовместим с flow xtls-rprx-vision, а XHTTP мультиплексирует соединения сам (xmux)
	if p.Flow == "" && p.Network != "xhttp" {
		out["mux"] = obj{"enabled": true, "concurrency": 8, "xudpConcurrency": 16, "xudpProxyUDP443": "reject"}
	}
	return out, nil
}

// Hysteria2 (QUIC): protocol hysteria версии 2, TLS с ALPN h3, обфускация salamander — маска finalmask.udp.
func hysteriaOutbound(p *ProxyLink, serverIP string) obj {
	alpn := p.ALPN
	if alpn == "" {
		alpn = "h3"
	}
	tls := obj{"serverName": firstNonEmpty(p.SNI, p.Server), "alpn": alpnList(alpn)}
	if p.FP != "" {
		tls["fingerprint"] = p.FP
	}
	applyCertChecks(tls, p)
	stream := obj{
		"network":          "hysteria",
		"hysteriaSettings": obj{"version": 2, "auth": p.UUID},
		"security":         "tls",
		"tlsSettings":      tls,
	}
	fm := jsonObject(p.Finalmask)
	if p.Obfs == "salamander" {
		if fm == nil {
			fm = obj{}
		}
		udp, _ := fm["udp"].([]any)
		udp = append(udp, obj{"type": "salamander", "settings": obj{"password": p.ObfsPassword}})
		fm["udp"] = udp
	}
	if fm != nil {
		stream["finalmask"] = fm
	}
	return obj{
		"tag":            "proxy",
		"protocol":       "hysteria",
		"settings":       obj{"address": serverIP, "port": p.Port, "version": 2},
		"streamSettings": stream,
	}
}

// Проверка сертификата: по другому имени (vcn) и/или по отпечатку (pcs, pinSHA256).
func applyCertChecks(tls obj, p *ProxyLink) {
	if p.VerifyName != "" {
		tls["verifyPeerCertByName"] = p.VerifyName
	}
	if p.PinnedCert != "" {
		tls["pinnedPeerCertSha256"] = p.PinnedCert
	}
}

func alpnList(alpn string) arr {
	var a arr
	for _, s := range strings.Split(alpn, ",") {
		if s = strings.TrimSpace(s); s != "" {
			a = append(a, s)
		}
	}
	return a
}

// jsonObject — JSON-объект из параметра ссылки; nil, если параметра нет или он испорчен.
func jsonObject(text string) obj {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var o obj
	if json.Unmarshal([]byte(text), &o) != nil {
		return nil
	}
	return o
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
