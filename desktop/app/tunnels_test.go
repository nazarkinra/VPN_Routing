package app

import (
	"os"
	"testing"
)

func TestStoreTunnels(t *testing.T) {
	dir, err := os.MkdirTemp("", "vpnrouting-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := s.AddTunnel("")
	b := s.AddTunnel("Работа")
	if a.Name != "Туннель 2" || b.Name != "Работа" {
		t.Fatalf("названия: %q %q", a.Name, b.Name)
	}
	const url = "vless://id@nl.example.com:443?security=tls&type=ws#NL"
	if err := s.SetTunnel(a.ID, "Стриминг", url, true); err != nil {
		t.Fatal(err)
	}

	// Программа только в одном туннеле
	_ = s.SetTunnelApps(a.ID, []string{"Telegram.exe", "telegram.exe", "firefox"})
	_ = s.SetTunnelApps(b.ID, []string{"FIREFOX"})
	ts := s.Tunnels()
	if len(ts[0].Apps) != 1 || ts[0].Apps[0] != "Telegram.exe" || len(ts[1].Apps) != 1 {
		t.Fatalf("программа должна перейти во второй туннель: %v / %v", ts[0].Apps, ts[1].Apps)
	}

	// Домены: поддомен заменяется родителем, повтор не добавляется, чужой домен — ошибка
	_, _ = s.AddTunnelDomain(a.ID, "music.youtube.com")
	if v, err := s.AddTunnelDomain(a.ID, "https://youtube.com/watch?v=1"); err != nil || v != "youtube.com" {
		t.Fatal(v, err)
	}
	if v, _ := s.AddTunnelDomain(a.ID, "www.youtube.com"); v != "" {
		t.Fatal("уже покрыт youtube.com")
	}
	if _, err := s.AddTunnelDomain(b.ID, "m.youtube.com"); err == nil {
		t.Fatal("адрес уже идёт через туннель «Стриминг»")
	}
	if d := s.Tunnels()[0].Domains; len(d) != 1 || d[0] != "youtube.com" {
		t.Fatal(d)
	}
	if !s.Tunnels()[0].IsActive() || s.Tunnels()[1].IsActive() {
		t.Fatal("активен только туннель с сервером и правилами")
	}

	// Перечитать с диска
	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ts := s2.Tunnels(); len(ts) != 2 || ts[0].Name != "Стриминг" || ts[0].Server != url {
		t.Fatalf("туннели не сохранились: %+v", ts)
	}
	s2.RemoveTunnelDomain(a.ID, "youtube.com")
	s2.RemoveTunnel(b.ID)
	if ts := s2.Tunnels(); len(ts) != 1 || len(ts[0].Domains) != 0 {
		t.Fatalf("%+v", ts)
	}
}
