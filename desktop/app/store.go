// Package app — службы ПК-версии: хранение настроек, VPN (ядро Xray), туннель МГТУ, пинг, подписки.
package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vpnrouting/desktop/core"
)

// Хранение ключа, серверов, подписок, исключений и настроек МГТУ — аналог Settings.java и
// ServerStore.java (SharedPreferences) в одном JSON-файле.

// Server — сервер: отдельная ссылка или сервер из подписки.
type Server struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	// Sub — id подписки; "" — сервер добавлен отдельной ссылкой.
	Sub  string `json:"sub,omitempty"`
	link *core.ProxyLink
}

func newServer(id, url, sub string) (*Server, error) {
	l, err := core.ParseLink(url)
	if err != nil {
		return nil, err
	}
	return &Server{ID: id, URL: url, Sub: sub, link: l}, nil
}

// Link — разобранная ссылка.
func (s *Server) Link() *core.ProxyLink { return s.link }

// Subscription — подписка и сведения о ней из заголовков.
type Subscription struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updatedAt"` // мс Unix
	Upload    int64  `json:"upload"`
	Download  int64  `json:"download"`
	// Total 0 — безлимит.
	Total int64 `json:"total"`
	// Expire — окончание, секунды Unix; 0 — бессрочно.
	Expire      int64  `json:"expire"`
	Interval    int    `json:"interval"`
	Announce    string `json:"announce"`
	Error       string `json:"error"`
	Unsupported int    `json:"unsupported"`
}

// Title — название подписки или её хост.
func (s *Subscription) Title() string {
	if s.Name != "" {
		return s.Name
	}
	u := s.URL
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndexByte(u, '@'); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// IsStale — пора обновить (интервал от провайдера, по умолчанию 12 ч).
func (s *Subscription) IsStale() bool {
	hours := s.Interval
	if hours <= 0 {
		hours = 12
	}
	return time.Now().UnixMilli()-s.UpdatedAt > int64(hours)*3600_000
}

type bmstuData struct {
	Enabled bool   `json:"enabled"`
	User    string `json:"user"`
	// Pass — пароль, зашифрованный средствами системы (DPAPI на Windows).
	Pass     string  `json:"pass"`
	Ovpn     *string `json:"ovpn,omitempty"`
	OvpnPrev *string `json:"ovpnPrev,omitempty"`
}

// Prefs — настройки приложения для ПК.
type Prefs struct {
	// BlockDNSLeak — Windows: DNS остальных программ только через туннель (фильтры WFP).
	BlockDNSLeak bool `json:"blockDnsLeak"`
	// ConnectOnStart — подключаться при запуске приложения.
	ConnectOnStart bool `json:"connectOnStart"`
	// IPv6 — заворачивать IPv6 в туннель.
	IPv6 bool `json:"ipv6"`
}

type storeData struct {
	ActiveURL  string   `json:"activeUrl"`
	Exceptions []string `json:"exceptions"`
	// DisabledBuiltins — встроенные домены, которые пользователь убрал со вкладки «Напрямую».
	DisabledBuiltins []string        `json:"disabledBuiltins,omitempty"`
	Servers          []*Server       `json:"servers"`
	Subscriptions    []*Subscription `json:"subscriptions"`
	Bmstu            bmstuData       `json:"bmstu"`
	Prefs            Prefs           `json:"prefs"`
	// Apps — маршрутизация по приложениям.
	Apps    Apps   `json:"apps"`
	UIToken string `json:"uiToken"`
	// Обновления: время последней проверки (мс Unix) и версия, отложенная кнопкой «Позже»
	UpdateLastCheck int64 `json:"updateLastCheck,omitempty"`
	UpdateSkipped   int   `json:"updateSkipped,omitempty"`
}

// Store — настройки приложения. Все методы потокобезопасны.
type Store struct {
	path  string
	mu    sync.Mutex
	d     storeData
	pings sync.Map // id сервера -> int64 мс; -1 — не ответил. Только в памяти.
}

// OpenStore читает настройки из dir/settings.json.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "settings.json")}
	s.d.Prefs = Prefs{BlockDNSLeak: true, IPv6: true}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &s.d); err != nil {
			return nil, errors.New("файл настроек повреждён: " + err.Error())
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Серверы с неподдерживаемыми ссылками пропускаются, как при загрузке на Android
	kept := s.d.Servers[:0]
	for _, srv := range s.d.Servers {
		if l, err := core.ParseLink(srv.URL); err == nil {
			srv.link = l
			kept = append(kept, srv)
		}
	}
	s.d.Servers = kept
	if s.d.UIToken == "" {
		s.d.UIToken = randomHex(16)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newID() string {
	b := randomHex(16)
	return b[:8] + "-" + b[8:12] + "-" + b[12:16] + "-" + b[16:20] + "-" + b[20:]
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(&s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	// На Windows файл ненадолго держат антивирус и индексатор: замена тогда отказывает — повторяем
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmp, s.path); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}

func (s *Store) save() {
	if err := s.saveLocked(); err != nil {
		logf("settings: %v", err)
	}
}

// UIToken — секрет для доступа к интерфейсу.
func (s *Store) UIToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.UIToken
}

// ---------- Ключ ----------

// ActiveURL — ссылка выбранного сервера.
func (s *Store) ActiveURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.ActiveURL
}

