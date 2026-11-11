package main

import (
	"fmt"
	"os"

	"vpnrouting/desktop/install"
)

// runInstall — «sudo vpnrouting install [--user ИМЯ]» и «sudo vpnrouting uninstall [--purge]»
// (их вызывают install.sh и uninstall.sh из архива).
func runInstall(dir, command string, args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Нужны права root: sudo vpnrouting", command)
		return 1
	}
	if command == "uninstall" {
		purge := false
		for _, a := range args {
			if a == "--purge" {
				purge = true
			}
		}
		if c, ok := running(dir); ok {
			_, _ = c.Do("/api/quit", nil)
		}
		if err := install.Uninstall(dir, purge); err != nil {
			fmt.Fprintln(os.Stderr, "Ошибка:", err)
			return 1
		}
		fmt.Println("VPN routing удалён.")
		if !purge {
			fmt.Println("Настройки оставлены в", dir, "(удалить: sudo rm -r", dir+")")
		}
		return 0
	}
	uiUser := os.Getenv("SUDO_USER")
	for i, a := range args {
		if a == "--user" && i+1 < len(args) {
			uiUser = args[i+1]
		}
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := install.Install(exe, uiUser); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		return 1
	}
	fmt.Println("VPN routing установлен:", install.BinPath)
	fmt.Println("Служба", install.UnitName, "запущена и включена при загрузке.")
	fmt.Println("Ярлык «VPN routing» — в меню приложений (раздел «Сеть»).")
	if uiUser != "" && uiUser != "root" {
		fmt.Println("Окно и команды vpnrouting connect / disconnect доступны пользователю", uiUser, "без sudo.")
	}
	return 0
}
