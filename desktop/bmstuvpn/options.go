package bmstuvpn

import (
	"errors"
	"strings"

	"github.com/ooni/minivpn/pkg/config"
)

// Шифры, которые умеет minivpn. AEAD (GCM) — в приоритете: их выбирает современный сервер OpenVPN.
var supportedCiphers = []string{"AES-256-GCM", "AES-128-GCM", "AES-192-GCM", "AES-256-CBC", "AES-128-CBC", "AES-192-CBC"}

func supported(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// parseOptions разбирает .ovpn (уже очищенный приложением от опасных директив).
// Адрес сервера подставляется отдельно: имя разрешает приложение, Go-резолвер на Android ненадёжен.
func parseOptions(text, username, password, remoteIP string) (*config.OpenVPNOptions, error) {
	o := &config.OpenVPNOptions{
		Port:     "1194",
		Proto:    config.ProtoUDP, // по умолчанию OpenVPN работает по UDP
		Username: username,
		Password: password,
		Auth:     "SHA1",
		// Пустое значение: без байта сжатия в пакетах и без "compress" в строке опций
		Compress: config.Compression(""),
	}
	var cipher, dataCiphers string
	var block string
	var ca strings.Builder

	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if block != "" {
			if strings.EqualFold(line, "</"+block+">") {
				block = ""
			} else if block == "ca" {
				ca.WriteString(line + "\n")
			}
			continue
		}
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			block = strings.ToLower(strings.Trim(line, "<>"))
			continue
		}
		f := strings.Fields(line)
		key := strings.ToLower(strings.TrimLeft(f[0], "-"))
		args := f[1:]
		switch key {
		case "remote":
			if len(args) >= 2 {
				o.Port = args[1]
			}
			if len(args) >= 3 {
				setProto(o, args[2])
			}
		case "port", "rport":
			if len(args) >= 1 {
				o.Port = args[0]
			}
		case "proto":
			if len(args) >= 1 {
				setProto(o, args[0])
			}
		case "cipher":
			if len(args) >= 1 {
				cipher = strings.ToUpper(args[0])
			}
		case "data-ciphers", "ncp-ciphers":
			if len(args) >= 1 {
				dataCiphers = strings.ToUpper(args[0])
			}
		case "auth":
			if len(args) >= 1 && supported(config.SupportedAuth, strings.ToUpper(args[0])) {
				o.Auth = strings.ToUpper(args[0])
			}
		case "compress":
			if len(args) == 1 && args[0] == "stub" {
				o.Compress = config.CompressionStub
			} else if len(args) == 0 {
				o.Compress = config.CompressionEmpty
			}
		case "comp-lzo":
			if len(args) == 1 && args[0] == "no" {
				o.Compress = config.CompressionLZONo
			}
		}
	}

	// minivpn не умеет согласовывать шифр (NCP), поэтому шифр выбирается заранее:
	// первый поддерживаемый из data-ciphers, затем cipher, иначе AES-256-GCM.
	// Сервер OpenVPN 2.5+ принимает шифр клиента, если тот есть в его data-ciphers.
	for _, c := range strings.Split(dataCiphers, ":") {
		if supported(supportedCiphers, c) {
			o.Cipher = c
			break
		}
	}
	if o.Cipher == "" && supported(supportedCiphers, cipher) {
		o.Cipher = cipher
	}
	if o.Cipher == "" {
		o.Cipher = "AES-256-GCM"
	}

	o.CA = []byte(ca.String())
	if len(o.CA) == 0 {
		return nil, errors.New("в файле нет сертификата <ca>")
	}
	if remoteIP == "" {
		return nil, errors.New("не указан адрес сервера")
	}
	o.Remote = remoteIP
	if username == "" || password == "" {
		return nil, errors.New("не заданы логин и пароль")
	}
	return o, nil
}

func setProto(o *config.OpenVPNOptions, p string) {
	switch strings.ToLower(p) {
	case "tcp", "tcp4", "tcp-client":
		o.Proto = config.ProtoTCP
	case "udp", "udp4":
		o.Proto = config.ProtoUDP
	}
}
