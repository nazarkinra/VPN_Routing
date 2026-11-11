package core

import (
	"strings"

	"golang.org/x/net/idna"
)

// Встроенные правила маршрутизации — те же, что в routing/configs/sing-box.json.template
// и RoutingRules.java Android-версии.

// DirectDomains — домены напрямую (вместе с поддоменами) и через DNS домашней сети.
// Сайты в зонах .ru/.su/.рф/.moscow сюда не входят: их уже покрывает зона.
var DirectDomains = []string{
	// Доменные зоны (xn--p1ai — это .рф)
	"ru", "su", "xn--p1ai", "moscow",
	// Яндекс
	"yandex.net", "yandex.com",
	// VK и его CDN
	"vk.com", "vk.me", "userapi.com", "vk-portal.net", "vk-apps.com", "vkuser.net", "vkuserlive.com",
	"vkuseraudio.net", "vkuseraudio.com", "vkuservideo.net", "vkuservideo.com",
	// Банки, госсервисы, прочее
	"sberbank.com", "emias.info", "habr.com", "kaspersky.com", "drweb.com", "crealitycloud.com",
}

// DirectTunnelDomains — туннели Cloudflare работают только напрямую.
var DirectTunnelDomains = []string{"argotunnel.com", "trycloudflare.com"}

// PrivateCIDRs — локальные и служебные сети.
var PrivateCIDRs = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8",
	"169.254.0.0/16", "100.64.0.0/10", "224.0.0.0/4", "255.255.255.255/32",
	"fc00::/7", "fe80::/10", "::1/128",
}

// DirectPorts — порты VPN-протоколов и Cloudflare Tunnel: их трафик не заворачивается в прокси.
const DirectPorts = "2100,7844,1194,51820,500,4500,1701,1723,10808,10809"

// FallbackLocalDNS — запасные DNS для российских доменов и исключений, напрямую в обход VPN: на случай,
// если DNS сети не определился или не отвечает. Только TCP и DoH по IP: в корпоративных сетях (МГТУ)
// обычный DNS по UDP 53 на внешние серверы заблокирован. «+local» — запрос идёт напрямую, мимо правил.
var FallbackLocalDNS = []string{"tcp+local://77.88.8.8", "https+local://1.1.1.1/dns-query"}

// ProxyDNS — DNS для остальных доменов: через VPN, по TCP, затем DoH — от политики сети не зависит.
var ProxyDNS = []string{"tcp://8.8.8.8", "https://1.1.1.1/dns-query"}

// DNSTimeoutMs — сколько ждать ответа одного DNS-сервера, прежде чем спросить следующий.
const DNSTimeoutMs = 2500

// IsBuiltinDirect — исключение уже покрыто встроенными правилами.
func IsBuiltinDirect(value string) bool {
	return CoveredBy(Canonical(value), DirectDomains)
}

// Canonical — домен в одном виде: без точек по краям, строчными, кириллица — в punycode.
func Canonical(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.Trim(d, ".")
	if a, err := idna.Punycode.ToASCII(d); err == nil {
		d = strings.ToLower(a)
	}
	return d
}

// Without — список без отключённых записей (сравнение в каноническом виде).
func Without(domains, disabled []string) []string {
	off := map[string]bool{}
	for _, d := range disabled {
		off[Canonical(d)] = true
	}
	result := []string{}
	for _, d := range domains {
		if !off[Canonical(d)] {
			result = append(result, d)
		}
	}
	return result
}

// CoveredBy — domain совпадает с одним из parents или является его поддоменом.
func CoveredBy(domain string, parents []string) bool {
	for _, p := range parents {
		parent := Canonical(p)
		if parent != "" && (domain == parent || strings.HasSuffix(domain, "."+parent)) {
			return true
		}
	}
	return false
}

// Compact — список доменов без повторов: домены, у которых в списке есть родитель
// (ya.ru при ru), выбрасываются. Порядок первых вхождений сохраняется.
func Compact(domains []string) []string {
	var canon []string
	seen := map[string]bool{}
	for _, d := range domains {
		c := Canonical(d)
		if c != "" && !seen[c] {
			seen[c] = true
			canon = append(canon, c)
		}
	}
	var result []string
	for _, d := range canon {
		covered := false
		for _, other := range canon {
			if other != d && strings.HasSuffix(d, "."+other) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, d)
		}
	}
	return result
}

// DisplayDomain — кириллические домены показываются по-русски, /32 и /128 — без префикса.
func DisplayDomain(value string) string {
	if IsCIDR(value) {
		if strings.HasSuffix(value, "/32") || strings.HasSuffix(value, "/128") {
			return value[:strings.LastIndexByte(value, '/')]
		}
		return value
	}
	prefix := ""
	if strings.HasPrefix(value, ".") {
		prefix = "."
	}
	if u, err := idna.Punycode.ToUnicode(value[len(prefix):]); err == nil {
		return prefix + u
	}
	return value
}
