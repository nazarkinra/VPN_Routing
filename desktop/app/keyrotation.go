package app

import (
	"strings"

	"vpnrouting/desktop/core"
)

// Плановая смена ключей на сервере (то же, что KeyRotation.java): подписка присылает тот же сервер
// с новым UUID, а старый ключ работает ещё ограниченное время. После обновления подписки выбранный
// сервер переводится на новый ключ без участия человека.

// replacementFor — тот же сервер с другим ключом: совпадают протокол, адрес, порт, транспорт, путь
// и название, а ссылка другая. "" — замены нет или она неоднозначна.
func replacementFor(oldURL string, servers []*Server) string {
	old, err := core.ParseLink(oldURL)
	if err != nil {
		return ""
	}
	found := ""
	for _, s := range servers {
		if s.URL == oldURL {
			return ""
		}
		l := s.link
		if l == nil || l.Protocol != old.Protocol || !strings.EqualFold(l.Server, old.Server) || l.Port != old.Port ||
			l.Network != old.Network || l.Path != old.Path || l.Name != old.Name {
			continue
		}
		if found != "" {
			return ""
		}
		found = s.URL
	}
	return found
}

// followKeyRotation — выбранный сервер исчез из подписки, но есть он же с новым ключом: перейти.
// apply вызывается, если основной VPN включён (служба переподключится), иначе ключ просто запоминается.
func (s *Servers) followKeyRotation() {
	active := s.store.ActiveURL()
	if active == "" || s.store.FindByURL(active) != nil {
		return
	}
	next := replacementFor(active, s.store.Servers())
	if next == "" {
		return
	}
	if s.OnKeyRotated != nil && s.OnKeyRotated(next) {
		logf("key rotation: переход на новый ключ сервера")
		return
	}
	s.store.SetActiveURL(next)
}
