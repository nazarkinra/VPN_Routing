package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"vpnrouting/desktop/core"
	"vpnrouting/desktop/netenv"
)

// Дополнительные туннели (как TunnelStore.java в Android 2.1). Каждый туннель — свой сервер, параллельный
// основному VPN: в него уходят выбранные программы целиком и выбранные домены. Это не отдельные TUN,
// а дополнительные выходы того же ядра Xray; трафик раскладывают правила маршрутизации.

// Tunnel — дополнительный туннель.
type Tunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Server — ссылка сервера (как ключ основного VPN: переживает обновление подписки). "" — не выбран.
	Server  string `json:"server"`
	Enabled bool   `json:"enabled"`
	// Apps — имена программ (telegram.exe, firefox), которые целиком идут через туннель.
	Apps []string `json:"apps"`
	// Domains — домены (с поддоменами) и IP-подсети туннеля.
	Domains []string `json:"domains"`
}

// Link — разобранная ссылка сервера или nil.
func (t *Tunnel) Link() *core.ProxyLink {
	if t.Server == "" {
		return nil
	}
	l, err := core.ParseLink(t.Server)
	if err != nil {
		return nil
	}
	return l
}

// IsActive — туннелю есть что делать: включён, выбран сервер и есть хотя бы одно правило.
func (t *Tunnel) IsActive() bool {
	return t.Enabled && t.Link() != nil && (len(t.Apps) > 0 || len(t.Domains) > 0)
}

func copyTunnel(t *Tunnel) Tunnel {
	c := *t
	c.Apps = append([]string{}, t.Apps...)
	c.Domains = append([]string{}, t.Domains...)
	return c
}

// ---------- Хранение ----------

// Tunnels — копии всех туннелей по порядку (номер туннеля = позиция + 2: 1 — основной VPN).
func (s *Store) Tunnels() []Tunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]Tunnel, len(s.d.Tunnels))
	for i, t := range s.d.Tunnels {
		r[i] = copyTunnel(t)
	}
	return r
}

func (s *Store) tunnelLocked(id string) *Tunnel {
	for _, t := range s.d.Tunnels {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// AddTunnel создаёт туннель. Числа туннелей не ограничено: простаивающий туннель не держит соединений.
func (s *Store) AddTunnel(name string) Tunnel {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		name = fmt.Sprintf("Туннель %d", len(s.d.Tunnels)+2)
	}
	t := &Tunnel{ID: newID(), Name: name, Enabled: true, Apps: []string{}, Domains: []string{}}
	s.d.Tunnels = append(s.d.Tunnels, t)
	s.save()
	return copyTunnel(t)
}

// SetTunnel меняет название, сервер и включённость туннеля.
func (s *Store) SetTunnel(id, name, server string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnelLocked(id)
	if t == nil {
		return errors.New("туннель не найден")
	}
	if name = strings.TrimSpace(name); name != "" {
		t.Name = name
	}
	t.Server = strings.TrimSpace(server)
	t.Enabled = enabled
	s.save()
	return nil
}

func (s *Store) RemoveTunnel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.d.Tunnels {
		if t.ID == id {
			s.d.Tunnels = append(s.d.Tunnels[:i:i], s.d.Tunnels[i+1:]...)
			s.save()
			return
		}
	}
}

