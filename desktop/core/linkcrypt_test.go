package core

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Вектор testdata/linkcrypt_vector.txt зашифрован сервером подписок (vpn_corp,
// subs/happ_crypt.build_app_link) тестовым ключом — проверяется совместимость с ним.
func linkVector(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/linkcrypt_vector.txt")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l) // CRLF после checkout на Windows
		if l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestDecryptServerLinks(t *testing.T) {
	v := linkVector(t)
	for _, i := range []int{1, 3} { // короткий адрес и адрес на несколько RSA-блоков
		got, err := decryptLinkWithKey(v[i+1], v[0])
		if err != nil || got != v[i] {
			t.Fatalf("got %q, %v", got, err)
		}
	}
}

func TestCryptLinkScheme(t *testing.T) {
	v := linkVector(t)
	if !IsCryptLink(" "+v[2]+"\n") || IsCryptLink("https://x/sub") || IsCryptLink("happ://crypt4/abc") {
		t.Fatal("распознавание схемы")
	}
	if IsSubscriptionURL(v[2]) {
		t.Fatal("зашифрованная ссылка не должна считаться обычным адресом подписки")
	}
}

func TestDamagedOrForeignLinkRejected(t *testing.T) {
	v := linkVector(t)
	if _, err := decryptLinkWithKey(v[2][:len(v[2])-10], v[0]); err == nil {
		t.Fatal("обрезанная ссылка принята")
	}
	k, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	if _, err := decryptLinkWithKey(v[2], base64.StdEncoding.EncodeToString(der)); err == nil {
		t.Fatal("ссылка расшифрована чужим ключом")
	}
}

func TestStoredCryptSubscriptionSendsHwid(t *testing.T) {
	var gotHwid string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHwid = r.Header.Get("X-Hwid")
		if gotHwid == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("vless://11111111-2222-3333-4444-555555555555@b.example.com:443?type=ws&security=tls#NL"))
	}))
	defer srv.Close()

	// Своя пара ключей: адрес тестового сервера и зашифрованная ссылка на него
	k, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, &k.PublicKey, []byte(srv.URL+"/s7k2vq9m/tok"))
	if err != nil {
		t.Fatal(err)
	}
	link := CryptLinkPrefix + base64.RawURLEncoding.EncodeToString(ct)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	old := linkPrivateKey
	linkPrivateKey = base64.StdEncoding.EncodeToString(der)
	defer func() { linkPrivateKey = old }()

	r, err := FetchStoredSubscription(context.Background(), srv.Client(), link, "0123456789abcdef0123456789abcdef")
	if err != nil || len(r.Links) != 1 || gotHwid != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("links=%v hwid=%q err=%v", r, gotHwid, err)
	}
	// Без X-Hwid сервер отвечает 404 — сообщение про привязку, а не «проверьте адрес»
	_, err = FetchStoredSubscription(context.Background(), srv.Client(), link, "")
	if err == nil {
		t.Fatal("ожидалась ошибка без X-Hwid")
	}
}

func TestBuildWithoutKeyRefusesCryptLinks(t *testing.T) {
	old := linkPrivateKey
	linkPrivateKey = ""
	defer func() { linkPrivateKey = old }()
	if _, err := DecryptLink(linkVector(t)[2]); err == nil || !strings.Contains(err.Error(), "обновите") {
		t.Fatalf("err=%v", err)
	}
}
