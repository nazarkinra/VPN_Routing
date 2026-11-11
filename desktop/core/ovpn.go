package core

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Проверка и очистка .ovpn МГТУ (аналог bmstu-config.py и BmstuConfig.java).
// Из файла берутся только настройки из белого списка, остальные пропускаются.

const MaxOvpnSize = 100000

var (
	ovpnAllowedOptions = setOf("client", "tls-client", "remote", "port", "rport", "proto", "remote-random",
		"remote-cert-tls", "verify-x509-name", "reneg-sec", "tun-mtu", "mssfix", "fragment", "cipher", "ncp-ciphers",
		"data-ciphers", "data-ciphers-fallback", "auth", "tls-version-min", "tls-version-max", "tls-cipher",
		"tls-ciphersuites", "key-direction", "comp-lzo", "compress", "allow-compression",
		"explicit-exit-notify", "sndbuf", "rcvbuf", "connect-retry", "connect-timeout", "hand-window", "tran-window")
	ovpnAllowedBlocks = setOf("ca", "cert", "key", "extra-certs", "tls-auth", "tls-crypt", "tls-crypt-v2")
	// Задаются приложением, поэтому о них незачем предупреждать.
	ovpnReplacedOptions = setOf("dev", "dev-type", "auth-user-pass", "nobind", "resolv-retry", "ping",
		"ping-restart", "verb", "persist-key", "persist-tun", "pull", "auth-nocache", "route-noexec", "pull-filter",
		"script-security", "up", "down", "mute", "mute-replay-warnings")
	// Не поддерживаются клиентом в приложении (minivpn).
	ovpnUnsupportedBlocks = setOf("tls-auth", "tls-crypt", "tls-crypt-v2")
	ovpnFileRefs          = setOf("ca", "cert", "key", "tls-auth", "tls-crypt")

	notAfterRE = regexp.MustCompile(`^#\s*Not After\s*:\s*(.+)$`)
	blockRE    = regexp.MustCompile(`^<([A-Za-z0-9-]+)>$`)
)

func setOf(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range items {
		m[s] = true
	}
	return m
}

// OvpnConfig — очищенный профиль OpenVPN.
type OvpnConfig struct {
	Text       string
	Skipped    []string
	RemoteHost string
	NotAfter   string
}

// NotAfterDate — срок сертификата для экрана: «21.05.2027» (в файле — «May 21 00:00:00 2027 GMT»).
func (c *OvpnConfig) NotAfterDate() string {
	if c.NotAfter == "" {
		return ""
	}
	v := spaces.ReplaceAllString(c.NotAfter, " ")
	for _, layout := range []string{"Jan 2 15:04:05 2006 MST", "Jan _2 15:04:05 2006 MST"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("02.01.2006")
		}
	}
	return c.NotAfter
}

// ProtoAndPort — «UDP 2100» из очищенного .ovpn.
func (c *OvpnConfig) ProtoAndPort() string {
	proto, port := "UDP", "1194"
	isTCP := func(s string) bool { return strings.HasPrefix(strings.ToLower(s), "tcp") }
	for _, line := range strings.Split(c.Text, "\n") {
		p := strings.Fields(line)
		if len(p) == 0 {
			continue
		}
		switch strings.ToLower(p[0]) {
		case "proto":
			if len(p) > 1 {
				proto = map[bool]string{true: "TCP", false: "UDP"}[isTCP(p[1])]
			}
		case "port", "rport":
			if len(p) > 1 {
				port = p[1]
			}
		case "remote":
			if len(p) > 2 {
				port = p[2]
			}
			if len(p) > 3 {
				proto = map[bool]string{true: "TCP", false: "UDP"}[isTCP(p[3])]
			}
		}
	}
	return proto + " " + port
}

// ParseOvpn проверяет и очищает .ovpn.
func ParseOvpn(source string) (*OvpnConfig, error) {
	if source == "" {
		return nil, errors.New("Файл пуст.")
	}
	if len(source) > MaxOvpnSize {
		return nil, errors.New("Файл слишком большой для конфига OpenVPN.")
	}
	source = strings.ReplaceAll(source, string(rune(0xFEFF)), "")
	source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")

	var comments, options, blocks, skipped []string
	remoteHost, notAfter, blockName := "", "", ""
	var blockText strings.Builder

	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if blockName != "" {
			if strings.EqualFold(line, "</"+blockName+">") {
				if ovpnUnsupportedBlocks[blockName] {
					return nil, errors.New("Блок <" + blockName + "> пока не поддерживается клиентом OpenVPN в приложении.")
				}
				if ovpnAllowedBlocks[blockName] {
					blocks = append(blocks, "<"+blockName+">\n"+blockText.String()+"</"+blockName+">")
				} else {
					skipped = append(skipped, "<"+blockName+">")
				}
				blockName = ""
				blockText.Reset()
			} else {
				blockText.WriteString(line + "\n")
			}
			continue
		}
		if line == "" {
			continue
		}
		if line[0] == '#' || line[0] == ';' {
			// Срок сертификата из шапки показывается в приложении
			if m := notAfterRE.FindStringSubmatch(line); m != nil {
				notAfter = strings.TrimSpace(m[1])
				comments = append(comments, line)
			}
			continue
		}
		if m := blockRE.FindStringSubmatch(line); m != nil {
			blockName = strings.ToLower(m[1])
			continue
		}
		parts := strings.Fields(line)
		name := strings.TrimLeft(strings.ToLower(parts[0]), "-")
		if ovpnFileRefs[name] {
			return nil, errors.New("Настройка " + name + " ссылается на отдельный файл. Нужен .ovpn со встроенными сертификатами.")
		}
		if ovpnAllowedOptions[name] {
			options = append(options, line)
			if name == "remote" && remoteHost == "" && len(parts) >= 2 {
				remoteHost = parts[1]
			}
		} else if !ovpnReplacedOptions[name] {
			skipped = append(skipped, name)
		}
	}
	if blockName != "" {
		return nil, errors.New("Блок <" + blockName + "> не закрыт: файл повреждён.")
	}
	if remoteHost == "" {
		return nil, errors.New("В файле нет адреса сервера (remote). Похоже, это не конфиг OpenVPN.")
	}
	hasCA := false
	for _, b := range blocks {
		if strings.HasPrefix(b, "<ca>") {
			hasCA = true
		}
	}
	if !hasCA {
		return nil, errors.New("В файле нет сертификата <ca>. Похоже, это не конфиг OpenVPN.")
	}
	var out strings.Builder
	for _, s := range [][]string{comments, options, blocks} {
		for _, l := range s {
			out.WriteString(l + "\n")
		}
	}
	return &OvpnConfig{Text: out.String(), Skipped: skipped, RemoteHost: remoteHost, NotAfter: notAfter}, nil
}
