package app

import (
	"os"
	"strings"
	"testing"

	xserial "github.com/xtls/xray-core/infra/conf/serial"

	"vpnrouting/desktop/core"
)

// Собранные конфиги должно принимать само ядро Xray (как xray run -test в Android-версии):
// разбор JSON и сборка protobuf-конфига всех inbound/outbound, включая tun.
func TestXrayAcceptsConfigs(t *testing.T) {
	links := map[string]string{
		"reality": "vless://11111111-2222-3333-4444-555555555555@vpn.example.com:443?security=reality&type=tcp" +
			"&flow=xtls-rprx-vision&sni=www.microsoft.com&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2",
		"ws":   "vless://11111111-2222-3333-4444-555555555555@cdn.example.com:443?security=tls&type=ws&path=%2Fws&host=cdn.example.com",
		"grpc": "vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=tls&type=grpc&serviceName=grpc&sni=example.com",
		"xhttp": "vless://11111111-2222-3333-4444-555555555555@x.example.com:443?type=xhttp&security=tls&sni=x.example.com" +
			"&alpn=h2%2Chttp%2F1.1&mode=auto&path=%2Fx&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%226-12%22%7D%7D",
		"hy2": "hysteria2://AUTH@cdn.example.com:443/?sni=cdn.example.com&obfs=salamander&obfs-password=pw" +
			"&pinSHA256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	tuns := []core.TunOptions{
		{Name: "VPN routing", IPv6: true, SetDNS: true, BlockDNSLeak: true},
		{Name: "vpnrouting0", DNSToGateway: true},
	}
	for name, url := range links {
		link, err := core.ParseLink(url)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, tun := range tuns {
			cfg, err := core.BuildConfig(link, "93.184.216.34", "192.168.0.1", []string{"example.org", "195.19.0.0/16"}, core.AllBuiltins(),
				&core.BmstuRoute{Port: 41234, User: "u", Password: "p"}, core.AppRules{}, tun)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := xserial.LoadJSONConfig(strings.NewReader(cfg)); err != nil {
				t.Errorf("%s (%s): Xray не принял конфиг: %v", name, tun.Name, err)
			}
		}
		ping, err := core.BuildPingConfig(link, "93.184.216.34")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := xserial.LoadJSONConfig(strings.NewReader(ping)); err != nil {
			t.Errorf("%s: конфиг пинга: %v", name, err)
		}
	}
}

func TestStoreExceptionsAndServers(t *testing.T) {
	// Не t.TempDir(): на Windows антивирус иногда держит файлы, и очистка проваливает тест
	dir, err := os.MkdirTemp("", "vpnrouting-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.AddException("ya.ru"); v != "" {
		t.Fatal("ya.ru уже идёт напрямую (зона ru)")
	}
	_, _ = s.AddException("sub.example.com")
	v, _ := s.AddException("https://example.com/page")
	if v != "example.com" {
		t.Fatal(v)
	}
	if ex := s.Exceptions(); len(ex) != 1 || ex[0] != "example.com" {
		t.Fatalf("поддомен должен замениться родителем: %v", ex)
	}

	srv, err := s.AddServer("vless://id@a.example.com:443?security=tls&type=ws#A")
	if err != nil {
		t.Fatal(err)
	}
	s.SelectFirstIfNone()
	if s.ActiveURL() != srv.URL {
		t.Fatal("должен выбраться первый сервер")
	}
	sub := s.AddSubscription("https://panel.example.com/sub/abc")
	n := s.ApplySubscription(sub.ID, core.ParseSubscriptionBody(
		"vless://id@b.example.com:443?security=tls&type=ws#B\nhysteria2://x@c.example.com#C"))
	if n != 2 || len(s.Servers()) != 3 {
		t.Fatalf("%d %d", n, len(s.Servers()))
	}
	s.DisableBuiltin(".RU")
	if v, _ := s.AddException("ya.ru"); v != "ya.ru" {
		t.Fatal("после отключения зоны ru сайт можно добавить своим исключением")
	}
	if err := s.SetBmstuCredentials("user", "пароль"); err != nil {
		t.Fatal(err)
	}

	// Перечитать с диска
	s2, err := OpenStore(s.path[:len(s.path)-len("settings.json")])
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Servers()) != 3 || s2.BmstuPassword() != "пароль" || s2.ActiveURL() != srv.URL {
		t.Fatal("настройки не сохранились")
	}
	s2.RemoveSubscription(sub.ID)
	if len(s2.Servers()) != 1 {
		t.Fatal("серверы подписки должны удалиться вместе с ней")
	}
}
