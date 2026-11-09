package bmstuvpn

import (
	"testing"

	"github.com/ooni/minivpn/pkg/config"
)

// Структура как у bmstu.ovpn: proto не указан, шифр не указан, только <ca> и логин/пароль.
const sampleOvpn = `#Not After : May 21 00:00:00 2027 GMT
dev tun
port 2100
client
remote-cert-tls server
remote openvpn.bmstu.ru
reneg-sec 28800
auth-user-pass
tun-mtu 1492
mssfix 1400
<ca>
-----BEGIN CERTIFICATE-----
MIIB
-----END CERTIFICATE-----
</ca>
`

func TestParseBmstuLike(t *testing.T) {
	o, err := parseOptions(sampleOvpn, "user", "pass", "195.19.40.1")
	if err != nil {
		t.Fatal(err)
	}
	if o.Remote != "195.19.40.1" || o.Port != "2100" || o.Proto != config.ProtoUDP {
		t.Fatalf("remote: %s:%s/%s", o.Remote, o.Port, o.Proto)
	}
	if o.Cipher != "AES-256-GCM" || o.Auth != "SHA1" || o.Compress != "" {
		t.Fatalf("crypto: %s %s %q", o.Cipher, o.Auth, o.Compress)
	}
	if string(o.CA) != "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n" {
		t.Fatalf("ca: %q", o.CA)
	}
	if o.ServerOptionsString() != "V4,dev-type tun,link-mtu 1549,tun-mtu 1500,proto UDPv4,cipher AES-256-GCM,auth SHA1,keysize 256,key-method 2,tls-client" {
		t.Fatalf("options: %s", o.ServerOptionsString())
	}
}

func TestParseCiphersAndProto(t *testing.T) {
	o, err := parseOptions("remote h 443 tcp-client\ndata-ciphers CHACHA20-POLY1305:AES-128-GCM\nauth SHA256\n<ca>\nX\n</ca>", "u", "p", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if o.Port != "443" || o.Proto != config.ProtoTCP || o.Cipher != "AES-128-GCM" || o.Auth != "SHA256" {
		t.Fatalf("%s %s %s %s", o.Port, o.Proto, o.Cipher, o.Auth)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := parseOptions("remote h 1194", "u", "p", "1.2.3.4"); err == nil {
		t.Fatal("expected error without <ca>")
	}
	if _, err := parseOptions(sampleOvpn, "", "", "1.2.3.4"); err == nil {
		t.Fatal("expected error without credentials")
	}
}