// SetTunnelApps задаёт программы туннеля. Программа может быть только в одном туннеле: из других она убирается.
func (s *Store) SetTunnelApps(id string, apps []string) error {
	apps = uniqueNames(apps)
	taken := map[string]bool{}
	for _, a := range apps {
		taken[strings.ToLower(a)] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tunnelLocked(id) == nil {
		return errors.New("туннель не найден")
	}
	for _, t := range s.d.Tunnels {
		if t.ID == id {
			t.Apps = apps
			continue
		}
		kept := []string{}
		for _, a := range t.Apps {
			if !taken[strings.ToLower(a)] {
				kept = append(kept, a)
			}
		}
		t.Apps = kept
	}
	s.save()
	return nil
}

// AddTunnelDomain добавляет домен, адрес сайта или IP-подсеть. Возвращает нормализованное значение или "",
// если адрес уже есть в туннеле. Адрес, который уже идёт через другой туннель, не добавляется.
// Вложенные домены заменяются родителем.
func (s *Store) AddTunnelDomain(id, raw string) (string, error) {
	value, err := core.NormalizeException(raw)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnelLocked(id)
	if t == nil {
		return "", errors.New("туннель не найден")
	}
	cidr := core.IsCIDR(value)
	c := core.Canonical(value)
	covers := func(item string) bool {
		if cidr || core.IsCIDR(item) {
			return item == value
		}
		p := core.Canonical(item)
		return c == p || strings.HasSuffix(c, "."+p)
	}
	for _, other := range s.d.Tunnels {
		if other.ID == id {
			continue
		}
		for _, d := range other.Domains {
			if covers(d) {
				return "", fmt.Errorf("Этот адрес уже идёт через туннель «%s». Уберите его там, если хотите перенести сюда.", other.Name)
			}
		}
	}
	for _, d := range t.Domains {
		if covers(d) {
			return "", nil
		}
	}
	kept := []string{}
	for _, d := range t.Domains {
		if cidr || core.IsCIDR(d) || !strings.HasSuffix(core.Canonical(d), "."+c) {
			kept = append(kept, d)
		}
	}
	t.Domains = append(kept, value)
	s.save()
	return value, nil
}

func (s *Store) RemoveTunnelDomain(id, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tunnelLocked(id)
	if t == nil {
		return
	}
	for i, d := range t.Domains {
		if d == value {
			t.Domains = append(t.Domains[:i:i], t.Domains[i+1:]...)
			s.save()
			return
		}
	}
}

// ---------- Состояние туннелей в работающем VPN ----------

// TunnelStatus — состояние туннеля для окна.
type TunnelStatus struct {
	OK bool `json:"ok"`
	// Delay — задержка через сервер туннеля, мс; 0 — ещё меряется, -1 — не отвечает.
	Delay int64  `json:"delay"`
	Text  string `json:"text"`
}

// tunnelProbeTimeout — сколько ждать замер задержки сервера туннеля.
const tunnelProbeTimeout = 15 * time.Second

type tunnelStates struct {
	mu         sync.Mutex
	m          map[string]TunnelStatus
	generation int
}

// TunnelStatus — состояние туннеля; false — туннель сейчас не запущен.
func (v *VPN) TunnelStatus(id string) (TunnelStatus, bool) {
	v.tunnels.mu.Lock()
	defer v.tunnels.mu.Unlock()
	st, ok := v.tunnels.m[id]
	return st, ok
}

func (v *VPN) clearTunnelStatus() {
	v.tunnels.mu.Lock()
	v.tunnels.generation++
	v.tunnels.m = nil
	v.tunnels.mu.Unlock()
}

// resolveTunnels — туннели для конфига: включённые, с сервером и правилами. Адреса серверов разрешаются
// параллельно. Туннель, чей сервер не найден, пропускается: его программы и домены идут по правилам
// основного VPN, а в окне видна ошибка. Только в рабочем потоке.
func (v *VPN) resolveTunnels() []core.TunnelRoute {
	all := v.store.Tunnels()
	type lookup struct {
		ip     string
		method string
		err    error
	}
	results := make([]chan lookup, len(all))
	for i := range all {
		t := &all[i]
		if !t.IsActive() {
			continue
		}
		ch := make(chan lookup, 1)
		results[i] = ch
		server := t.Link().Server
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ip, method, err := netenv.ResolveServer(ctx, server)
			ch <- lookup{ip, method, err}
		}()
	}
	status := map[string]TunnelStatus{}
	var routes []core.TunnelRoute
	for i := range all {
		t := &all[i]
		if results[i] == nil {
			continue
		}
		r := <-results[i]
		if r.err != nil {
			logf("tunnel %s: %v", t.Name, r.err)
			status[t.ID] = TunnelStatus{OK: false, Delay: -1,
				Text: "Адрес сервера не найден: " + r.err.Error() + ". Трафик туннеля идёт через основной VPN."}
			continue
		}
		link := t.Link()
		route := core.TunnelRoute{Tag: fmt.Sprintf("tunnel-%d", i+1), Link: link, ServerIP: r.ip,
			Processes: t.Apps, Domains: t.Domains}
		routes = append(routes, route)
		status[t.ID] = TunnelStatus{OK: true, Text: "Проверка сервера…"}
		logf("%s %s: %s via %s (%s), apps %d, domains %d", route.Tag, t.Name, link.Describe(), r.ip, r.method,
			len(t.Apps), len(t.Domains))
	}
	v.tunnels.mu.Lock()
	v.tunnels.generation++
	gen := v.tunnels.generation
	v.tunnels.m = status
	v.tunnels.mu.Unlock()
	v.measureTunnels(all, routes, gen)
	return routes
}

// measureTunnels — задержка до серверов туннелей (реальный запрос через каждый сервер, как пинг серверов).
func (v *VPN) measureTunnels(all []Tunnel, routes []core.TunnelRoute, gen int) {
	byTag := map[string]string{}
	for i := range all {
		byTag[fmt.Sprintf("tunnel-%d", i+1)] = all[i].ID
	}
	for _, r := range routes {
		id, route := byTag[r.Tag], r
		go func() {
			ms := int64(-1)
			done := make(chan int64, 1)
			go func() {
				cfg, err := core.BuildPingConfig(route.Link, route.ServerIP)
				if err != nil {
					done <- -1
					return
				}
				d, err := measureOutboundDelay(cfg, core.DelayTestURL)
				if err != nil {
					d = -1
				}
				done <- d
			}()
			select {
			case ms = <-done:
			case <-time.After(tunnelProbeTimeout):
			}
			v.tunnels.mu.Lock()
			if v.tunnels.generation != gen || v.tunnels.m == nil {
				v.tunnels.mu.Unlock()
				return
			}
			if _, ok := v.tunnels.m[id]; !ok {
				v.tunnels.mu.Unlock()
				return
			}
			if ms > 0 {
				v.tunnels.m[id] = TunnelStatus{OK: true, Delay: ms, Text: fmt.Sprintf("Работает · %d мс", ms)}
			} else {
				v.tunnels.m[id] = TunnelStatus{OK: false, Delay: -1, Text: "Сервер не отвечает — сайты туннеля не откроются"}
			}
			v.tunnels.mu.Unlock()
			v.notify()
		}()
	}
}
