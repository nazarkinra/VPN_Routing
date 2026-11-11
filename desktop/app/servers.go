package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"vpnrouting/desktop/core"
	"vpnrouting/desktop/netenv"
)

// Пинг серверов и обновление подписок (аналог Pinger.java и обновления в VpnFragment.java).

// Servers — действия со списком серверов.
type Servers struct {
	store *Store
	sem   chan struct{}

	mu         sync.Mutex
	pinging    map[string]bool
	refreshing map[string]bool
}

func NewServers(store *Store) *Servers {
	return &Servers{store: store, sem: make(chan struct{}, 6),
		pinging: map[string]bool{}, refreshing: map[string]bool{}}
}

// IsPinging / IsRefreshing — для индикаторов в интерфейсе.
func (s *Servers) IsPinging(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinging[id]
}

func (s *Servers) IsRefreshing(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshing[id]
}

// AnyPinging — идёт замер.
func (s *Servers) AnyPinging() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pinging) > 0
}

// Ping замеряет задержку серверов (реальный запрос через сервер, как «пинг» в клиентах Xray).
func (s *Servers) Ping(servers []*Server) {
	for _, srv := range servers {
		s.mu.Lock()
		if s.pinging[srv.ID] {
			s.mu.Unlock()
			continue
		}
		s.pinging[srv.ID] = true
		s.mu.Unlock()
		go func(srv *Server) {
			s.sem <- struct{}{}
			ms := measureServer(srv)
			<-s.sem
			s.store.SetPing(srv.ID, ms)
			s.mu.Lock()
			delete(s.pinging, srv.ID)
			s.mu.Unlock()
		}(srv)
	}
}

// measureServer — задержка в мс или -1.
func measureServer(srv *Server) int64 {
	if netenv.Current() == nil {
		_, _ = netenv.Refresh()
	}
	ip, err := netenv.LookupIP(context.Background(), srv.link.Server)
	if err != nil {
		logf("ping %s: %v", srv.link.Title(), err)
		return -1
	}
	cfg, err := core.BuildPingConfig(srv.link, ip)
	if err != nil {
		return -1
	}
	ms, err := measureOutboundDelay(cfg, core.DelayTestURL)
	if err != nil || ms <= 0 {
		logf("ping %s: %v", srv.link.Title(), err)
		return -1
	}
	return ms
}

// Refresh обновляет подписку. Возвращает число серверов или ошибку.
func (s *Servers) Refresh(id string) (int, error) {
	sub, ok := s.store.Subscription(id)
	if !ok {
		return 0, fmt.Errorf("подписка не найдена")
	}
	s.mu.Lock()
	if s.refreshing[id] {
		s.mu.Unlock()
		return 0, fmt.Errorf("подписка уже обновляется")
	}
	s.refreshing[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.refreshing, id)
		s.mu.Unlock()
	}()
	if netenv.Current() == nil {
		_, _ = netenv.Refresh()
	}
	r, err := core.FetchSubscription(context.Background(), netenv.HTTPClient(), sub.URL)
	if err != nil {
		s.store.SetSubscriptionError(id, err.Error())
		logf("subscription %s: %v", sub.Title(), err)
		return 0, err
	}
	n := s.store.ApplySubscription(id, r)
	s.store.SelectFirstIfNone()
	return n, nil
}

// RefreshAll обновляет подписки в фоне; onlyStale — только устаревшие.
func (s *Servers) RefreshAll(onlyStale bool) int {
	n := 0
	for _, sub := range s.store.Subscriptions() {
		if !onlyStale || sub.IsStale() {
			n++
			go func(id string) { _, _ = s.Refresh(id) }(sub.ID)
		}
	}
	return n
}

// AddResult — итог добавления ссылок.
type AddResult struct {
	Added         int      `json:"added"`
	Subscriptions []string `json:"subscriptions"`
	Errors        []string `json:"errors"`
}

// AddInput — ссылки vless:// и hysteria2:// и адреса подписок — по одной на строке.
func (s *Servers) AddInput(input string) AddResult {
	var r AddResult
	var subs []string
	for _, line := range strings.Fields(input) {
		switch {
		case core.IsSubscriptionURL(line):
			sub := s.store.AddSubscription(line)
			subs = append(subs, sub.ID)
			r.Subscriptions = append(r.Subscriptions, sub.Title())
		case core.IsSupportedScheme(line):
			if _, err := s.store.AddServer(line); err != nil {
				r.Errors = append(r.Errors, err.Error())
			} else {
				r.Added++
			}
		case strings.Contains(line, "://"):
			r.Errors = append(r.Errors, "Протокол "+line[:strings.Index(line, "://")]+" пока не поддерживается.")
		default:
			short := line
			if len([]rune(short)) > 40 {
				short = string([]rune(short)[:40]) + "…"
			}
			r.Errors = append(r.Errors, "Не похоже на ссылку: "+short)
		}
	}
	s.store.SelectFirstIfNone()
	for _, id := range subs {
		go func(id string) { _, _ = s.Refresh(id) }(id)
	}
	return r
}
