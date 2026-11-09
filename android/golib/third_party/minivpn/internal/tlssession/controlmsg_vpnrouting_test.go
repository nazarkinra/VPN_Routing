package tlssession

import (
	"errors"
	"strings"
	"testing"

	"github.com/apex/log"
	"github.com/ooni/minivpn/internal/session"
	"github.com/ooni/minivpn/pkg/config"
)

// Правки VPN routing: причина AUTH_FAILED и объявление шифра серверу.

func TestAuthFailedKeepsServerReason(t *testing.T) {
	_, err := parseServerPushReply(log.Log, []byte("AUTH_FAILED,Data channel cipher negotiation failed (no shared cipher)\x00"))
	if !errors.Is(err, ErrBadAuth) {
		t.Fatalf("expected ErrBadAuth, got %v", err)
	}
	if !strings.Contains(err.Error(), "no shared cipher") {
		t.Fatalf("reason lost: %v", err)
	}
	_, err = parseServerPushReply(log.Log, []byte("AUTH_FAILED\x00"))
	if err == nil || err.Error() != "AUTH_FAILED" {
		t.Fatalf("plain AUTH_FAILED: %v", err)
	}
}

func TestPeerInfoAnnouncesCipher(t *testing.T) {
	ks, err := session.NewKeySource()
	if err != nil {
		t.Fatal(err)
	}
	o := &config.OpenVPNOptions{Cipher: "AES-256-GCM", Auth: "SHA1", Proto: config.ProtoUDP, Username: "u", Password: "p"}
	msg, err := encodeClientControlMessageAsBytes(ks, o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "IV_CIPHERS=AES-256-GCM\n") || !strings.Contains(string(msg), "IV_NCP=2\n") {
		t.Fatal("peer info must announce the cipher")
	}
}
