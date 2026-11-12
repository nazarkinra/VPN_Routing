package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vpnrouting/desktop/app"
	"vpnrouting/desktop/core"
)

// Обработчики команд (VPN, серверы, подписки, «Напрямую», МГТУ, настройки, обновления).

// ---------- VPN и серверы ----------

func (s *Server) check(_ *http.Request) (any, error) {
	ms, err := s.VPN.MeasureDelay()
	if err != nil {
		return nil, err
	}
	return map[string]int64{"delay": ms}, nil
}

func (s *Server) selectServer(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	srv := s.Store.Find(req.ID)
	if srv == nil {
		return nil, errors.New("сервер не найден")
	}
	if srv.URL == s.Store.ActiveURL() {
		return nil, nil
	}
	if s.VPN.MainActive() {
		// Служба проверит сервер запуском и при ошибке вернёт прежний
		s.VPN.ApplyKey(srv.URL)
		return map[string]string{"message": "Переключение на " + srv.Link().Title()}, nil
	}
	s.Store.SetActiveURL(srv.URL)
	return nil, nil
}

func (s *Server) addServers(r *http.Request) (any, error) {
	req, err := decode[struct {
		Text string `json:"text"`
	}](r)
	if err != nil {
		return nil, err
	}
	return s.Servers.AddInput(req.Text), nil
}

func (s *Server) removeServer(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	s.Store.RemoveServer(req.ID)
	return nil, nil
}

func (s *Server) ping(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	var list []*app.Server
	if req.ID != "" {
		if srv := s.Store.Find(req.ID); srv != nil {
			list = append(list, srv)
		}
	} else {
		list = s.Store.Servers()
	}
	if len(list) == 0 {
		return nil, errors.New("Список серверов пуст.")
	}
	s.Servers.Ping(list)
	return nil, nil
}

func (s *Server) serverLink(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	srv := s.Store.Find(req.ID)
	if srv == nil {
		return nil, errors.New("сервер не найден")
	}
	return map[string]string{"url": srv.URL}, nil
}

func (s *Server) refreshSub(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	if req.ID == "" {
		n := s.Servers.RefreshAll(false)
		if n == 0 {
			return nil, errors.New("Подписок нет.")
		}
		return map[string]int{"started": n}, nil
	}
	n, err := s.Servers.Refresh(req.ID)
	if err != nil {
		return nil, errors.New("Подписка не обновилась: " + err.Error())
	}
	sub, _ := s.Store.Subscription(req.ID)
	return map[string]string{"message": fmt.Sprintf("%s: серверов %d", sub.Title(), n)}, nil
}

func (s *Server) renameSub(r *http.Request) (any, error) {
	req, err := decode[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](r)
	if err != nil {
		return nil, err
	}
	s.Store.RenameSubscription(req.ID, req.Name)
	return nil, nil
}

func (s *Server) removeSub(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	s.Store.RemoveSubscription(req.ID)
	return nil, nil
}

func (s *Server) subLink(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	sub, ok := s.Store.Subscription(req.ID)
	if !ok {
		return nil, errors.New("подписка не найдена")
	}
	return map[string]string{"url": sub.URL}, nil
}

// ---------- Напрямую ----------

func (s *Server) reloadIfConnected() {
	if s.VPN.Running() {
		s.VPN.Reload()
	}
}

func (s *Server) addDirect(r *http.Request) (any, error) {
	req, err := decode[struct {
		Value string `json:"value"`
	}](r)
	if err != nil {
		return nil, err
	}
	v, err := s.Store.AddException(req.Value)
	if err != nil {
		return nil, err
	}
	if v == "" {
		return map[string]string{"message": "Этот адрес уже идёт напрямую."}, nil
	}
	s.reloadIfConnected()
	return map[string]string{"message": core.DisplayDomain(v) + " теперь открывается без VPN"}, nil
}

func (s *Server) removeDirect(r *http.Request) (any, error) {
	req, err := decode[struct {
		Value string `json:"value"`
	}](r)
	if err != nil {
		return nil, err
	}
	if s.Store.RemoveException(req.Value) {
		s.reloadIfConnected()
	}
	return nil, nil
}

func (s *Server) disableBuiltin(r *http.Request) (any, error) {
	req, err := decode[struct {
		Value string `json:"value"`
	}](r)
	if err != nil {
		return nil, err
	}
	s.Store.DisableBuiltin(req.Value)
	s.reloadIfConnected()
	return nil, nil
}

