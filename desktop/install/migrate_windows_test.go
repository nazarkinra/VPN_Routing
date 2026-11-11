package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Перенос настроек в папку службы: списки сохраняются, пароль МГТУ перешифровывается ключом компьютера.
func TestMigrateSettings(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("пароль-МГТУ")
	in := windows.DataBlob{Size: uint32(len(pass)), Data: &pass[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		t.Fatal(err)
	}
	userBlob := "dpapi:" + b64(unsafe.Slice(out.Data, out.Size))
	windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	src := map[string]any{
		"servers":    []any{map[string]any{"id": "a", "url": "vless://x@a.example.com:443?security=tls&type=ws#A"}},
		"exceptions": []any{"example.com"},
		"bmstu":      map[string]any{"enabled": true, "user": "ivanov", "pass": userBlob},
	}
	b, _ := json.Marshal(src)
	srcFile, dstFile := filepath.Join(dir, "user.json"), filepath.Join(dir, "service.json")
	if err := os.WriteFile(srcFile, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateSettingsFile(srcFile, dstFile); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	data, _ := os.ReadFile(dstFile)
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m["servers"].([]any)) != 1 || len(m["exceptions"].([]any)) != 1 {
		t.Fatal("списки не перенеслись")
	}
	newPass := m["bmstu"].(map[string]any)["pass"].(string)
	if !strings.HasPrefix(newPass, "dpapi:") || newPass == userBlob {
		t.Fatal("пароль должен быть перешифрован")
	}
	plain, err := dpapiDecrypt(newPass[len("dpapi:"):])
	if err != nil || string(plain) != string(pass) {
		t.Fatalf("пароль не расшифровывается: %v", err)
	}
	// Повторный перенос не трогает уже существующие настройки службы
	if err := os.WriteFile(srcFile, []byte(`{"servers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = migrateSettingsFile(srcFile, dstFile)
	data2, _ := os.ReadFile(dstFile)
	if string(data2) != string(data) {
		t.Fatal("настройки службы перезаписаны")
	}
}

func b64(b []byte) string { return base64Std(b) }
