package app

import (
	"encoding/base64"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Пароль МГТУ шифруется DPAPI. Программа без службы — ключом учётной записи (расшифровать может только она);
// служба (LocalSystem) — ключом компьютера: её папка настроек доступна только системе и администраторам.

const dpapiPrefix = "dpapi:"

func protectSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := []byte(plain)
	inBlob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if ServiceMode {
		flags |= windows.CRYPTPROTECT_LOCAL_MACHINE
	}
	if err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, flags, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	enc := unsafe.Slice(out.Data, out.Size)
	return dpapiPrefix + base64.StdEncoding.EncodeToString(enc), nil
}

func unprotectSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, dpapiPrefix) {
		b, err := base64.StdEncoding.DecodeString(stored)
		return string(b), err
	}
	enc, err := base64.StdEncoding.DecodeString(stored[len(dpapiPrefix):])
	if err != nil || len(enc) == 0 {
		return "", err
	}
	inBlob := windows.DataBlob{Size: uint32(len(enc)), Data: &enc[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