// ---------- МГТУ ----------

// bmstuEnabled — прежняя команда переключателя: включить или выключить VPN МГТУ.
func (s *Server) bmstuEnabled(r *http.Request) (any, error) {
	req, err := decode[struct {
		On bool `json:"on"`
	}](r)
	if err != nil {
		return nil, err
	}
	if req.On {
		return s.bmstuConnect(r)
	}
	s.VPN.StopCorp()
	return nil, nil
}

// bmstuConnect — включить VPN МГТУ (нужны файл профиля и учётные данные).
func (s *Server) bmstuConnect(_ *http.Request) (any, error) {
	if !s.Store.HasBmstuOvpn() {
		return nil, errors.New("Сначала импортируйте файл профиля .ovpn от университета.")
	}
	if !s.Store.HasBmstuCredentials() {
		return nil, errors.New("Сначала укажите логин и пароль МГТУ.")
	}
	s.VPN.StartCorp()
	return nil, nil
}

func (s *Server) bmstuCredentials(r *http.Request) (any, error) {
	req, err := decode[struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}](r)
	if err != nil {
		return nil, err
	}
	user, pass := strings.TrimSpace(req.User), req.Pass
	if pass == "" {
		pass = s.Store.BmstuPassword()
	}
	if user == "" || pass == "" {
		return nil, errors.New("Нужны и логин, и пароль.")
	}
	if err := s.Store.SetBmstuCredentials(user, pass); err != nil {
		return nil, err
	}
	s.VPN.BmstuSync()
	return nil, nil
}

func (s *Server) bmstuOvpn(r *http.Request) (any, error) {
	req, err := decode[struct {
		Text string `json:"text"`
	}](r)
	if err != nil {
		return nil, err
	}
	cfg, err := core.ParseOvpn(req.Text)
	if err != nil {
		return nil, err
	}
	note := ""
	if len(cfg.Skipped) > 0 {
		note = " Пропущены настройки: " + strings.Join(cfg.Skipped, ", ") + "."
	}
	if s.VPN.ApplyBmstuOvpn(req.Text) {
		// Служба проверит файл подключением и при ошибке вернёт прежний
		return map[string]string{"message": "Профиль импортирован, проверка подключения…" + note}, nil
	}
	s.Store.InstallBmstuOvpn(req.Text)
	return map[string]string{"message": "Профиль импортирован." + note}, nil
}

func (s *Server) bmstuRestore(_ *http.Request) (any, error) {
	if !s.Store.RestoreBmstuOvpn() {
		return nil, errors.New("Предыдущего файла нет.")
	}
	s.VPN.BmstuSync()
	return map[string]string{"message": "Возвращён предыдущий файл профиля."}, nil
}

// ---------- Настройки ----------

func (s *Server) setPrefs(r *http.Request) (any, error) {
	p, err := decode[app.Prefs](r)
	if err != nil {
		return nil, err
	}
	old := s.Store.Prefs()
	s.Store.SetPrefs(p)
	if (old.BlockDNSLeak != p.BlockDNSLeak || old.IPv6 != p.IPv6) && s.VPN.Running() {
		s.VPN.Reload()
	}
	return nil, nil
}

func (s *Server) setAutostart(r *http.Request) (any, error) {
	req, err := decode[struct {
		On bool `json:"on"`
	}](r)
	if err != nil {
		return nil, err
	}
	err = app.SetAutostart(req.On)
	s.autostart(true)
	if err != nil {
		return nil, errors.New("Не удалось изменить автозапуск: " + err.Error())
	}
	return nil, nil
}

// ---------- Обновления ----------

func (s *Server) checkUpdate(_ *http.Request) (any, error) {
	info, err := s.Updater.Check(true)
	if err != nil {
		return nil, errors.New("Не удалось проверить обновления: " + err.Error())
	}
	if !info.Newer() {
		return map[string]any{"newer": false, "message": "Установлена последняя версия " + app.VersionName}, nil
	}
	return map[string]any{"newer": true}, nil
}

func (s *Server) skipUpdate(_ *http.Request) (any, error) {
	s.Updater.Skip()
	return nil, nil
}

func (s *Server) installUpdate(_ *http.Request) (any, error) {
	if err := s.Updater.Install(); err != nil {
		return nil, err
	}
	return nil, nil
}
