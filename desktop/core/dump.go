package core

import "encoding/json"

// Копия рабочего конфига Xray для проверки (аналог ConfigDump.java): UUID, ключи Reality, пароли и
// логины заменены звёздочками — файл можно показать кому угодно.

var secretKeys = map[string]bool{
	"id": true, "publicKey": true, "shortId": true, "password": true, "pass": true, "user": true,
	"auth": true, "seed": true,
}

// MaskConfig возвращает конфиг со скрытыми секретами.
func MaskConfig(config string) (string, error) {
	var root any
	if err := json.Unmarshal([]byte(config), &root); err != nil {
		return "", err
	}
	maskValue(root)
	b, err := json.MarshalIndent(root, "", "  ")
	return string(b), err
}

func maskValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok && secretKeys[k] && s != "" {
				t[k] = "***"
			} else {
				maskValue(x)
			}
		}
	case []any:
		for _, x := range t {
			maskValue(x)
		}
	}
}
