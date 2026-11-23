package app

import "testing"

const (
	rotOld = "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?type=ws&security=tls&path=%2Fws#NL"
	rotNew = "vless://22222222-2222-2222-2222-222222222222@vpn.example.com:443?type=ws&security=tls&path=%2Fws#NL"
	rotDE  = "vless://33333333-3333-3333-3333-333333333333@de.example.com:443?type=ws&security=tls&path=%2Fws#DE"
)

func servers(t *testing.T, urls ...string) []*Server {
	t.Helper()
	var out []*Server
	for i, u := range urls {
		s, err := newServer(string(rune('a'+i)), u, "sub")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestReplacementForRotatedKey(t *testing.T) {
	if got := replacementFor(rotOld, servers(t, rotDE, rotNew)); got != rotNew {
		t.Fatalf("got %q", got)
	}
	// ссылка на месте — менять нечего
	if got := replacementFor(rotOld, servers(t, rotOld, rotNew)); got != "" {
		t.Fatalf("got %q", got)
	}
	// другого такого же сервера нет
	if got := replacementFor(rotOld, servers(t, rotDE)); got != "" {
		t.Fatalf("got %q", got)
	}
	// неоднозначно — не угадываем
	dup := "vless://44444444-4444-4444-4444-444444444444@vpn.example.com:443?type=ws&security=tls&path=%2Fws#NL"
	if got := replacementFor(rotOld, servers(t, rotNew, dup)); got != "" {
		t.Fatalf("got %q", got)
	}
}
