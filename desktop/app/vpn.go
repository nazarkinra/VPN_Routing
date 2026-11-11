package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	xcore "github.com/xtls/xray-core/core"

	"vpnrouting/desktop/core"
	"vpnrouting/desktop/netenv"
)

// VPN: ядро Xray с TUN. Аналог RoutingVpnService.java (и служб xray + sing-box на Raspberry Pi).

type State string

const (
	StateStopped    State = "stopped"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateError      State = "error"
)

// TunName — имя интерфейса TUN (адаптера Wintun на Windows).
func TunName() string {
	if runtime.GOOS == "windows" {
		return "VPN routing"
	}
	return "vpnrouting0"
}

// Status — состояние для интерфейса.
type Status struct {
	State   State  `json:"state"`
	Message string `json:"message"`
	// Since — момент подключения, мс Unix; 0 — не подключено.
	Since int64 `json:"since"`
	// Delay — последняя задержка через VPN в мс, -1 — не отвечает, 0 — не измерялась.
	Delay int64 `json:"delay"`
}

// VPN — служба основного VPN. Все действия выполняются по очереди в одном потоке, как в Android-службе.
type VPN struct {
	store   *Store
	bmstu   *Bmstu
	dataDir string

	work chan func()

	mu       sync.Mutex
	status   Status
	inst     *xcore.Instance
	onChange []func()

	// Только в рабочем потоке
	appliedDNS   string
	appliedBmstu bool
	watching     chan struct{}
	lastUplink   *netenv.Uplink

	// Для диагностики «сервер не отвечает»: сервер, его IP и чем IP получен
	diagMu     sync.Mutex
	diagLink   *core.ProxyLink
	diagIP     string
	diagMethod string
}

// NewVPN создаёт службу. dataDir — папка настроек (журнал ядра).
func NewVPN(store *Store, dataDir string) *VPN {
	v := &VPN{store: store, dataDir: dataDir, work: make(chan func(), 64)}
	v.status = Status{State: StateStopped, Message: "Отключено"}
	v.bmstu = newBmstu(store)
	netenv.SetTun(TunName(), core.TunDNS)
	go func() {
		for f := range v.work {
			f()
		}
	}()
	return v
}

// Bmstu — менеджер туннеля МГТУ.
func (v *VPN) Bmstu() *Bmstu { return v.bmstu }

// OnChange — подписка на смену состояния (значок в трее).
func (v *VPN) OnChange(f func()) {
	v.mu.Lock()
	v.onChange = append(v.onChange, f)
	v.mu.Unlock()
}

// Status — снимок состояния.
func (v *VPN) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.status
}

// Active — VPN подключён или подключается.
func (v *VPN) Active() bool {
	s := v.Status().State
	return s == StateConnected || s == StateConnecting
}

func (v *VPN) post(f func()) { v.work <- f }

func (v *VPN) setState(state State, message string) {
	v.mu.Lock()
	if state == StateConnected && v.status.State != StateConnected {
		v.status.Since = time.Now().UnixMilli()
	}
	if state == StateStopped || state == StateError {
		v.status.Since = 0
		v.status.Delay = 0
	}
	v.status.State, v.status.Message = state, message
	listeners := append([]func(){}, v.onChange...)
	v.mu.Unlock()
	logf("vpn %s: %s", state, message)
	for _, f := range listeners {
		f()
	}
}

// ---------- Команды ----------

// Start — подключить с выбранным сервером.
func (v *VPN) Start() { v.post(v.startInternal) }

// Stop — отключить.
func (v *VPN) Stop() { v.post(v.stopEverything) }

// Toggle — переключить.
func (v *VPN) Toggle() {
	if v.Active() {
		v.Stop()
	} else {
		v.Start()
	}
}

// Reload — пересобрать конфиг после изменения исключений (туннель перезапускается).
func (v *VPN) Reload() {
	v.post(func() {
		if v.inst != nil {
			v.restartCore("Правила обновлены")
		}
	})
}

// ApplyKey — сменить сервер; если новый не запустится, вернуть прежний (как update-vpn).
func (v *VPN) ApplyKey(url string) { v.post(func() { v.applyKeyInternal(url) }) }

// BmstuSync — изменились настройки VPN МГТУ: обновить маршруты и переподключить туннель.
func (v *VPN) BmstuSync() {
	v.post(func() {
		if v.inst == nil {
			return
		}
		// Маршрут bmstu.ru добавляется в Xray или убирается из него только при включении и выключении
		if v.appliedBmstu != v.store.BmstuEnabled() {
			v.restartCore("Маршруты МГТУ обновлены")
		}
		v.bmstu.Restart()
	})
}

// ApplyBmstuOvpn передаёт новый .ovpn МГТУ на проверку подключением. false — VPN не запущен.
func (v *VPN) ApplyBmstuOvpn(text string) bool {
	if !v.Active() {
		return false
	}
	v.bmstu.ApplyOvpn(text)
	return true
}

