package app

import (
	"crypto/sha256"
	"encoding/hex"
)

// Идентификатор устройства для заголовка X-Hwid (как у Happ и DeviceId.java): сервер подписок
// привязывает зашифрованную ссылку к первому устройству и отказывает остальным.
//
// Основа — постоянный идентификатор системы (Windows: MachineGuid, Linux: /etc/machine-id), он один
// и для программы, и для службы. Наружу уходит не он сам, а хеш с солью приложения. Если система
// идентификатора не даёт, берётся случайный, сохранённый в настройках (Store.DeviceID).

func hwidFromPlatformID(platformID string) string {
	h := sha256.Sum256([]byte("vpnrouting-hwid-v1:" + platformID))
	return hex.EncodeToString(h[:16]) // 32 знака — проходят проверку формата на сервере
}

// DeviceID — значение X-Hwid этого компьютера.
func (s *Store) DeviceID() string {
	if id := platformID(); id != "" {
		return hwidFromPlatformID(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.DeviceID == "" {
		s.d.DeviceID = randomHex(32)
		_ = s.saveLocked()
	}
	return hwidFromPlatformID(s.d.DeviceID)
}
