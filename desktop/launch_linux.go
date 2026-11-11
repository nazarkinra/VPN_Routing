package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
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
	c := client(dir)
	if _, err := c.State(); err != nil && install.ServiceInstalled() {
		go func() { _ = startService() }()
	}
	var start func() error
	if install.ServiceInstalled() {
		start = startService
	}
	ui := gui.New(gui.Options{Client: c, StartService: start})
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