func (s *Store) SetActiveURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.ActiveURL = strings.TrimSpace(url)
	s.save()
}

// ---------- Настройки ПК ----------

func (s *Store) Prefs() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Prefs
}

func (s *Store) SetPrefs(p Prefs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Prefs = p
	s.save()
}

// ---------- Маршрутизация по приложениям ----------

// Apps — режим (core.AppsAll, AppsInclude, AppsExclude) и списки программ для каждого режима:
// при переключении режима выбор другого режима сохраняется.
type Apps struct {
	Mode    string   `json:"mode"`
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

func (s *Store) Apps() Apps {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.d.Apps
	a.Include = append([]string{}, a.Include...)
	a.Exclude = append([]string{}, a.Exclude...)
	return a
}

// SetApps сохраняет режим и списки (имена без повторов, без учёта регистра).
func (s *Store) SetApps(a Apps) {
	if a.Mode != core.AppsInclude && a.Mode != core.AppsExclude {
		a.Mode = core.AppsAll
	}
	a.Include, a.Exclude = uniqueNames(a.Include), uniqueNames(a.Exclude)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Apps = a
	s.save()
}

// AppRules — правила для конфига Xray (список действующего режима).
func (s *Store) AppRules() core.AppRules {
	a := s.Apps()
	switch a.Mode {
	case core.AppsInclude:
		return core.AppRules{Mode: a.Mode, Names: a.Include}
	case core.AppsExclude:
		return core.AppRules{Mode: a.Mode, Names: a.Exclude}
	}
	return core.AppRules{}
}

func uniqueNames(names []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return out
}

// ---------- Исключения ----------

func (s *Store) Exceptions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.d.Exceptions...)
}

func domainsOf(items []string) []string {
	var r []string
	for _, it := range items {
		if !core.IsCIDR(it) {
			r = append(r, it)
		}
	}
	return r
}

// AddException добавляет исключение. Возвращает нормализованное значение или "", если адрес уже идёт
// напрямую. Добавленный домен заменяет свои же поддомены.
func (s *Store) AddException(raw string) (string, error) {
	value, err := core.NormalizeException(raw)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.d.Exceptions
	for _, it := range items {
		if it == value {
			return "", nil
		}
	}
	if !core.IsCIDR(value) {
		if s.isBuiltinActiveLocked(value) || core.CoveredBy(core.Canonical(value), domainsOf(items)) {
			return "", nil
		}
		parent := core.Canonical(value)
		var kept []string
		for _, it := range items {
			if core.IsCIDR(it) || !strings.HasSuffix(core.Canonical(it), "."+parent) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	s.d.Exceptions = append(items, value)
	s.save()
	return value, nil
}

// CompactExceptions убирает домены, уже покрытые встроенными правилами или другими исключениями.
func (s *Store) CompactExceptions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.d.Exceptions
	domains := map[string]bool{}
	for _, d := range core.Compact(domainsOf(items)) {
		domains[d] = true
	}
	var kept []string
	seen := map[string]bool{}
	for _, it := range items {
		if core.IsCIDR(it) {
			if !seen[it] {
				seen[it] = true
				kept = append(kept, it)
			}
			continue
		}
		c := core.Canonical(it)
		if domains[c] && !s.isBuiltinActiveLocked(it) {
			delete(domains, c) // тот же домен в другом написании не сохраняем
			kept = append(kept, it)
		}
	}
	removed := len(items) - len(kept)
	if removed > 0 {
		s.d.Exceptions = kept
		s.save()
	}
	return removed
}

func (s *Store) RemoveException(value string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.d.Exceptions {
		if it == value {
			s.d.Exceptions = append(s.d.Exceptions[:i:i], s.d.Exceptions[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

// ---------- Встроенные правила, отключённые пользователем ----------

// DisabledBuiltins — отключённые встроенные домены (в каноническом виде).
func (s *Store) DisabledBuiltins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.d.DisabledBuiltins...)
}

// DisableBuiltin убирает встроенный домен: сайты из него пойдут через VPN.
func (s *Store) DisableBuiltin(domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := core.Canonical(domain)
	for _, d := range s.d.DisabledBuiltins {
		if d == c {
			return
		}
	}
	s.d.DisabledBuiltins = append(s.d.DisabledBuiltins, c)
	s.save()
}

// RestoreBuiltins возвращает все встроенные правила.
func (s *Store) RestoreBuiltins() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.DisabledBuiltins = nil
	s.save()
}

// Builtins — действующие встроенные правила без отключённых.
func (s *Store) Builtins() core.Builtins {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.builtinsLocked()
}

func (s *Store) builtinsLocked() core.Builtins {
	return core.Builtins{
		Direct: core.Without(core.DirectDomains, s.d.DisabledBuiltins),
		Tunnel: core.Without(core.DirectTunnelDomains, s.d.DisabledBuiltins),
	}
}

// isBuiltinActiveLocked — адрес уже идёт напрямую по действующим встроенным правилам.
func (s *Store) isBuiltinActiveLocked(value string) bool {
	return core.CoveredBy(core.Canonical(value), s.builtinsLocked().Direct)
}

// ---------- Серверы и подписки ----------

func (s *Store) Servers() []*Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Server{}, s.d.Servers...)
}

func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]Subscription, len(s.d.Subscriptions))
	for i, sub := range s.d.Subscriptions {
		r[i] = *sub
	}
	return r
}

