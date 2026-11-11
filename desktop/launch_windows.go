package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/svc"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/app"
	"vpnrouting/desktop/gui"
	"vpnrouting/desktop/install"
)

// launch — Windows.
//
//   - Установленная программа: VPN работает в службе «VPN routing» (LocalSystem, флаг --service), а это —
//     окно без прав администратора (как у Happ). Запрос прав — только при установке.
//   - Переносная версия (из zip, службы нет): VPN и окно в одном процессе с правами администратора.
func launch(dir string, args []string, flags map[string]bool) {
	if flags["service"] {
		runWindowsService()
		return
	}
	if install.ServiceInstalled() {
		runServiceGUI(dir, flags)
		return
	}
	runPortable(dir, args, flags)
}

// ---------- Служба ----------

type winService struct{}

func (winService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	app.ServiceMode, api.ServiceMode = true, true
	_ = install.PrepareServiceDataDir()
	b := startBackend(install.ServiceDataDir(), map[string]bool{})
	// «Выход» и vpnrouting quit у службы только выключают VPN: служба остаётся для окна и команд
	b.server.Quit = b.vpn.Stop
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				b.shutdown()
				return false, 0
			}
		case <-b.quitCh:
			// Перезапуск после самообновления: b.shutdown выходит с кодом ошибки, диспетчер служб
			// перезапускает службу уже новой программой
			status <- svc.Status{State: svc.StopPending}
			b.shutdown()
			return true, 1
		}
	}
}

func runWindowsService() {
	if ok, _ := svc.IsWindowsService(); !ok {
		fail("Флаг --service — для запуска службой Windows. Откройте «VPN routing» из меню «Пуск».")
	}
	if err := svc.Run(install.ServiceName, winService{}); err != nil {
		app.Logf("служба: %v", err)
		os.Exit(1)
	}
}

// ---------- Окно для службы ----------

// guiPipe — канал «показать окно» этого пользователя: второй запуск ярлыка показывает открытое окно.
func guiPipe() string { return `\\.\pipe\vpnrouting-gui-` + install.CurrentUserSID() }

func runServiceGUI(dir string, flags map[string]bool) {
	if c, err := winio.DialPipe(guiPipe(), nil); err == nil {
		_, _ = c.Write([]byte("show\n"))
		c.Close()
		return
	}
	c := client(dir)
	if _, err := c.State(); err != nil {
		go func() { _ = startService(dir) }()
	}
	var ui *gui.UI
	ui = gui.New(gui.Options{
		Client:       c,
		Tray:         true,
		Hidden:       flags["hidden"] || flags["autostart"],
		Service:      true,
		StartService: func() error { return startService(dir) },
		// «Выход»: выключить VPN и закрыть окно; служба остаётся (без VPN) для следующего запуска
		OnQuit: func() {
			_, _ = c.Do("/api/vpn/disconnect", nil)
			ui.Quit()
		},
		// Служба обновилась (новая версия программы) — перезапустить и окно новой программой
		OnServiceVersion: func(v string) {
			if v == version {
				return
			}
			if exe, err := os.Executable(); err == nil {
				_ = exec.Command(exe, "--hidden").Start()
			}
			ui.Quit()
		},
	})
	if ln, err := winio.ListenPipe(guiPipe(), &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + install.CurrentUserSID() + ")",
	}); err == nil {
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				conn.Close()
				ui.Show()
			}
		}()
	}
	ui.Run()
}

// startService — служба остановлена: запустить (пользователю это разрешено) и дождаться ответа.
func startService(dir string) error {
	if err := install.StartService(); err != nil {
		return err
	}
	if _, ok := waitRunning(dir, 20*time.Second); !ok {
		return errors.New("служба запущена, но не отвечает — подробности в журнале C:\\ProgramData\\VPN routing\\app.log")
	}
	return nil
}

// ---------- Переносная версия ----------

func runPortable(dir string, args []string, flags map[string]bool) {
	if c, ok := running(dir); ok {
		if flags["connect"] {
			_, _ = c.Do("/api/vpn/connect", nil)
		}
		if !flags["autostart"] && !flags["hidden"] {
			_, _ = c.Do("/api/show", nil)
		}
		return
	}
	// VPNROUTING_NOADMIN=1 — без прав администратора (проверка окна; подключение в этом режиме не работает)
	if !isPrivileged() && os.Getenv("VPNROUTING_NOADMIN") != "1" {
		if err := relaunchPrivileged(args); err != nil {
			fail("Для VPN нужны права администратора: " + err.Error())
		}
		return
	}
	b := startBackend(dir, flags)
	ui := gui.New(gui.Options{
		Client: api.Local(b.server.Handler()),
		Tray:   true,
		Hidden: flags["autostart"] || flags["hidden"],
		OnQuit: b.quit,
	})
	b.server.Show = ui.Show
	go func() {
		<-b.quitCh
		ui.Quit()
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		b.quit()
	}()
	ui.Run()
	b.quit()
	b.shutdown()
}

// startBackground — команда connect, а приложение (служба) не запущено.
func startBackground(dir string) (*api.Client, bool) {
	if install.ServiceInstalled() {
		if err := startService(dir); err != nil {
			return nil, false
		}
		return running(dir)
	}
	if err := startDetachedApp([]string{"--hidden"}); err != nil {
		return nil, false
	}
	return waitRunning(dir, 60*time.Second)
}

func startDetachedApp(args []string) error {
	if !isPrivileged() {
		return relaunchPrivileged(args)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return spawnDetached(exe, args)
}
