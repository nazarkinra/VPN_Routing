package core

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Тесты перенесены из Android-версии (app/src/test): те же входные данные и ожидания.

const (
	reality = "vless://11111111-2222-3333-4444-555555555555@vpn.example.com:443" +
		"?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com" +
		"&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&fp=chrome"
	wsTLS = "vless://11111111-2222-3333-4444-555555555555@cdn.example.com:443" +
		"?security=tls&type=ws&path=%2Fws&host=cdn.example.com&sni=cdn.example.com"
)

var testTun = TunOptions{Name: "vpnrouting0", IPv6: true, SetDNS: true, BlockDNSLeak: true}

func mustLink(t *testing.T, url string) *ProxyLink {
	t.Helper()
	l, err := ParseLink(url)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", url, err)
	}
	return l
}

func buildMap(t *testing.T, url string, bmstu *BmstuRoute) map[string]any {
	t.Helper()
	s, err := BuildConfig(mustLink(t, url), "93.184.216.34", "192.168.0.1",
		[]string{"bmstu.example", "195.19.0.0/16"}, AllBuiltins(), bmstu, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	if rules := m["routing"].(map[string]any)["rules"].([]any); len(rules) <= 5 {
		t.Fatalf("мало правил: %d", len(rules))
	}
	return m
}

func outbound(m map[string]any, i int) map[string]any {
	return m["outbounds"].([]any)[i].(map[string]any)
}

func get(m any, path ...string) any {
	for _, p := range path {
		m = m.(map[string]any)[p]
	}
	return m
}

func TestRealityConfig(t *testing.T) {
	m := buildMap(t, reality, nil)
	p := outbound(m, 0)
	if p["tag"] != "proxy" {
		t.Fatal("первый outbound — proxy")
	}
	if addr := get(p, "settings", "vnext").([]any)[0].(map[string]any)["address"]; addr != "93.184.216.34" {
		t.Fatalf("address %v", addr)
	}
	if _, ok := p["mux"]; ok {
		t.Fatal("mux несовместим с Vision")
	}
	if sn := get(p, "streamSettings", "realitySettings", "serverName"); sn != "www.microsoft.com" {
		t.Fatalf("serverName %v", sn)
	}
	routing, _ := json.Marshal(m["routing"])
	for _, want := range []string{"domain:ru", "domain:bmstu.example", "195.19.0.0/16"} {
		if !strings.Contains(string(routing), want) {
			t.Errorf("в правилах нет %s", want)
		}
	}
}

func TestWSConfig(t *testing.T) {
	p := outbound(buildMap(t, wsTLS, nil), 0)
	if _, ok := p["mux"]; !ok {
		t.Fatal("для ws нужен mux")
	}
	if path := get(p, "streamSettings", "wsSettings", "path"); path != "/ws" {
		t.Fatalf("path %v", path)
	}
}

func TestXhttpExtraAlpnAndFinalmask(t *testing.T) {
	url := "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?encryption=none&type=xhttp" +
		"&security=tls&sni=x.example.com&alpn=h2%2Chttp%2F1.1&mode=auto&path=%2Fx" +
		"&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%226-12%22%7D%7D" +
		"&fm=%7B%22tcp%22%3A%5B%7B%22type%22%3A%22fragment%22%7D%5D%7D#X"
	p := outbound(buildMap(t, url, nil), 0)
	if _, ok := p["mux"]; ok {
		t.Fatal("XHTTP мультиплексирует сам")
	}
	s := p["streamSettings"]
	if a := get(s, "tlsSettings", "alpn").([]any)[0]; a != "h2" {
		t.Fatalf("alpn %v", a)
	}
	if v := get(s, "xhttpSettings", "extra", "xmux", "maxConcurrency"); v != "6-12" {
		t.Fatalf("extra %v", v)
	}
	if v := get(s, "finalmask").(map[string]any)["tcp"].([]any)[0].(map[string]any)["type"]; v != "fragment" {
		t.Fatalf("finalmask %v", v)
	}
}

func TestBmstuRouteBeforeDirectRu(t *testing.T) {
	m := buildMap(t, reality, &BmstuRoute{Port: 41234, User: "u", Password: "p"})
	b := outbound(m, 4)
	if b["tag"] != "bmstu" {
		t.Fatal("пятый outbound — bmstu")
	}
	if port := get(b, "settings", "servers").([]any)[0].(map[string]any)["port"]; port != float64(41234) {
		t.Fatalf("port %v", port)
	}
	bmstuIdx, ruIdx := -1, -1
	for i, r := range get(m, "routing", "rules").([]any) {
		rule := r.(map[string]any)
		if rule["outboundTag"] == "bmstu" {
			bmstuIdx = i
		}
		if d, ok := rule["domain"]; ok {
			b, _ := json.Marshal(d)
			if strings.Contains(string(b), `"domain:ru"`) {
				ruIdx = i
			}
		}
	}
	if bmstuIdx < 0 || bmstuIdx > ruIdx {
		t.Fatalf("bmstu.ru должен проверяться раньше правила .ru: %d, %d", bmstuIdx, ruIdx)
	}
}

func TestNoBmstuRouteWhenDisabled(t *testing.T) {
	b, _ := json.Marshal(buildMap(t, reality, nil))
	if strings.Contains(string(b), `"bmstu"`) {
		t.Fatal("маршрута МГТУ быть не должно")
	}
}

func TestTunInboundDesktop(t *testing.T) {
	m := buildMap(t, reality, nil)
	in := m["inbounds"].([]any)[0].(map[string]any)
	s := in["settings"].(map[string]any)
	if s["autoOutboundsInterface"] != "auto" {
		t.Fatal("сокеты ядра должны идти мимо TUN")
	}
	routes, _ := json.Marshal(s["autoSystemRoutingTable"])
	if string(routes) != `["0.0.0.0/0","::/0"]` {
		t.Fatalf("маршруты %s", routes)
	}
	gw, _ := json.Marshal(s["gateway"])
	if !strings.Contains(string(gw), TunAddress+"/30") {
		t.Fatalf("gateway %s", gw)
	}
}

// ---------- RoutingRulesTest ----------

func TestBuiltinListHasNoRedundantEntries(t *testing.T) {
	if !reflect.DeepEqual(Compact(DirectDomains), DirectDomains) {
		t.Fatal("встроенный список уже без повторов")
	}
	for _, d := range DirectDomains {
		if strings.HasSuffix(d, ".ru") {
			t.Fatal(d)
		}
	}
}

func TestCompactRemovesChildrenAndSpellings(t *testing.T) {
	out := Compact([]string{".ru", "ya.ru", "рф", "xn--p1ai", "госуслуги.рф", "Vk.com", "m.vk.com", "vk.com."})
	if !reflect.DeepEqual(out, []string{"ru", "xn--p1ai", "vk.com"}) {
		t.Fatalf("%v", out)
	}
}

func TestBuiltinCoverage(t *testing.T) {
	for _, d := range []string{"ya.ru", "gosuslugi.ru", "госуслуги.рф", "sun9-1.userapi.com"} {
		if !IsBuiltinDirect(d) {
			t.Error(d)
		}
	}
	if IsBuiltinDirect("example.com") {
		t.Error("example.com")
	}
}

func TestXrayConfigHasNoDuplicateDomains(t *testing.T) {
	s, err := BuildConfig(mustLink(t, "vless://id@a.example.com:443?security=tls&type=ws#A"), "1.2.3.4", "",
		[]string{"ya.ru", "example.com", "sub.example.com"}, AllBuiltins(), nil, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "domain:ya.ru") || strings.Contains(s, "domain:sub.example.com") || !strings.Contains(s, "domain:example.com") {
		t.Fatal("повторы доменов в конфиге")
	}
}

// ---------- HysteriaTest ----------

func TestHysteriaParsesPanelLink(t *testing.T) {
	l := mustLink(t, "hysteria2://secret-auth@cdn.example.com:443/?sni=cdn.example.com"+
		"&fm=%7B%22quicParams%22%3A%7B%22keepAlivePeriod%22%3A10%7D%7D#%F0%9F%87%B3%F0%9F%87%B1%20Gaming")
	if !l.IsHysteria() || l.UUID != "secret-auth" || l.Server != "cdn.example.com" || l.Port != 443 {
		t.Fatalf("%+v", l)
	}
	if l.Finalmask != `{"quicParams":{"keepAlivePeriod":10}}` || l.Name != "🇳🇱 Gaming" {
		t.Fatalf("%q %q", l.Finalmask, l.Name)
	}
}

func TestHysteriaShortScheme(t *testing.T) {
	l := mustLink(t, "hy2://user:pass@1.2.3.4?obfs=salamander&obfs-password=xyz&pinSHA256=AB:CD:EF#X")
	if l.UUID != "user:pass" || l.Port != 443 || l.Obfs != "salamander" || l.PinnedCert != "abcdef" {
		t.Fatalf("%+v", l)
	}
}

func TestHysteriaRejectsPortHopping(t *testing.T) {
	if _, err := ParseLink("hysteria2://auth@example.com:20000-30000/?sni=example.com"); err == nil {
		t.Fatal("port hopping не поддерживается")
	}
}

func TestHysteriaOutbound(t *testing.T) {
	l := mustLink(t, "hysteria2://AUTH@cdn.example.com:443/?sni=cdn.example.com&obfs=salamander"+
		"&obfs-password=pw&pinSHA256=abcd&fm=%7B%22quicParams%22%3A%7B%22keepAlivePeriod%22%3A10%7D%7D#G")
	s, err := BuildConfig(l, "93.184.216.34", "", nil, AllBuiltins(), nil, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	out := outbound(m, 0)
	if out["protocol"] != "hysteria" {
		t.Fatal(out["protocol"])
	}
	if _, ok := out["mux"]; ok {
		t.Fatal("Mux поверх QUIC не нужен")
	}
	if get(out, "settings", "address") != "93.184.216.34" || get(out, "settings", "version") != float64(2) {
		t.Fatal("settings")
	}
	st := out["streamSettings"]
	if get(st, "network") != "hysteria" || get(st, "hysteriaSettings", "auth") != "AUTH" {
		t.Fatal("stream")
	}
	if get(st, "tlsSettings", "serverName") != "cdn.example.com" || get(st, "tlsSettings", "alpn").([]any)[0] != "h3" ||
		get(st, "tlsSettings", "pinnedPeerCertSha256") != "abcd" {
		t.Fatal("tls")
	}
	if get(st, "finalmask", "quicParams", "keepAlivePeriod") != float64(10) {
		t.Fatal("quicParams")
	}
	udp := get(st, "finalmask").(map[string]any)["udp"].([]any)[0].(map[string]any)
	if udp["type"] != "salamander" || get(udp, "settings", "password") != "pw" {
		t.Fatal("salamander")
	}
}

// ---------- SubscriptionTest ----------

const (
	subA = "vless://11111111-2222-3333-4444-555555555555@a.example.com:443" +
		"?security=reality&type=tcp&pbk=KEY&sni=x.com#%F0%9F%87%A9%F0%9F%87%AA%20DE"
	subB = "vless://11111111-2222-3333-4444-555555555555@b.example.com:443?type=ws&security=tls#NL"
)

func TestSubscriptionPlainList(t *testing.T) {
	r := ParseSubscriptionBody(subA + "\n" + subB + "\nvmess://abc\n\n# comment\n" + subA)
	if len(r.Links) != 2 || r.Unsupported != 1 {
		t.Fatalf("%d %d", len(r.Links), r.Unsupported)
	}
}

func TestSubscriptionBase64Body(t *testing.T) {
	r := ParseSubscriptionBody(base64.StdEncoding.EncodeToString([]byte(subA + "\r\n" + subB)))
	if len(r.Links) != 2 || r.Links[0] != subA {
		t.Fatalf("%v", r.Links)
	}
}

func TestSubscriptionURLSafeBase64(t *testing.T) {
	if d, ok := DecodeBase64(base64.RawURLEncoding.EncodeToString([]byte(subB))); !ok || d != subB {
		t.Fatalf("%q", d)
	}
	if _, ok := DecodeBase64("не base64!"); ok {
		t.Fatal("не base64")
	}
}

func TestSubscriptionHeaders(t *testing.T) {
	r := &SubscriptionResult{}
	title := "base64:" + base64.StdEncoding.EncodeToString([]byte("Мой VPN"))
	ApplySubscriptionHeaders(r, title, "upload=1024; download=2048; total=107374182400; expire=1798675200", "12", "")
	if r.Title != "Мой VPN" || r.Upload != 1024 || r.Download != 2048 || r.Total != 107374182400 ||
		r.Expire != 1798675200 || r.UpdateIntervalHours != 12 {
		t.Fatalf("%+v", r)
	}
}

func TestSubscriptionAcceptsHysteria(t *testing.T) {
	r := ParseSubscriptionBody("vless://id@a.example.com:443?security=tls&type=ws#A\n" +
		"hysteria2://auth@b.example.com:443/?sni=b.example.com#B\n" +
		"trojan://x@c.example.com:443#C")
	if len(r.Links) != 2 || r.Unsupported != 1 {
		t.Fatalf("%d %d", len(r.Links), r.Unsupported)
	}
}

// ---------- BmstuConfigTest ----------

const testOvpn = "#Not Before: May 21 02:36:52 2025 GMT\n" +
	"#Not After : May 21 00:00:00 2027 GMT\n" +
	"dev tun\nport 2100\nclient\nremote-cert-tls server\nremote openvpn.bmstu.ru\n" +
	"reneg-sec 28800\nauth-user-pass\n# tun-mtu 1500\ntun-mtu 1492\nmssfix 1400\n" +
	"up /bin/sh\nplugin evil.so\n" +
	"<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>\n"

func TestOvpnKeepsWhitelistAndDropsCommands(t *testing.T) {
	c, err := ParseOvpn(testOvpn)
	if err != nil {
		t.Fatal(err)
	}
	if c.RemoteHost != "openvpn.bmstu.ru" || c.NotAfter != "May 21 00:00:00 2027 GMT" {
		t.Fatalf("%q %q", c.RemoteHost, c.NotAfter)
	}
	if c.NotAfterDate() != "21.05.2027" {
		t.Fatalf("дата %q", c.NotAfterDate())
	}
	if !strings.Contains(c.Text, "port 2100\n") ||
		!strings.Contains(c.Text, "<ca>\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n</ca>") {
		t.Fatal(c.Text)
	}
	for _, bad := range []string{"up /bin/sh", "plugin", "dev tun"} {
		if strings.Contains(c.Text, bad) {
			t.Error(bad)
		}
	}
	if !reflect.DeepEqual(c.Skipped, []string{"plugin"}) {
		t.Fatalf("skipped %v", c.Skipped)
	}
	if c.ProtoAndPort() != "UDP 2100" {
		t.Fatal(c.ProtoAndPort())
	}
}

func TestOvpnRejects(t *testing.T) {
	for _, src := range []string{
		"client\nremote host 1194\n",
		"remote host 1194\nca /etc/ca.crt\n",
		"remote host 1194\n<ca>\nX\n</ca>\n<tls-crypt>\nY\n</tls-crypt>\n",
	} {
		if _, err := ParseOvpn(src); err == nil {
			t.Errorf("должен отклонить: %q", src)
		}
	}
}

// ---------- Исключения ----------

func TestNormalizeException(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/path?q=1": "example.com",
		"sub.example.com:8080/x":       "sub.example.com",
		"госуслуги.рф":                 "xn--c1aapkosapc.xn--p1ai",
		".example":                     ".example",
		"1.2.3.4":                      "1.2.3.4/32",
		"10.1.2.3/8":                   "10.0.0.0/8",
		"2001:db8::1/32":               "2001:db8::/32",
		"[2001:db8::5]":                "2001:db8::5/128",
	}
	for in, want := range cases {
		got, err := NormalizeException(in)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v (ждали %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "not a domain", "-bad-.com"} {
		if _, err := NormalizeException(bad); err == nil {
			t.Errorf("должен отклонить %q", bad)
		}
	}
	if DisplayDomain("xn--c1aapkosapc.xn--p1ai") != "госуслуги.рф" || DisplayDomain("1.2.3.4/32") != "1.2.3.4" {
		t.Error("DisplayDomain")
	}
}

// ---------- UpdateAndBuiltinsTest ----------

func TestWithoutRemovesDisabledBuiltins(t *testing.T) {
	left := Without(DirectDomains, []string{".ru", "рф", "VK.com"})
	for _, gone := range []string{"ru", "xn--p1ai", "vk.com"} {
		for _, d := range left {
			if d == gone {
				t.Fatalf("%s должен быть убран", gone)
			}
		}
	}
	if left[0] != "su" {
		t.Fatalf("%v", left)
	}
}

func TestConfigWithoutDisabledZone(t *testing.T) {
	b := Builtins{Direct: Without(DirectDomains, []string{"ru"}), Tunnel: DirectTunnelDomains}
	s, err := BuildConfig(mustLink(t, "vless://id@a.example.com:443?security=tls&type=ws#A"), "1.2.3.4", "", nil, b, nil, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, `"domain:ru"`) || !strings.Contains(s, `"domain:su"`) {
		t.Fatal("зона ru должна исчезнуть, su — остаться")
	}
}

func TestConfigWithAllBuiltinsRemoved(t *testing.T) {
	s, err := BuildConfig(mustLink(t, "vless://id@a.example.com:443?security=tls&type=ws#A"), "1.2.3.4", "", nil,
		Builtins{}, nil, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	for _, r := range get(m, "routing", "rules").([]any) {
		if d, ok := r.(map[string]any)["domain"]; ok && len(d.([]any)) == 0 {
			t.Fatal("правило с пустым списком доменов")
		}
	}
	// Без доменов DNS сети не нужен: остаётся только DNS через VPN
	if n := len(get(m, "dns", "servers").([]any)); n != len(ProxyDNS) {
		t.Fatalf("серверов DNS: %d", n)
	}
}

// ---------- CorpNetworkTest ----------

const corpKey = "vless://11111111-2222-3333-4444-555555555555@srv.example.com:443" +
	"?security=reality&type=tcp&flow=xtls-rprx-vision&sni=www.google.com&pbk=PUBKEY&sid=abcd&fp=chrome#R"

func corpConfig(t *testing.T, localDNS string, bmstu *BmstuRoute) (string, map[string]any) {
	t.Helper()
	s, err := BuildConfig(mustLink(t, corpKey), "203.0.113.10", localDNS, nil, AllBuiltins(), bmstu, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return s, m
}

// Ни один DNS-сервер не ходит на внешний адрес по UDP 53 — кроме DNS, выданного сетью.
func TestNoPlainUDPDNSToExternalResolvers(t *testing.T) {
	for _, local := range []string{"195.19.33.202", ""} {
		_, m := corpConfig(t, local, nil)
		for _, s := range get(m, "dns", "servers").([]any) {
			srv := s.(map[string]any)
			addr := srv["address"].(string)
			if !strings.Contains(addr, "://") && addr != "195.19.33.202" {
				t.Errorf("обычный DNS только от сети: %s", addr)
			}
			if ms, _ := srv["timeoutMs"].(float64); ms == 0 || ms > 3000 {
				t.Errorf("тайм-аут %v у %s", srv["timeoutMs"], addr)
			}
		}
	}
}

// DNS сети — первым для российских доменов, затем запасные TCP и DoH в обход туннеля.
func TestNetworkDNSFirstThenTCPAndDoH(t *testing.T) {
	_, m := corpConfig(t, "195.19.33.202", nil)
	servers := get(m, "dns", "servers").([]any)
	want := []string{"195.19.33.202", "tcp+local://77.88.8.8", "https+local://1.1.1.1/dns-query",
		"tcp://8.8.8.8", "https://1.1.1.1/dns-query"}
	if len(servers) != len(want) {
		t.Fatalf("серверов %d", len(servers))
	}
	for i, w := range want {
		if a := servers[i].(map[string]any)["address"]; a != w {
			t.Errorf("%d: %v, ждали %s", i, a, w)
		}
	}
	if _, ok := servers[3].(map[string]any)["domains"]; ok {
		t.Error("DNS через VPN — для всех доменов")
	}
}

// Без DNS сети нет и правила «DNS-модуль → напрямую» с пустым списком адресов.
func TestNoEmptyDirectDNSRule(t *testing.T) {
	_, m := corpConfig(t, "", nil)
	for _, r := range get(m, "routing", "rules").([]any) {
		if ip, ok := r.(map[string]any)["ip"]; ok && len(ip.([]any)) == 0 {
			t.Fatal("правило с пустым списком адресов")
		}
	}
}

func TestTunMTUIs1400(t *testing.T) {
	_, m := corpConfig(t, "", nil)
	if mtu := m["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["mtu"]; mtu != float64(1400) {
		t.Fatalf("mtu %v", mtu)
	}
}

func TestDumpHidesSecrets(t *testing.T) {
	s, _ := corpConfig(t, "", &BmstuRoute{Port: 1080, User: "u-secret", Password: "p-secret"})
	masked, err := MaskConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"11111111-2222", "PUBKEY", `"abcd"`, "p-secret", "u-secret"} {
		if strings.Contains(masked, secret) {
			t.Errorf("в копии конфига виден %s", secret)
		}
	}
	if !strings.Contains(masked, "203.0.113.10") {
		t.Error("адрес сервера должен остаться")
	}
}

func appsConfig(t *testing.T, link *ProxyLink, apps AppRules) []map[string]any {
	t.Helper()
	s, err := BuildConfig(link, "203.0.113.10", "195.19.33.202", nil, AllBuiltins(),
		&BmstuRoute{Port: 1080, User: "u", Password: "p"}, apps, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	var rules []map[string]any
	for _, r := range get(m, "routing", "rules").([]any) {
		rules = append(rules, r.(map[string]any))
	}
	return rules
}

func hasProcess(r map[string]any, name string) bool {
	list, _ := r["process"].([]any)
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}

// «Кроме выбранных»: выбранные программы — сразу напрямую, до МГТУ и остальных правил.
func TestAppsExclude(t *testing.T) {
	rules := appsConfig(t, mustLink(t, corpKey), AppRules{Mode: AppsExclude, Names: []string{"Telegram.exe"}})
	first := -1
	for i, r := range rules {
		if hasProcess(r, "Telegram.exe") {
			if r["outboundTag"] != "direct" {
				t.Fatalf("исключённые приложения — напрямую, а не %v", r["outboundTag"])
			}
			if !hasProcess(r, "telegram.exe") {
				t.Error("нет варианта имени в нижнем регистре")
			}
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("нет правила по процессам")
	}
	for i, r := range rules {
		if r["outboundTag"] == "bmstu" && i < first {
			t.Error("правило МГТУ раньше исключения приложений")
		}
		if r["outboundTag"] == "dns-out" && i > first {
			t.Error("перехват DNS должен идти раньше правил по приложениям")
		}
	}
}

// «Только выбранные»: правила — с условием процесса, выбранные по умолчанию — через VPN, прочие — напрямую.
func TestAppsInclude(t *testing.T) {
	rules := appsConfig(t, mustLink(t, corpKey), AppRules{Mode: AppsInclude, Names: []string{"firefox"}})
	n := len(rules)
	last, prev := rules[n-1], rules[n-2]
	if last["outboundTag"] != "direct" || last["process"] != nil || last["network"] != "tcp,udp" {
		t.Fatalf("последнее правило — всё остальное напрямую: %v", last)
	}
	if prev["outboundTag"] != "proxy" || !hasProcess(prev, "firefox") {
		t.Fatalf("выбранные по умолчанию — через VPN: %v", prev)
	}
	for _, r := range rules[:n-2] {
		switch r["outboundTag"] {
		case "bmstu", "block":
			if !hasProcess(r, "firefox") {
				t.Errorf("правило %v без условия процесса", r)
			}
		case "dns-out":
			if r["process"] != nil {
				t.Error("перехват DNS — для всех приложений")
			}
		}
	}
	// Без основного VPN выбранные по умолчанию — тоже напрямую
	rules = appsConfig(t, nil, AppRules{Mode: AppsInclude, Names: []string{"firefox"}})
	if r := rules[len(rules)-2]; r["outboundTag"] != "direct" {
		t.Fatalf("без основного VPN выхода proxy нет: %v", r)
	}
	// Пустой список — как «все приложения»
	for _, r := range appsConfig(t, mustLink(t, corpKey), AppRules{Mode: AppsInclude}) {
		if r["process"] != nil {
			t.Fatal("пустой список не должен добавлять правил по процессам")
		}
	}
}

// Только МГТУ: основного сервера нет, bmstu.ru — в туннель, остальное и DNS — напрямую.
func TestCorpOnlyConfig(t *testing.T) {
	s, err := BuildConfig(nil, "", "195.19.33.202", []string{"example.org"}, AllBuiltins(),
		&BmstuRoute{Port: 1080, User: "u", Password: "p"}, AppRules{}, nil, testTun)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	outs := get(m, "outbounds").([]any)
	if tag := outs[0].(map[string]any)["tag"]; tag != "direct" {
		t.Fatalf("маршрут по умолчанию %v, нужен direct", tag)
	}
	for _, o := range outs {
		if o.(map[string]any)["tag"] == "proxy" {
			t.Fatal("выхода proxy быть не должно")
		}
	}
	if !strings.Contains(s, `"outboundTag": "bmstu"`) {
		t.Fatal("нет маршрута bmstu.ru в туннель МГТУ")
	}
	servers := get(m, "dns", "servers").([]any)
	want := []string{"195.19.33.202", "tcp+local://77.88.8.8", "https+local://1.1.1.1/dns-query"}
	if len(servers) != len(want) {
		t.Fatalf("серверов DNS %d", len(servers))
	}
	for i, w := range want {
		srv := servers[i].(map[string]any)
		if srv["address"] != w || srv["domains"] != nil {
			t.Errorf("DNS %d: %v", i, srv)
		}
	}
	// QUIC к остальным сайтам не блокируется: без основного VPN он идёт напрямую
	n := 0
	for _, r := range get(m, "routing", "rules").([]any) {
		if r.(map[string]any)["outboundTag"] == "block" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("правил block %d, нужно одно (QUIC к МГТУ)", n)
	}
}
