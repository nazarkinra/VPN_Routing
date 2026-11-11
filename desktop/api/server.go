// Package api — команды приложения (подключить, выбрать сервер, добавить сайт напрямую…) и состояние для
// окна. На Windows окно вызывает их прямо внутри процесса; на Linux окно пользователя обращается к службе
// (root) через сокет /run/vpnrouting/control.sock. Тот же сокет обслуживает команды vpnrouting connect и т. п.
package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"vpnrouting/desktop/app"
	"vpnrouting/desktop/core"
)

// Server — обработчик команд.
type Server struct {
	Store   *app.Store
	VPN     *app.VPN
	Servers *app.Servers
	Updater *app.Updater
	// DataDir — папка настроек (копия конфига ядра)
	DataDir string
	// Quit — выход из приложения (выключить VPN и закрыть).
	Quit func()
	// Show — показать окно (повторный запуск ярлыка, команда vpnrouting open); nil — окна нет (служба Linux).
	Show func()
	// Version — версия для экрана настроек.
	Version string

	once    sync.Once
	handler http.Handler

	autoMu        sync.Mutex
	autoChecked   time.Time
	autoSupported bool
	autoEnabled   bool
}

// Handler — HTTP-обработчик команд (для сокета и для вызова внутри процесса).
func (s *Server) Handler() http.Handler {
	s.once.Do(func() { s.handler = s.routes() })
	return s.handler
}

// Serve обслуживает команды на слушателе (сокет управления). Возвращает сразу.
func (s *Server) Serve(ln net.Listener) {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	api := func(path string, h func(r *http.Request) (any, error)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, `{"error":"нужен JSON"}`, http.StatusUnsupportedMediaType)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			res, err := h(r)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if res == nil {
				res = map[string]bool{"ok": true}
			}
			_ = json.NewEncoder(w).Encode(res)
		})
	}

	api("/api/state", s.state)
	api("/api/vpn/connect", s.post(func(_ *http.Request) (any, error) { s.VPN.Start(); return nil, nil }))
	api("/api/vpn/disconnect", s.post(func(_ *http.Request) (any, error) { s.VPN.Stop(); return nil, nil }))
	api("/api/vpn/toggle", s.post(func(_ *http.Request) (any, error) { s.VPN.Toggle(); return nil, nil }))
	api("/api/vpn/check", s.post(s.check))
	api("/api/servers/select", s.post(s.selectServer))
	api("/api/servers/add", s.post(s.addServers))
	api("/api/servers/remove", s.post(s.removeServer))
	api("/api/servers/ping", s.post(s.ping))
	api("/api/servers/link", s.post(s.serverLink))
	api("/api/subs/refresh", s.post(s.refreshSub))
	api("/api/subs/rename", s.post(s.renameSub))
	api("/api/subs/remove", s.post(s.removeSub))
	api("/api/subs/link", s.post(s.subLink))
	api("/api/direct/add", s.post(s.addDirect))
	api("/api/direct/remove", s.post(s.removeDirect))
	api("/api/direct/disable-builtin", s.post(s.disableBuiltin))
	api("/api/direct/restore-builtins", s.post(func(_ *http.Request) (any, error) {
		s.Store.RestoreBuiltins()
		s.reloadIfConnected()
		return map[string]string{"message": "Встроенные правила возвращены"}, nil
	}))
	api("/api/update/check", s.post(s.checkUpdate))
	api("/api/update/skip", s.post(s.skipUpdate))
	api("/api/update/install", s.post(s.installUpdate))
	api("/api/config", func(_ *http.Request) (any, error) {
		b, err := os.ReadFile(app.ConfigDumpPath(s.DataDir))
		if err != nil {
			return nil, errors.New("Конфиг появится после первого подключения.")
		}
		return map[string]string{"config": string(b)}, nil
	})
	api("/api/bmstu/enabled", s.post(s.bmstuEnabled))
	api("/api/bmstu/credentials", s.post(s.bmstuCredentials))
	api("/api/bmstu/ovpn", s.post(s.bmstuOvpn))
	api("/api/bmstu/restore", s.post(s.bmstuRestore))
	api("/api/bmstu/log", func(_ *http.Request) (any, error) { return map[string]string{"log": s.VPN.Bmstu().Log()}, nil })
	api("/api/applog", func(_ *http.Request) (any, error) { return map[string]string{"log": app.AppLog()}, nil })
	api("/api/prefs", s.post(s.setPrefs))
	api("/api/autostart", s.post(s.setAutostart))
	api("/api/show", s.post(func(_ *http.Request) (any, error) {
		if s.Show == nil {
			return nil, errors.New("окна нет")
		}
		s.Show()
		return nil, nil
	}))
	api("/api/quit", s.post(func(_ *http.Request) (any, error) {
		if s.Quit != nil {
			go func() { time.Sleep(300 * time.Millisecond); s.Quit() }()
		}
		return nil, nil
	}))
	return mux
}

func (s *Server) post(h func(r *http.Request) (any, error)) func(r *http.Request) (any, error) {
	return func(r *http.Request) (any, error) {
		if r.Method != http.MethodPost {
			return nil, errors.New("нужен POST")
		}
		return h(r)
	}
}

func decode[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&v)
	if err != nil {
		return v, errors.New("неверный запрос")
	}
	return v, nil
}

type idReq struct {
	ID string `json:"id"`
}

// ---------- Состояние ----------

