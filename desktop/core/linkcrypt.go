package core

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"
)

// Зашифрованные ссылки-подписки vpnrouting://crypt1/<base64url> — то же, что LinkCrypt.java.
//
// Внутри — адрес подписки (https://…), зашифрованный открытым ключом RSA-4096 (PKCS#1 v1.5, блоками
// по 501 байту, как happ://crypt4). Закрытый ключ встраивается при сборке (build-wsl.sh,
// -ldflags -X) и в репозитории не лежит. Сервер подписок к тому же привязывает ссылку к первому
// устройству (заголовок X-Hwid, см. app.DeviceID).

// CryptLinkPrefix — схема и версия формата: новый ключ или шифр — новая версия.
const CryptLinkPrefix = "vpnrouting://crypt1/"

const cryptBlock = 512 // RSA-4096

// linkPrivateKey — закрытый ключ PKCS#8 (DER) в base64; задаётся при сборке.
var linkPrivateKey string

// IsCryptLink — строка похожа на зашифрованную ссылку.
func IsCryptLink(text string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), CryptLinkPrefix)
}

// CryptLinksAvailable — в сборке есть ключ (без него зашифрованные ссылки не читаются).
func CryptLinksAvailable() bool { return linkPrivateKey != "" }

// DecryptLink — адрес подписки из зашифрованной ссылки ключом этой сборки.
func DecryptLink(link string) (string, error) {
	if !CryptLinksAvailable() {
		return "", errors.New("эта сборка приложения не умеет зашифрованные ссылки — обновите приложение")
	}
	return decryptLinkWithKey(link, linkPrivateKey)
}

func decryptLinkWithKey(link, keyBase64 string) (string, error) {
	if !IsCryptLink(link) {
		return "", errors.New("это не зашифрованная ссылка")
	}
	link = strings.TrimSpace(link)
	data, ok := DecodeBase64(link[len(CryptLinkPrefix):])
	if !ok || len(data) == 0 || len(data)%cryptBlock != 0 {
		return "", errors.New("ссылка повреждена: скопируйте её из бота целиком")
	}
	der, ok := DecodeBase64(keyBase64)
	if !ok {
		return "", errors.New("ключ сборки повреждён")
	}
	parsed, err := x509.ParsePKCS8PrivateKey([]byte(der))
	if err != nil {
		return "", errors.New("ключ сборки повреждён")
	}
	key, isRSA := parsed.(*rsa.PrivateKey)
	if !isRSA || key.N.BitLen() != cryptBlock*8 {
		return "", errors.New("ключ сборки не RSA-4096")
	}
	var out []byte
	for off := 0; off < len(data); off += cryptBlock {
		plain, err := rsa.DecryptPKCS1v15(nil, key, []byte(data[off:off+cryptBlock]))
		if err != nil {
			// Чужой ключ или испорченная ссылка: PKCS#1 не сходится
			return "", errors.New("ссылка не подходит к этому приложению или повреждена")
		}
		out = append(out, plain...)
	}
	url := string(out)
	if !IsSubscriptionURL(url) {
		return "", errors.New("внутри ссылки не адрес подписки")
	}
	return url, nil
}
