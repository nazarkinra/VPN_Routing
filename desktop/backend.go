package main

import (
	"os"
	"runtime"
	"sync"
	"time"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/app"
)

// backend — VPN, туннель МГТУ, подписки, обновления и сокет управления (на Windows — в одном процессе
// с окном, на Linux — служба root).
type backend struct {
	dir     string
	store   *app.Store
	vpn     *app.VPN
	servers *app.Servers
	updater *app.Updater
	server  *api.Server
	socket  string

	quitOnce sync.Once
	quitCh   chan struct{}
	// restartArgs — после обновления запустить новую программу с этими флагами.
	restartArgs []string
}

func startBackend(dir string, flags map[string]bool) *backend {
	store, err := app.OpenStore(dir)
	if err != nil {
		fail("Не удалось открыть настройки: " + err.Error())
	}
	app.InitLog(dir)
	app.Logf("VPN routing %s (%s/%s) запущен, настройки: %s", version, runtime.GOOS, runtime.GOARCH, dir)
	app.Preflight = preflight

	// Старая программа после обновления; в Windows её файл занят, пока прежний процесс не завершится
	go func() {
		for i := 0; i < 10; i++ {
			app.CleanupOldExecutable()
			time.Sleep(3 * time.Second)
		}
	}()
	if flags["after-update"] {
		// Прежний экземпляр ещё закрывает сокет
		for i := 0; i < 50; i++ {
			if _, ok := running(dir); !ok {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	b := &backend{dir: dir, store: store, quitCh: make(chan struct{})}
	b.vpn = app.NewVPN(store, dir)
	b.servers = app.NewServers(store)
	b.updater = app.NewUpdater(store, dir)
	// Свои исключения, уже покрытые встроенными правилами (ya.ru при .ru), убираются из списка
	store.CompactExceptions()

	// Перезапуск новой программой после обновления: VPN поднимется снова, если был включён
	b.updater.Restart = func() {
		b.restartArgs = []string{"--after-update", "--hidden"}
		// Флаг VPN МГТУ сохраняется в настройках; --connect-corp — поднять его и без основного
		if b.vpn.MainActive() {
			b.restartArgs = append(b.restartArgs, "--connect")
		} else if b.vpn.CorpOn() {
			b.restartArgs = append(b.restartArgs, "--connect-corp")
		}
		b.quit()
	}
	go b.updater.AutoCheckLoop()

	b.server = &api.Server{Store: store, VPN: b.vpn, Servers: b.servers, Updater: b.updater, DataDir: dir,
		Quit: b.quit, Version: version}
	// Сокет-файлы прежних сборок (на Windows их может удалить только процесс с правами администратора)
	api.CleanupLegacySockets(dir)
	b.socket = api.SocketPath(dir)
	ln, err := api.Listen(b.socket)
	if err != nil {
		fail("Не удалось создать сокет управления " + b.socket + ": " + err.Error())
	}
	b.server.Serve(ln)

	// Устаревшие подписки обновляются при запуске и затем раз в полчаса
	go func() {
		b.servers.RefreshAll(true)
		for range time.Tick(30 * time.Minute) {
			b.servers.RefreshAll(true)
		}
	}()
	// Части VPN поднимаются, как были: основной — по флагу или настройке, МГТУ — если был включён
	switch {
	case flags["connect"] || store.Prefs().ConnectOnStart:
		b.vpn.Restore(true)
	case flags["connect-corp"]:
		b.vpn.Restore(false)
	default:
		store.SetBmstuEnabled(false)
	}
	return b
}

// quit — выключить VPN и закрыть приложение (команда, «Выход», сигнал, перезапуск после обновления).
func (b *backend) quit() { b.quitOnce.Do(func() { close(b.quitCh) }) }

func (b *backend) shutdown() {
	app.Logf("выход")
	b.vpn.Shutdown()
	_ = os.Remove(b.socket)
	if b.restartArgs != nil {
		restartAfterUpdate(b.restartArgs)
	}
}

// restartAfterUpdate запускает обновлённую программу. Под systemd — выход с ошибкой: служба
// (Restart=on-failure) сама запустит новую программу, а дочерний процесс systemd убил бы вместе со старым.
func restartAfterUpdate(args []string) {
	if os.Getenv("INVOCATION_ID") != "" {
		os.Exit(75)
	}
	// Служба Windows: её перезапускает диспетчер служб (выход с кодом ошибки, см. winService)
	if app.ServiceMode {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		err = spawnDetached(exe, args)
	}
	if err != nil {
		app.Logf("перезапуск после обновления: %v", err)
	}
}
