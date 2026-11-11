// VPN routing для Windows и Linux: то же, что Android-приложение из ../android.
//
//	vpnrouting                 открыть приложение
//	vpnrouting connect         включить VPN (запустит приложение, если оно не запущено)
//	vpnrouting disconnect      выключить VPN
//	vpnrouting toggle          переключить
//	vpnrouting status          состояние
//	vpnrouting open            открыть окно
//	vpnrouting quit            выключить VPN и закрыть приложение
//	vpnrouting install         установить в систему
//	vpnrouting uninstall       удалить из системы
//
// Флаги запуска: --connect (подключиться сразу), --hidden (без окна, значок в трее),
// --autostart (запуск при входе в систему: подключение по настройке, окно не открывается).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/app"
)

// version — версия ПК-версии (app.VersionName, по ней же проверяются обновления).
var version = app.VersionName

func main() {
	args := os.Args[1:]
	var command string
	flags := map[string]bool{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags[strings.TrimLeft(a, "-")] = true
		} else if command == "" {
			command = a
		}
	}
	// Прежнее название флага
	if flags["no-browser"] {
		flags["hidden"] = true
	}
	if flags["h"] || flags["help"] || command == "help" {
		fmt.Println(usage)
		return
	}
	dir := dataDir()
	if command != "" && command != "open" {
		os.Exit(runCommand(dir, command, args))
	}
	launch(dir, args, flags)
}

var usage = `VPN routing ` + version + `

  vpnrouting                 открыть приложение
  vpnrouting connect         включить VPN
  vpnrouting disconnect      выключить VPN
  vpnrouting toggle          переключить VPN
  vpnrouting status          состояние VPN
  vpnrouting open            открыть окно
  vpnrouting quit            выключить VPN и закрыть приложение
  vpnrouting install         установить в систему (меню «Пуск» / меню приложений)
  vpnrouting uninstall       удалить из системы (--purge — вместе с настройками)

Флаги: --connect, --hidden, --autostart`

func dataDir() string {
	if d := os.Getenv("VPNROUTING_DATA"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = os.TempDir()
		}
		return filepath.Join(base, "VPN routing")
	}
	if os.Geteuid() == 0 {
		return "/var/lib/vpnrouting"
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "vpnrouting")
}

// client — команды запущенному приложению (службе) через сокет управления.
func client(dir string) *api.Client {
	return api.Dial(func() []string { return api.SocketCandidates(dir) })
}

// running — запущенное приложение отвечает.
func running(dir string) (*api.Client, bool) {
	c := client(dir)
	_, err := c.State()
	return c, err == nil
}

// runCommand — команды для сценариев (аналог интентов CommandActivity на Android).
func runCommand(dir, command string, args []string) int {
	if command == "install" || command == "uninstall" {
		return runInstall(dir, command, args)
	}
	known := map[string]bool{"connect": true, "disconnect": true, "toggle": true, "status": true, "quit": true}
	if !known[command] {
		fmt.Fprintln(os.Stderr, "Неизвестная команда:", command)
		fmt.Println(usage)
		return 2
	}
	c, ok := running(dir)
	if !ok {
		switch command {
		case "connect", "toggle":
			// Приложение не запущено — запустить его с подключением
			var started bool
			c, started = startBackground(dir)
			if !started {
				return 1
			}
			command = "connect"
		case "status":
			fmt.Println("stopped: приложение не запущено")
			return 0
		default:
			return 0
		}
	}
	switch command {
	case "connect", "disconnect", "toggle":
		if _, err := c.Do("/api/vpn/"+command, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "status":
		st, err := c.State()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("%s: %s\n", st.VPN.State, st.VPN.Message)
	case "quit":
		_, _ = c.Do("/api/quit", nil)
	}
	return 0
}

// waitRunning ждёт, пока приложение (служба) начнёт отвечать.
func waitRunning(dir string, max time.Duration) (*api.Client, bool) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if c, ok := running(dir); ok {
			return c, true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil, false
}

func fail(msg string) {
	app.Logf("%s", msg)
	// У службы нет рабочего стола: сообщение некому показать, только журнал
	if !app.ServiceMode {
		showError(msg)
	}
	os.Exit(1)
}