// Shutdown — остановить всё и дождаться (выход из приложения).
func (v *VPN) Shutdown() {
	done := make(chan struct{})
	v.post(func() {
		if v.inst != nil || v.watching != nil {
			v.stopEverything()
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	v.bmstu.shutdown()
}

// MeasureDelay — проверка связи через VPN-сервер, как «адрес через прокси» в check-vpn.
func (v *VPN) MeasureDelay() (int64, error) {
	v.mu.Lock()
	inst := v.inst
	v.mu.Unlock()
	if inst == nil {
		return 0, errors.New("VPN не подключён.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ms, err := measureInstDelay(ctx, inst, core.DelayTestURL)
	v.mu.Lock()
	if err != nil {
		v.status.Delay = -1
	} else {
		v.status.Delay = ms
	}
	v.mu.Unlock()
	return ms, err
}

// ---------- Рабочий поток ----------

// Preflight — проверка перед подключением, зависящая от системы (wintun.dll на Windows).
var Preflight func() error

func (v *VPN) startInternal() {
	if v.inst != nil {
		v.setState(StateConnected, v.Status().Message)
		return
	}
	if Preflight != nil {
		if err := Preflight(); err != nil {
			v.setState(StateError, err.Error())
			return
		}
	}
	url := v.store.ActiveURL()
	if url == "" {
		v.setState(StateError, "Сервер не выбран. Добавьте ссылку vless://, hysteria2:// или подписку.")
		return
	}
	v.setState(StateConnecting, "Подключение…")
	if err := v.startCore(url); err != nil {
		logf("start failed: %v", err)
		v.stopEverything()
		v.setState(StateError, err.Error())
		return
	}
	v.watchNetwork()
	v.setState(StateConnected, "Подключено")
	v.checkConnection()
	v.bmstu.Sync()
}

func (v *VPN) applyKeyInternal(url string) {
	previous := v.store.ActiveURL()
	wasRunning := v.inst != nil
	if !wasRunning {
		v.store.SetActiveURL(url)
		return
	}
	v.setState(StateConnecting, "Применение ключа…")
	if err := v.startCore(url); err != nil {
		logf("new key failed: %v", err)
		msg := "Ключ не применён: " + err.Error()
		if previous != "" {
			if err2 := v.startCore(previous); err2 == nil {
				v.setState(StateConnected, msg+" Работает прежний ключ.")
				return
			} else {
				logf("rollback failed: %v", err2)
			}
		}
		v.stopEverything()
		v.setState(StateError, msg)
		return
	}
	// Ключ сохраняется только после успешного запуска
	v.store.SetActiveURL(url)
	v.setState(StateConnected, "Новый ключ применён")
	v.checkConnection()
	v.bmstu.Sync()
}

func (v *VPN) restartCore(okMessage string) {
	if err := v.startCore(v.store.ActiveURL()); err != nil {
		logf("restart failed: %v", err)
		v.stopEverything()
		v.setState(StateError, err.Error())
		return
	}
	v.setState(StateConnected, okMessage)
}

func (v *VPN) tunOptions() core.TunOptions {
	p := v.store.Prefs()
	o := core.TunOptions{
		Name:    TunName(),
		IPv6:    p.IPv6 && netenv.HasIPv6(),
		LogFile: filepath.Join(v.dataDir, "xray.log"),
	}
	switch runtime.GOOS {
	case "windows":
		// Windows сам опрашивает DNS адаптера туннеля; фильтры WFP не дают запросам уйти мимо него
		o.SetDNS = true
		o.BlockDNSLeak = p.BlockDNSLeak
	case "linux":
		o.DNSToGateway = netenv.HasSystemdResolved()
	}
	return o
}

// startCore — разбор ключа, разрешение имени сервера, сборка конфига и (пере)запуск ядра.
func (v *VPN) startCore(url string) error {
	link, err := core.ParseLink(url)
	if err != nil {
		return err
	}
	up, err := netenv.Refresh()
	if err != nil {
		return fmt.Errorf("Нет подключения к сети: %v", err)
	}
	serverIP, method, err := netenv.ResolveServer(context.Background(), link.Server)
	if err != nil {
		return err
	}
	logf("server %s -> %s (%s)", link.Server, serverIP, method)
	var route *core.BmstuRoute
	if v.store.BmstuEnabled() {
		p, err := v.bmstu.EnsureProxy()
		if err != nil {
			return fmt.Errorf("прокси МГТУ: %v", err)
		}
		route = &core.BmstuRoute{Port: p.Port(), User: p.User(), Password: p.Password()}
	}
	config, err := core.BuildConfig(link, serverIP, up.DNS, v.store.Exceptions(), v.store.Builtins(), route, v.tunOptions())
	if err != nil {
		return err
	}
	v.stopCore()
	inst, err := startXray(config)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.inst = inst
	v.mu.Unlock()
	v.appliedDNS = up.DNS
	v.appliedBmstu = route != nil
	v.lastUplink = up
	v.diagMu.Lock()
	v.diagLink, v.diagIP, v.diagMethod = link, serverIP, method
	v.diagMu.Unlock()
	logf("Xray started: %s via %s, uplink %s, local DNS %q", link.Describe(), serverIP, up.Name, up.DNS)
	saveConfigDump(v.dataDir, config)
	return nil
}

func (v *VPN) stopCore() {
	v.mu.Lock()
	inst := v.inst
	v.inst = nil
	v.mu.Unlock()
	if inst != nil {
		if err := inst.Close(); err != nil {
			logf("xray close: %v", err)
		}
	}
	resetDialerControllers()
}

func (v *VPN) stopEverything() {
	v.bmstu.StopAll()
	v.stopCore()
	v.unwatchNetwork()
	if v.Status().State != StateError {
		v.setState(StateStopped, "Отключено")
	}
}

// checkConnection — проверка связи после подключения: задержка или предупреждение.
func (v *VPN) checkConnection() {
	go func() {
		ms, err := v.MeasureDelay()
		st := v.Status()
		if st.State != StateConnected {
			return
		}
		if err != nil {
			v.setState(StateConnected, st.Message+". Сервер не отвечает: "+err.Error()+v.diagnose())
		} else {
			v.setState(StateConnected, fmt.Sprintf("%s. Задержка: %d мс", st.Message, ms))
		}
	}()
}

// ---------- Слежение за сетью ----------

// watchNetwork раз в 3 с проверяет физическую сеть. Сменился DNS (дом -> мобильный интернет) —
// ядро перезапускается, чтобы российские домены резолвились через DNS новой сети; сменился
// интерфейс — переподключается туннель МГТУ (его UDP был привязан к прежней сети).
func (v *VPN) watchNetwork() {
	if v.watching != nil {
		return
	}
	stop := make(chan struct{})
	v.watching = stop
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
			}
			up, err := netenv.Refresh()
			if err != nil {
				continue
			}
			v.post(func() {
				if v.watching != stop || v.inst == nil {
					return
				}
				prev := v.lastUplink
				v.lastUplink = up
				if up.DNS != v.appliedDNS {
					logf("network DNS changed: %q -> %q", v.appliedDNS, up.DNS)
					v.restartCore("Подключено (сменилась сеть)")
				}
				if prev != nil && (prev.Index != up.Index || prev.Name != up.Name) && v.store.BmstuEnabled() {
					logf("uplink changed: %s -> %s", prev.Name, up.Name)
					v.bmstu.Restart()
				}
			})
		}
	}()
}

func (v *VPN) unwatchNetwork() {
	if v.watching != nil {
		close(v.watching)
		v.watching = nil
	}
}

// diagnose — где сломалось: (а) чем получен адрес сервера, (б) открывается ли до него TCP-соединение.
// Для Hysteria2 (UDP) TCP не проверить — сообщается, что сеть может блокировать UDP.
func (v *VPN) diagnose() string {
	v.diagMu.Lock()
	link, ip, method := v.diagLink, v.diagIP, v.diagMethod
	v.diagMu.Unlock()
	if link == nil || ip == "" {
		return ""
	}
	where := fmt.Sprintf(" Адрес %s (%s)", ip, method)
	if link.IsHysteria() {
		logf("diagnose: hysteria over UDP, %s:%d", ip, link.Port)
		return where + ". Hysteria2 работает по UDP: корпоративные сети его часто блокируют, выберите сервер VLESS (TCP)."
	}
	d := net.Dialer{Timeout: 4 * time.Second, Control: netenv.BindControl}
	c, err := d.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(link.Port)))
	if err != nil {
		logf("diagnose: TCP %s:%d failed: %v", ip, link.Port, err)
		return fmt.Sprintf("%s, TCP-порт %d недоступен из этой сети.", where, link.Port)
	}
	_ = c.Close()
	logf("diagnose: TCP %s:%d ok", ip, link.Port)
	return fmt.Sprintf("%s, TCP-порт %d открыт: соединение есть, но сервер не принимает ключ или не выпускает в интернет.",
		where, link.Port)
}

// saveConfigDump — копия рабочего конфига ядра для проверки, со скрытыми UUID, ключами и паролями.
func saveConfigDump(dir, config string) {
	masked, err := core.MaskConfig(config)
	if err != nil {
		return
	}
	if err := os.WriteFile(ConfigDumpPath(dir), []byte(masked), 0o600); err != nil {
		logf("config dump: %v", err)
	}
}

// ConfigDumpPath — где лежит копия конфига ядра.
func ConfigDumpPath(dir string) string { return filepath.Join(dir, "xray_config.json") }
