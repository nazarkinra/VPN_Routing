package api

import "vpnrouting/desktop/app"

// Состояние для окна приложения (ответ /api/state).

type ServerView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	// Ping — задержка в мс, -1 — не ответил, nil — не измерялась.
	Ping    *int64 `json:"ping"`
	Pinging bool   `json:"pinging"`
	Active  bool   `json:"active"`
	Sub     string `json:"sub"`
}

type SubView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	UpdatedAt   int64  `json:"updatedAt"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	Total       int64  `json:"total"`
	Expire      int64  `json:"expire"`
	Announce    string `json:"announce"`
	Error       string `json:"error"`
	Unsupported int    `json:"unsupported"`
	Refreshing  bool   `json:"refreshing"`
	Stale       bool   `json:"stale"`
}

// Group — «Мои серверы» (Sub == nil) или подписка со своими серверами.
type Group struct {
	Sub     *SubView     `json:"sub"`
	Servers []ServerView `json:"servers"`
}

// Entry — строка вкладки «Напрямую». Value — значение для удаления ("" — встроенное, не удаляется).
type Entry struct {
	Value    string `json:"value,omitempty"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Icon     string `json:"icon"`
	Builtin  bool   `json:"builtin,omitempty"`
}

type Direct struct {
	User  []Entry `json:"user"`
	Bmstu []Entry `json:"bmstu"`
	Zones []Entry `json:"zones"`
	Sites []Entry `json:"sites"`
	Nets  []Entry `json:"nets"`
	// Disabled — сколько встроенных правил отключено.
	Disabled int `json:"disabled"`
}

type Bmstu struct {
	Enabled        bool            `json:"enabled"`
	HasOvpn        bool            `json:"hasOvpn"`
	HasCredentials bool            `json:"hasCredentials"`
	User           string          `json:"user"`
	HasPrev        bool            `json:"hasPrev"`
	Status         app.BmstuStatus `json:"status"`
	Profile        string          `json:"profile"`
	FileInfo       string          `json:"fileInfo"`
}

type Autostart struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

type State struct {
	Platform   string           `json:"platform"`
	Version    string           `json:"version"`
	VPN        app.Status       `json:"vpn"`
	Current    *ServerView      `json:"current"`
	Groups     []Group          `json:"groups"`
	Pinging    bool             `json:"pinging"`
	Bmstu      Bmstu            `json:"bmstu"`
	Direct     Direct           `json:"direct"`
	Prefs      app.Prefs        `json:"prefs"`
	Autostart  Autostart        `json:"autostart"`
	Update     app.UpdateStatus `json:"update"`
	ConfigPath string           `json:"configPath"`
}

// Result — ответ команды: сообщение для пользователя и данные (поля зависят от команды).
type Result struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Error   string   `json:"error"`
	URL     string   `json:"url"`
	Config  string   `json:"config"`
	Log     string   `json:"log"`
	Delay   int64    `json:"delay"`
	Started int      `json:"started"`
	Newer   bool     `json:"newer"`
	Added   int      `json:"added"`
	Errors  []string `json:"errors"`
	// Subscriptions — названия добавленных подписок (команда «Добавить»).
	Subscriptions []string `json:"subscriptions"`
}
