//go:build !windows

package app

import "encoding/base64"

// На Linux настройки лежат в /var/lib/vpnrouting с правами 0600 (читает только root), как
// /etc/openvpn/bmstu.auth на Raspberry Pi. Пароль хранится в base64 — от случайного взгляда.

func protectSecret(plain string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

func unprotectSecret(stored string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(stored)
	return string(b), err
}
