package netenv

import (
	"context"
	"testing"
)

func TestIPLiteralNeedsNoDNS(t *testing.T) {
	ip, method, err := ResolveServer(context.Background(), "203.0.113.10")
	if err != nil || ip != "203.0.113.10" || method != "IP из ключа" {
		t.Fatalf("%q %q %v", ip, method, err)
	}
	if !IsIPLiteral("2001:db8::1") || !IsIPLiteral("[2001:db8::1]") || IsIPLiteral("srv.example.com") {
		t.Fatal("IsIPLiteral")
	}
}

func TestParseDoHJSON(t *testing.T) {
	body := `{"Status":0,"Answer":[{"name":"a.example.","type":5,"data":"b.example."},` +
		`{"name":"b.example.","type":1,"TTL":60,"data":"203.0.113.10"}]}`
	if ip, err := ParseDoHJSON([]byte(body)); err != nil || ip != "203.0.113.10" {
		t.Fatalf("%q %v", ip, err)
	}
	if ip, _ := ParseDoHJSON([]byte(`{"Status":3}`)); ip != "" {
		t.Fatal(ip)
	}
}
