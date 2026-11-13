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

// Status — состояние основного VPN для интерфейса. VPN МГТУ — отдельная часть со своим состоянием
// (Bmstu().Status() и CorpOn()); обе части работают в одном ядре Xray, но включаются независимо.
type Status struct {
	State   State  `json:"state"`
	Message string `json:"message"`
	// Since — момент подключения, мс Unix; 0 — не подключено.
	Since int64 `json:"since"`
	// Delay — последняя задержка через VPN в мс, -1 — не отвечает, 0 — не измерялась.
	Delay int64 `json:"delay"`
}

// VPN — служба: ядро Xray с TUN. Работает, пока включена хотя бы одна часть — основной VPN или VPN МГТУ.
// Все действия выполняются по очереди в одном потоке, как в Android-службе.
type VPN struct {
	store   *Store
	bmstu   *Bmstu
	dataDir string

	work chan func()

	mu       sync.Mutex
	status   Status
	inst     *xcore.Instance
	onChange []func()

	// Только в рабочем потоке. Включён ли VPN МГТУ — флаг в настройках (store.BmstuEnabled).
	mainOn       bool
	appliedDNS   string
	appliedBmstu bool
	watching     chan struct{}
	lastUplink   *netenv.Uplink

	// tunnels — состояние дополнительных туннелей в работающем ядре
	tunnels tunnelStates

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

// Status — снимок состояния основного VPN.
func (v *VPN) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.status
}

// Running — работает ядро (включена хотя бы одна часть).
func (v *VPN) Running() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.inst != nil
}

// MainActive — основной VPN подключён или подключается.
func (v *VPN) MainActive() bool {
	s := v.Status().State
	return s == StateConnected || s == StateConnecting
}

// CorpOn — VPN МГТУ включён (туннель подключён, подключается или ждёт повтора).
func (v *VPN) CorpOn() bool { return v.Running() && v.store.BmstuEnabled() }

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
	v.mu.Unlock()
	logf("vpn %s: %s", state, message)
	v.notify()
}

func (v *VPN) notify() {
	v.mu.Lock()
	listeners := append([]func(){}, v.onChange...)
	v.mu.Unlock()
	for _, f := range listeners {
		f()
	}
}

// ---------- Команды ----------

// Start — включить основной VPN (VPN МГТУ, если работает, не трогается).
func (v *VPN) Start() {
	v.post(func() {
		// Всё было выключено: включается только основной (флаг МГТУ мог остаться от прошлого запуска)
		if v.inst == nil {
			v.store.SetBmstuEnabled(false)
		}
		v.mainOn = true
		v.apply()
	})
}

// StopMain — выключить основной VPN (VPN МГТУ, если включён, продолжает работать).
func (v *VPN) StopMain() {
	v.post(func() {
		v.mainOn = false
		v.apply()
	})
}

// StartCorp — включить VPN МГТУ (основной VPN не трогается).
func (v *VPN) StartCorp() {
	v.post(func() {
		v.store.SetBmstuEnabled(true)
		v.apply()
	})
}

// StopCorp — выключить VPN МГТУ (основной VPN не трогается).
func (v *VPN) StopCorp() {
	v.post(func() {
		v.store.SetBmstuEnabled(false)
		v.apply()
	})
}

// Restore — включить части, как в прошлый раз: основной — если main, МГТУ — по сохранённому флагу.
func (v *VPN) Restore(main bool) {
	v.post(func() {
		v.mainOn = main
		v.apply()
	})
}

// Stop — выключить всё.
func (v *VPN) Stop() { v.post(func() { v.stopEverything(true) }) }

// Toggle — переключить основной VPN.
func (v *VPN) Toggle() {
	if v.MainActive() {
		v.StopMain()
	} else {
		v.Start()
	}
}