func (s *Store) FindByURL(url string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range s.d.Servers {
		if srv.URL == url {
			return srv
		}
	}
	return nil
}

func (s *Store) Find(id string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range s.d.Servers {
		if srv.ID == id {
			return srv
		}
	}
	return nil
}

func (s *Store) subLocked(id string) *Subscription {
	for _, sub := range s.d.Subscriptions {
		if sub.ID == id {
			return sub
		}
	}
	return nil
}

func (s *Store) Subscription(id string) (Subscription, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub := s.subLocked(id); sub != nil {
		return *sub, true
	}
	return Subscription{}, false
}

func (s *Store) Ping(id string) (int64, bool) {
	v, ok := s.pings.Load(id)
	if !ok {
		return 0, false
	}
	return v.(int64), true
}

func (s *Store) SetPing(id string, ms int64) { s.pings.Store(id, ms) }

// AddServer добавляет отдельный сервер; если ссылка уже есть — возвращает существующий.
func (s *Store) AddServer(url string) (*Server, error) {
	clean := strings.Join(strings.Fields(url), "")
	if srv := s.FindByURL(clean); srv != nil {
		return srv, nil
	}
	srv, err := newServer(newID(), clean, "")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Servers = append(s.d.Servers, srv)
	s.save()
	return srv, nil
}

func (s *Store) AddSubscription(url string) Subscription {
	url = strings.TrimSpace(url)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.d.Subscriptions {
		if sub.URL == url {
			return *sub
		}
	}
	sub := &Subscription{ID: newID(), URL: url}
	s.d.Subscriptions = append(s.d.Subscriptions, sub)
	s.save()
	return *sub
}

// ApplySubscription заменяет серверы подписки свежим списком и сохраняет сведения о ней.
func (s *Store) ApplySubscription(id string, r *core.SubscriptionResult) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := s.subLocked(id)
	if sub == nil {
		return 0
	}
	// Серверы, которые остались в подписке, сохраняют свои id (и результаты пинга)
	old := map[string]*Server{}
	insertAt := -1
	var others []*Server
	for _, srv := range s.d.Servers {
		if srv.Sub == id {
			old[srv.URL] = srv
			if insertAt < 0 {
				insertAt = len(others)
			}
		} else {
			others = append(others, srv)
		}
	}
	if insertAt < 0 {
		insertAt = len(others)
	}
	var fresh []*Server
	for _, url := range r.Links {
		if keep := old[url]; keep != nil {
			fresh = append(fresh, keep)
		} else if srv, err := newServer(newID(), url, id); err == nil {
			fresh = append(fresh, srv)
		}
	}
	all := append([]*Server{}, others[:insertAt]...)
	all = append(all, fresh...)
	all = append(all, others[insertAt:]...)
	s.d.Servers = all

	if r.Title != "" {
		sub.Name = r.Title
	}
	sub.Upload, sub.Download, sub.Total, sub.Expire = r.Upload, r.Download, r.Total, r.Expire
	sub.Interval = r.UpdateIntervalHours
	sub.Announce = r.Announce
	sub.Unsupported = r.Unsupported + len(r.Links) - len(fresh)
	sub.UpdatedAt = time.Now().UnixMilli()
	sub.Error = ""
	s.save()
	return len(fresh)
}

