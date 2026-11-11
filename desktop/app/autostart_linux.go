package app

import (
	"os"
	"os/exec"

	"vpnrouting/desktop/install"
)

// Автозапуск на Linux — служба systemd (как bmstu-vpn и xray на Raspberry Pi).

// ServiceMode — служба Windows; на Linux не используется (служба — systemd, см. launch_linux.go).
var ServiceMode bool

const unitPath = "/etc/systemd/system/vpnrouting.service"

func AutostartSupported() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func AutostartEnabled() bool {
	return exec.Command("systemctl", "is-enabled", "--quiet", "vpnrouting.service").Run() == nil
}

func SetAutostart(on bool) error {
	if !on {
		_ = exec.Command("systemctl", "disable", "vpnrouting.service").Run()
		// У установленной программы служба остаётся: ярлык в меню запускает её по требованию
		if install.Installed() {
			return nil
		}
		if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return exec.Command("systemctl", "daemon-reload").Run()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	unit := install.Unit(exe)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	// Служба включается на следующую загрузку: сейчас уже работает этот экземпляр
	return exec.Command("systemctl", "enable", "vpnrouting.service").Run()
}
