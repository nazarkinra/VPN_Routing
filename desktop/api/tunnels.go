package api

import (
	"errors"
	"net/http"

	"vpnrouting/desktop/core"
)

// Команды дополнительных туннелей (версия 2.1). Каждое изменение пересобирает конфиг ядра, если VPN работает.

func (s *Server) tunnelsView() []TunnelView {
	byURL := map[string]string{}
	for _, srv := range s.Store.Servers() {
		byURL[srv.URL] = srv.ID
	}
	var r []TunnelView
	for i, t := range s.Store.Tunnels() {
		v := TunnelView{ID: t.ID, Number: i + 2, Name: t.Name, Enabled: t.Enabled,
			Apps: t.Apps, Domains: t.Domains, ServerID: byURL[t.Server]}
		if v.Apps == nil {
			v.Apps = []string{}
		}
		if v.Domains == nil {
			v.Domains = []string{}
		}
		if l := t.Link(); l != nil {
			v.HasServer = true
			v.ServerTitle, v.ServerSubtitle = l.Title(), l.Subtitle()
		}
		if st, ok := s.VPN.TunnelStatus(t.ID); ok {
			v.Running, v.Status = true, st
		}
		r = append(r, v)
	}
	return r
}

func (s *Server) tunnelAdd(r *http.Request) (any, error) {
	req, err := decode[struct {
		Name string `json:"name"`
	}](r)
	if err != nil {
		return nil, err
	}
	t := s.Store.AddTunnel(req.Name)
	return map[string]string{"id": t.ID}, nil
}

// tunnelSet — название, сервер (id из списка серверов; "" — оставить прежний) и включённость.
func (s *Server) tunnelSet(r *http.Request) (any, error) {
	req, err := decode[struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		ServerID string `json:"serverId"`
		Enabled  bool   `json:"enabled"`
	}](r)
	if err != nil {
		return nil, err
	}
	found, server := false, ""
	for _, t := range s.Store.Tunnels() {
		if t.ID == req.ID {
			found, server = true, t.Server
		}
	}
	if !found {
		return nil, errors.New("туннель не найден")
	}
	if req.ServerID != "" {
		srv := s.Store.Find(req.ServerID)
		if srv == nil {
			return nil, errors.New("сервер не найден")
		}
		server = srv.URL
	}
	if req.Enabled && server == "" {
		return nil, errors.New("Сначала выберите сервер туннеля.")
	}
	if err := s.Store.SetTunnel(req.ID, req.Name, server, req.Enabled); err != nil {
		return nil, err
	}
	s.reloadIfConnected()
	return nil, nil
}

func (s *Server) tunnelRemove(r *http.Request) (any, error) {
	req, err := decode[idReq](r)
	if err != nil {
		return nil, err
	}
	s.Store.RemoveTunnel(req.ID)
	s.reloadIfConnected()
	return nil, nil
}

func (s *Server) tunnelApps(r *http.Request) (any, error) {
	req, err := decode[struct {
		ID   string   `json:"id"`
		Apps []string `json:"apps"`
	}](r)
	if err != nil {
		return nil, err
	}
	if err := s.Store.SetTunnelApps(req.ID, req.Apps); err != nil {
		return nil, err
	}
	s.reloadIfConnected()
	return nil, nil
}

func (s *Server) tunnelDomainAdd(r *http.Request) (any, error) {
	req, err := decode[struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}](r)
	if err != nil {
		return nil, err
	}
	v, err := s.Store.AddTunnelDomain(req.ID, req.Value)
	if err != nil {
		return nil, err
	}
	if v == "" {
		return map[string]string{"message": "Этот адрес уже есть в туннеле."}, nil
	}
	s.reloadIfConnected()
	return map[string]string{"message": core.DisplayDomain(v) + " теперь идёт через туннель"}, nil
}

func (s *Server) tunnelDomainRemove(r *http.Request) (any, error) {
	req, err := decode[struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}](r)
	if err != nil {
		return nil, err
	}
	s.Store.RemoveTunnelDomain(req.ID, req.Value)
	s.reloadIfConnected()
	return nil, nil
}
