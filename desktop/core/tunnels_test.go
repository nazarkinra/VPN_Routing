package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Дополнительные туннели (2.1): выходы и порядок правил, как в Android-версии.

func tunnelRoutes(t *testing.T) []TunnelRoute {
	return []TunnelRoute{
		{Tag: "tunnel-1", Link: mustLink(t, "vless://id@nl.example.com:443?security=tls&type=ws&path=%2Fws#NL"),
			ServerIP: "5.6.7.8", Processes: []string{"Telegram.exe"}, Domains: []string{"youtube.com", "kinopoisk.ru"}},
		{Tag: "tunnel-2", Link: mustLink(t, "hysteria2://pass@us.example.com:443?sni=us.example.com#US"),
			ServerIP: "9.9.9.9", Processes: []string{"firefox"}, Domains: []string{"10.8.0.0/16"}},
	}
}

func buildRules(t *testing.T, apps AppRules, tunnels []TunnelRoute) (map[string]any, []map[string]any) {
	s, err := BuildConfig(mustLink(t, "vless://id@a.example.com:443?security=tls&type=ws#A"), "1.2.3.4",
		"192.168.0.1", []string{"example.org"}, AllBuiltins(), nil, apps, tunnels, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		t.Fatal(err)
	}
	var rules []map[string]any
	for _, r := range c["routing"].(map[string]any)["rules"].([]any) {
		rules = append(rules, r.(map[string]any))
	}
	return c, rules
}

func find(rules []map[string]any, pred func(r map[string]any) bool) int {
	for i, r := range rules {
		if pred(r) {
			return i
		}
	}
	return -1
}

func has(r map[string]any, key, sub string) bool {
	b, _ := json.Marshal(r[key])
	return strings.Contains(string(b), sub)
}

func TestTunnelsOutboundsAndRuleOrder(t *testing.T) {
	c, rules := buildRules(t, AppRules{}, tunnelRoutes(t))
	outs := c["outbounds"].([]any)
	tags := []string{}
	for _, o := range outs {
		tags = append(tags, o.(map[string]any)["tag"].(string))
	}
	if strings.Join(tags[:3], ",") != "proxy,tunnel-1,tunnel-2" {
		t.Fatalf("основной первым (маршрут по умолчанию), затем туннели: %v", tags)
	}
	if outs[2].(map[string]any)["protocol"] != "hysteria" {
		t.Fatal("туннель на Hysteria2")
	}

	domain := find(rules, func(r map[string]any) bool { return r["outboundTag"] == "tunnel-1" && r["domain"] != nil })
	ru := find(rules, func(r map[string]any) bool { return r["domain"] != nil && has(r, "domain", `"domain:ru"`) })
	quic := find(rules, func(r map[string]any) bool {
		return r["outboundTag"] == "block" && r["domain"] == nil && r["port"] == "443"
	})
	proc1 := find(rules, func(r map[string]any) bool { return r["outboundTag"] == "tunnel-1" && r["process"] != nil })
	cidr := find(rules, func(r map[string]any) bool { return r["outboundTag"] == "tunnel-2" && r["ip"] != nil })
	if domain < 0 || domain > ru || !has(rules[domain], "domain", "domain:kinopoisk.ru") {
		t.Fatalf("домены туннеля важнее «Напрямую»: domain=%d ru=%d", domain, ru)
	}
	if cidr < 0 || cidr > ru {
		t.Fatal("подсеть туннеля раньше «Напрямую»")
	}
	if proc1 < ru || proc1 < quic || !has(rules[proc1], "process", `"telegram.exe"`) {
		t.Fatalf("программы туннеля — после «Напрямую» и QUIC: proc=%d ru=%d quic=%d", proc1, ru, quic)
	}
}

func TestTunnelsWithAppModes(t *testing.T) {
	// «Кроме выбранных»: программа туннеля не уходит напрямую, даже если отмечена
	_, rules := buildRules(t, AppRules{Mode: AppsExclude, Names: []string{"telegram.exe", "steam.exe"}}, tunnelRoutes(t))
	direct := find(rules, func(r map[string]any) bool { return r["outboundTag"] == "direct" && r["process"] != nil })
	if direct < 0 || has(rules[direct], "process", "telegram") || !has(rules[direct], "process", "steam.exe") {
		t.Fatalf("исключение должно касаться только steam.exe: %v", rules[direct])
	}

	// «Только выбранные»: программы туннелей добавляются к выбранным, их правило — раньше маршрута по умолчанию
	_, rules = buildRules(t, AppRules{Mode: AppsInclude, Names: []string{"chrome.exe"}}, tunnelRoutes(t))
	def := find(rules, func(r map[string]any) bool { return r["outboundTag"] == "proxy" && r["process"] != nil })
	if def < 0 || !has(rules[def], "process", "firefox") || !has(rules[def], "process", "chrome.exe") {
		t.Fatalf("программы туннелей должны идти по правилам: %v", rules[def])
	}
	// (к правилу подсети туннеля в этом режиме тоже добавлено условие «процесс из выбранных» — его пропускаем)
	proc2 := find(rules, func(r map[string]any) bool {
		return r["outboundTag"] == "tunnel-2" && r["process"] != nil && r["ip"] == nil
	})
	if proc2 < 0 || proc2 > def || has(rules[proc2], "process", "chrome") {
		t.Fatalf("правило туннеля — только его программы и раньше маршрута по умолчанию: %d %d", proc2, def)
	}
}

func TestTunnelsIgnoredWithoutMainVPN(t *testing.T) {
	s, err := BuildConfig(nil, "", "192.168.0.1", nil, AllBuiltins(), &BmstuRoute{Port: 1080, User: "u", Password: "p"},
		AppRules{}, tunnelRoutes(t), testTun)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "tunnel-1") {
		t.Fatal("без основного VPN туннелей нет")
	}
}

func TestManyTunnels(t *testing.T) {
	var tunnels []TunnelRoute
	for i := 1; i <= 30; i++ {
		tunnels = append(tunnels, TunnelRoute{Tag: fmt.Sprintf("tunnel-%d", i),
			Link:     mustLink(t, "vless://id@nl.example.com:443?security=tls&type=ws#NL"),
			ServerIP: fmt.Sprintf("5.6.7.%d", i), Processes: []string{fmt.Sprintf("app%d.exe", i)},
			Domains: []string{fmt.Sprintf("site%d.com", i)}})
	}
	c, _ := buildRules(t, AppRules{}, tunnels)
	if n := len(c["outbounds"].([]any)); n != 30+1+3 {
		t.Fatalf("outbounds: %d", n)
	}
}
