package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/gui"
	"vpnrouting/desktop/install"
)

// launch — Linux. От root (служба systemd или sudo) работает VPN без окна; от пользователя открывается
// окно, которое управляет службой через сокет /run/vpnrouting/control.sock.
func launch(dir string, _ []string, flags map[string]bool) {
	if os.Geteuid() == 0 {
		runService(dir, flags)
		return
	}
	// Одно окно на пользователя: второй запуск ярлыка показывает уже открытое
	lock := fmt.Sprintf("@vpnrouting-gui-%d", os.Getuid())
	if c, err := net.Dial("unix", lock); err == nil {
		_, _ = c.Write([]byte("show\n"))
		c.Close()
		return
	}
	// Масштаб окна — как у остальных программ (системная настройка, например 125%), а не по DPI экрана
	applyDesktopScale()
	hidden := flags["hidden"] || flags["autostart"]
	c := client(dir)
	// При входе в систему (--hidden) служба не запускается: systemctl start спросил бы пароль
	if _, err := c.State(); err != nil && install.ServiceInstalled() && !hidden {
		go func() { _ = startService() }()
	}
	var start func() error
	if install.ServiceInstalled() {
		start = startService
	}
	var ui *gui.UI
	ui = gui.New(gui.Options{
		Client:       c,
		StartService: start,
		// Значок в трее (GNOME — через расширение AppIndicator, в Ubuntu оно включено); закрытие окна
		// прячет его в трей, VPN работает в службе и без окна
		Tray:    true,
		Hidden:  hidden,
		Service: true,
		// «Выход» в трее: выключить VPN и закрыть окно; служба остаётся для следующего запуска
		OnQuit: func() {
			_, _ = c.Do("/api/vpn/disconnect", nil)
			ui.Quit()
		},
		// Значок в трее при входе в систему — вместе с автозапуском службы
		OnState: func(st *api.State) { syncTrayAutostart(st.Autostart.Enabled) },
	})
	if ln, err := net.Listen("unix", lock); err == nil {
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

// runService — VPN без окна (служба). Выход — по сигналу или команде vpnrouting quit.
func runService(dir string, flags map[string]bool) {
	if _, ok := running(dir); ok {
		fmt.Println("VPN routing уже работает. Окно: «VPN routing» в меню приложений или команда vpnrouting (без sudo).")
		return
	}
	b := startBackend(dir, flags)
	if install.ServiceInstalled() {
		go install.RefreshDesktopEntry()
	}
	if os.Getenv("INVOCATION_ID") == "" {
		fmt.Println("VPN routing работает. Окно управления: «VPN routing» в меню приложений или команда vpnrouting " +
			"(без sudo, от вашего пользователя). Выход — Ctrl+C.")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-b.quitCh:
	}
	b.shutdown()
}

var trayAutostartDone *bool

// syncTrayAutostart — ~/.config/autostart/vpnrouting.desktop: при входе в систему окно запускается
// скрытым, со значком в трее. Есть, только пока включён автозапуск службы.
func syncTrayAutostart(enabled bool) {
	if trayAutostartDone != nil && *trayAutostartDone == enabled {
		return
	}
	trayAutostartDone = &enabled
	dir, err := os.UserConfigDir()
	if err != nil || !install.Installed() {
		return
	}
	path := filepath.Join(dir, "autostart", "vpnrouting.desktop")
	if !enabled {
		_ = os.Remove(path)
		return
	}
	entry := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=VPN routing",
		"Comment=Значок VPN routing в трее",
		"Exec=" + install.BinPath + " --hidden",
		"Icon=vpnrouting",
		"Terminal=false",
		"NoDisplay=true",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
	if old, err := os.ReadFile(path); err == nil && string(old) == entry {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(entry), 0o644)
}

// startService — systemctl start (система может спросить пароль) и ожидание ответа службы.
func startService() error {
	if !install.ServiceInstalled() {
		return errors.New("служба не установлена: sudo ./install.sh")
	}
	out, err := exec.Command("systemctl", "start", install.UnitName).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	if _, ok := waitRunning(dataDir(), 20*time.Second); !ok {
		return errors.New("служба запущена, но не отвечает — подробности: journalctl -u vpnrouting")
	}
	return nil
}

// startBackground — команда connect, а служба не запущена.
func startBackground(dir string) (*api.Client, bool) {
	if os.Geteuid() == 0 || !install.ServiceInstalled() {
		fmt.Fprintln(os.Stderr, "VPN routing не запущен: sudo systemctl start vpnrouting (или sudo vpnrouting)")
		return nil, false
	}
	if err := startService(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, false
	}
	return waitRunning(dir, 5*time.Second)
}