func (s *Server) state(_ *http.Request) (any, error) {
	active := s.Store.ActiveURL()
	var current *ServerView
	views := map[string][]ServerView{}
	for _, srv := range s.Store.Servers() {
		v := ServerView{ID: srv.ID, Title: srv.Link().Title(), Subtitle: srv.Link().Subtitle(),
			Pinging: s.Servers.IsPinging(srv.ID), Active: srv.URL == active, Sub: srv.Sub}
		if ms, ok := s.Store.Ping(srv.ID); ok {
			v.Ping = &ms
		}
		if v.Active {
			c := v
			current = &c
		}
		views[srv.Sub] = append(views[srv.Sub], v)
	}
	if current == nil && active != "" {
		if l, err := core.ParseLink(active); err == nil {
			current = &ServerView{Title: l.Title(), Subtitle: l.Describe(), Active: true}
		}
	}
	groups := []Group{{Servers: views[""]}}
	for _, sub := range s.Store.Subscriptions() {
		sv := &SubView{ID: sub.ID, Title: sub.Title(), UpdatedAt: sub.UpdatedAt, Upload: sub.Upload,
			Download: sub.Download, Total: sub.Total, Expire: sub.Expire, Announce: sub.Announce, Error: sub.Error,
			Unsupported: sub.Unsupported, Refreshing: s.Servers.IsRefreshing(sub.ID), Stale: sub.IsStale()}
		groups = append(groups, Group{Sub: sv, Servers: views[sub.ID]})
	}
	return &State{
		Platform:   runtime.GOOS,
		Version:    s.Version,
		VPN:        s.VPN.Status(),
		Current:    current,
		Groups:     groups,
		Pinging:    s.Servers.AnyPinging(),
		Bmstu:      s.bmstuView(),
		Direct:     s.directView(),
		Prefs:      s.Store.Prefs(),
		Autostart:  s.autostart(false),
		Update:     s.Updater.Status(),
		ConfigPath: app.ConfigDumpPath(s.DataDir),
	}, nil
}

// autostart — состояние автозапуска. Проверка запускает schtasks/systemctl, поэтому кешируется.
func (s *Server) autostart(refresh bool) Autostart {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if refresh || s.autoChecked.IsZero() || time.Since(s.autoChecked) > time.Minute {
		s.autoSupported = app.AutostartSupported()
		s.autoEnabled = s.autoSupported && app.AutostartEnabled()
		s.autoChecked = time.Now()
	}
	return Autostart{Supported: s.autoSupported, Enabled: s.autoEnabled}
}

func (s *Server) bmstuView() Bmstu {
	v := Bmstu{
		Enabled:        s.Store.BmstuEnabled(),
		HasOvpn:        s.Store.HasBmstuOvpn(),
		HasCredentials: s.Store.HasBmstuCredentials(),
		User:           s.Store.BmstuUser(),
		HasPrev:        s.Store.HasBmstuPreviousOvpn(),
		Status:         s.VPN.Bmstu().Status(),
	}
	if v.HasOvpn {
		cfg, err := core.ParseOvpn(s.Store.BmstuOvpn())
		if err != nil {
			v.Profile, v.FileInfo = "Файл профиля повреждён", err.Error()
		} else {
			v.Profile = cfg.RemoteHost + " · " + cfg.ProtoAndPort()
			if cfg.NotAfter == "" {
				v.FileInfo = "Срок сертификата не указан"
			} else {
				v.FileInfo = "Сертификат действует до " + cfg.NotAfterDate()
			}
		}
	}
	return v
}

func (s *Server) directView() Direct {
	var d Direct
	for _, it := range s.Store.Exceptions() {
		e := Entry{Value: it, Title: core.DisplayDomain(it)}
		switch {
		case core.IsCIDR(it):
			e.Icon = "dns"
			if strings.HasSuffix(it, "/32") || strings.HasSuffix(it, "/128") {
				e.Subtitle = "IP-адрес"
			} else {
				e.Subtitle = "подсеть " + it
			}
		case strings.HasPrefix(it, "."):
			e.Icon, e.Subtitle = "globe", "доменная зона: все сайты в ней"
		default:
			e.Icon, e.Subtitle = "link", "сайт и все поддомены"
		}
		d.User = append(d.User, e)
	}
	// Встроенные правила можно убрать (Value — домен), вернуть — кнопкой «Вернуть встроенные»
	b := s.Store.Builtins()
	for _, dom := range core.Compact(b.Direct) {
		if strings.Contains(dom, ".") {
			d.Sites = append(d.Sites, Entry{Value: dom, Builtin: true, Title: core.DisplayDomain(dom), Subtitle: "сайт и все поддомены", Icon: "link"})
		} else {
			d.Zones = append(d.Zones, Entry{Value: dom, Builtin: true, Title: "." + core.DisplayDomain(dom), Subtitle: "все сайты в зоне", Icon: "globe"})
		}
	}
	for _, dom := range b.Tunnel {
		d.Sites = append(d.Sites, Entry{Value: dom, Builtin: true, Title: dom, Subtitle: "Cloudflare Tunnel", Icon: "link"})
	}
	d.Nets = []Entry{
		{Title: "Локальная сеть", Subtitle: "роутер, принтеры, NAS: 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12", Icon: "dns"},
		{Title: "Служебные адреса", Subtitle: "127.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10, multicast, IPv6 ULA", Icon: "dns"},
		{Title: "VPN-протоколы", Subtitle: "порты " + strings.ReplaceAll(core.DirectPorts, ",", ", "), Icon: "shield"},
	}
	bmstu := "VPN МГТУ выключен: идёт напрямую, как остальные .ru"
	if s.Store.BmstuEnabled() {
		bmstu = "через корпоративный VPN; пока он не подключён — напрямую"
	}
	d.Bmstu = []Entry{{Title: core.BmstuDomain, Subtitle: bmstu, Icon: "school"}}
	d.Disabled = len(s.Store.DisabledBuiltins())
	return d
}