func (s *Store) SetSubscriptionError(id, e string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub := s.subLocked(id); sub != nil {
		sub.Error = e
		s.save()
	}
}

func (s *Store) RenameSubscription(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub := s.subLocked(id); sub != nil {
		sub.Name = strings.TrimSpace(name)
		s.save()
	}
}

func (s *Store) RemoveServer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, srv := range s.d.Servers {
		if srv.ID == id {
			s.d.Servers = append(s.d.Servers[:i:i], s.d.Servers[i+1:]...)
			s.pings.Delete(id)
			s.save()
			return
		}
	}
}

func (s *Store) RemoveSubscription(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []*Server
	for _, srv := range s.d.Servers {
		if srv.Sub == id {
			s.pings.Delete(srv.ID)
		} else {
			kept = append(kept, srv)
		}
	}
	s.d.Servers = kept
	for i, sub := range s.d.Subscriptions {
		if sub.ID == id {
			s.d.Subscriptions = append(s.d.Subscriptions[:i:i], s.d.Subscriptions[i+1:]...)
			break
		}
	}
	s.save()
}

// SelectFirstIfNone выбирает первый сервер, если выбранного нет в списке.
func (s *Store) SelectFirstIfNone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range s.d.Servers {
		if srv.URL == s.d.ActiveURL {
			return
		}
	}
	if len(s.d.Servers) > 0 {
		s.d.ActiveURL = s.d.Servers[0].URL
		s.save()
	}
}

// ---------- VPN МГТУ (аналог bmstu.auth и bmstu-source.ovpn) ----------

func (s *Store) BmstuEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Bmstu.Enabled
}

func (s *Store) SetBmstuEnabled(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Bmstu.Enabled = on
	s.save()
}

func (s *Store) BmstuUser() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Bmstu.User
}

func (s *Store) BmstuPassword() string {
	s.mu.Lock()
	enc := s.d.Bmstu.Pass
	s.mu.Unlock()
	if enc == "" {
		return ""
	}
	p, err := unprotectSecret(enc)
	if err != nil {
		logf("bmstu password: %v", err)
		return ""
	}
	return p
}

func (s *Store) SetBmstuCredentials(user, password string) error {
	enc, err := protectSecret(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Bmstu.User, s.d.Bmstu.Pass = user, enc
	s.save()
	return nil
}

func (s *Store) HasBmstuCredentials() bool {
	return s.BmstuUser() != "" && s.BmstuPassword() != ""
}

// BmstuOvpn — импортированный .ovpn или "" (встроенного файла нет: профиль университета не публикуется).
func (s *Store) BmstuOvpn() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Bmstu.Ovpn == nil {
		return ""
	}
	return *s.d.Bmstu.Ovpn
}

func (s *Store) HasBmstuOvpn() bool { return s.BmstuOvpn() != "" }

func (s *Store) HasBmstuPreviousOvpn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Bmstu.OvpnPrev != nil
}

// InstallBmstuOvpn устанавливает новый .ovpn, прежний сохраняется для отката.
func (s *Store) InstallBmstuOvpn(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.d.Bmstu.Ovpn; cur != nil && *cur != text {
		prev := *cur
		s.d.Bmstu.OvpnPrev = &prev
	}
	s.d.Bmstu.Ovpn = &text
	s.save()
}

// RestoreBmstuOvpn возвращает предыдущий .ovpn.
func (s *Store) RestoreBmstuOvpn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.d.Bmstu.OvpnPrev
	if prev == nil {
		return false
	}
	s.d.Bmstu.OvpnPrev = s.d.Bmstu.Ovpn
	s.d.Bmstu.Ovpn = prev
	s.save()
	return true
}

// ---------- Обновления ----------

func (s *Store) UpdateLastCheck() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.UpdateLastCheck
}

func (s *Store) SetUpdateLastCheck(ms int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.UpdateLastCheck = ms
	s.save()
}

func (s *Store) UpdateSkipped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.UpdateSkipped
}

func (s *Store) SetUpdateSkipped(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.UpdateSkipped = code
	s.save()
}