// ToggleCorp — переключить VPN МГТУ.
func (v *VPN) ToggleCorp() {
	if v.CorpOn() {
		v.StopCorp()
	} else {
		v.StartCorp()
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

// BmstuSync — изменились логин, пароль или файл МГТУ: переподключить туннель, если он включён.
func (v *VPN) BmstuSync() {
	v.post(func() {
		if v.inst != nil && v.store.BmstuEnabled() {
			v.bmstu.Restart()
		}
	})
}

// ApplyBmstuOvpn передаёт новый .ovpn МГТУ на проверку подключением. false — VPN МГТУ не включён.
func (v *VPN) ApplyBmstuOvpn(text string) bool {
	if !v.CorpOn() {
		return false
	}
	v.bmstu.ApplyOvpn(text)
	return true
}

// Shutdown — остановить всё и дождаться (выход из приложения). Флаги частей сохраняются.
func (v *VPN) Shutdown() {
	done := make(chan struct{})
	v.post(func() {
		if v.inst != nil || v.watching != nil {
			v.stopEverything(false)
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
	main := v.status.State == StateConnected || v.status.State == StateConnecting
	v.mu.Unlock()
	if inst == nil || !main {
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

// apply приводит ядро в соответствие с включёнными частями: обе выключены — всё останавливается,
// иначе конфиг Xray пересобирается (основной сервер — если включён основной, bmstu.ru — если МГТУ).
func (v *VPN) apply() {
	corp := v.store.BmstuEnabled()
	if !v.mainOn && !corp {
		v.stopEverything(true)
		return
	}
	if v.inst == nil && Preflight != nil {
		if err := Preflight(); err != nil {
			v.fail(err)
			return
		}
	}
	if v.mainOn {
		if v.store.ActiveURL() == "" {
			v.fail(errors.New("Сервер не выбран. Добавьте ссылку vless://, hysteria2:// или подписку."))
			return
		}
		if !v.MainActive() || v.appliedLinkMissing() {
			v.setState(StateConnecting, "Подключение…")
		}
	}
	if err := v.startCore(v.store.ActiveURL()); err != nil {
		v.fail(err)
		return
	}
	v.watchNetwork()
	if v.mainOn {
		if st := v.Status(); st.State != StateConnected {
			v.setState(StateConnected, "Подключено")
			v.checkConnection()
		} else {
			v.notify()
		}
	} else if v.Status().State != StateError {
		v.setState(StateStopped, "Отключено")
	} else {
		v.notify()
	}
	v.bmstu.Sync()
}

// appliedLinkMissing — в работающем ядре нет основного сервера (было включено только МГТУ).
func (v *VPN) appliedLinkMissing() bool {
	v.diagMu.Lock()
	defer v.diagMu.Unlock()
	return v.diagLink == nil
}

// fail — ошибка запуска. Если не поднялся основной VPN, а МГТУ включён, МГТУ продолжает работать один.
func (v *VPN) fail(err error) {
	logf("start failed: %v", err)
	main := v.mainOn
	if main && v.store.BmstuEnabled() {
		v.mainOn = false
		if err2 := v.startCore(""); err2 == nil {
			v.watchNetwork()
			v.setState(StateError, err.Error())
			v.bmstu.Sync()
			return
		} else {
			logf("BMSTU-only fallback failed: %v", err2)
		}
	}
	v.stopEverything(true)
	if main {
		v.setState(StateError, err.Error())
	} else {
		v.bmstu.reportError(err.Error())
		v.notify()
	}
}

func (v *VPN) applyKeyInternal(url string) {
	previous := v.store.ActiveURL()
	// Основной VPN выключен: новый сервер просто запоминается
	if v.inst == nil || !v.mainOn {
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
		v.fail(errors.New(msg))
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
		v.fail(err)
		return
	}
	if v.mainOn {
		v.setState(StateConnected, okMessage)
	}
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
// Сервер url — только если включён основной VPN; маршрут bmstu.ru — если включён VPN МГТУ.
func (v *VPN) startCore(url string) error {
	var link *core.ProxyLink
	if v.mainOn {
		l, err := core.ParseLink(url)
		if err != nil {
			return err
		}
		link = l
	}
	up, err := netenv.Refresh()
	if err != nil {
		return fmt.Errorf("Нет подключения к сети: %v", err)
	}
	var serverIP, method string
	if link != nil {
		serverIP, method, err = netenv.ResolveServer(context.Background(), link.Server)
		if err != nil {
			return err
		}
		logf("server %s -> %s (%s)", link.Server, serverIP, method)
	}
	var route *core.BmstuRoute
	if v.store.BmstuEnabled() {
		p, err := v.bmstu.EnsureProxy()
		if err != nil {
			return fmt.Errorf("прокси МГТУ: %v", err)
		}
		route = &core.BmstuRoute{Port: p.Port(), User: p.User(), Password: p.Password()}
	}
	// Дополнительные туннели — только вместе с основным VPN
	var tunnels []core.TunnelRoute
	if link != nil {
		tunnels = v.resolveTunnels()
	} else {
		v.clearTunnelStatus()
	}
	config, err := core.BuildConfig(link, serverIP, up.DNS, v.store.Exceptions(), v.store.Builtins(), route,
		v.store.AppRules(), tunnels, v.tunOptions())
	if err != nil {
		v.clearTunnelStatus()
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
	if link != nil {
		logf("Xray started: %s via %s, tunnels %d, uplink %s, local DNS %q", link.Describe(), serverIP, len(tunnels),
			up.Name, up.DNS)
	} else {
		logf("Xray started: only BMSTU VPN, uplink %s, local DNS %q", up.Name, up.DNS)
	}
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

// stopEverything останавливает ядро и туннель МГТУ. resetFlags — выключить обе части (иначе флаг МГТУ
// сохраняется для следующего запуска: выход из приложения).
func (v *VPN) stopEverything(resetFlags bool) {
	v.mainOn = false
	if resetFlags {
		v.store.SetBmstuEnabled(false)
	}
	v.bmstu.StopAll()
	v.stopCore()
	v.clearTunnelStatus()
	v.unwatchNetwork()
	v.diagMu.Lock()
	v.diagLink = nil
	v.diagMu.Unlock()
	if v.Status().State != StateError {
		v.setState(StateStopped, "Отключено")
	} else {
		v.notify()
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
