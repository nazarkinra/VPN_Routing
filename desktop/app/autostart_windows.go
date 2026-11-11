package app

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"vpnrouting/desktop/install"
)

// ServiceMode — программа работает службой Windows «VPN routing» (LocalSystem).
var ServiceMode bool

// Автозапуск:
//   - служба (установленная программа): служба запускается вместе с Windows, при входе — значок в трее;
//   - без службы (переносная версия из zip): задача планировщика с наивысшими правами (без запроса UAC).
// Аналог системной настройки «Постоянная VPN» на Android.

const taskName = "VPN routing"

func hiddenCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c
}

// AutostartSupported — можно ли включить автозапуск.
func AutostartSupported() bool { return true }

// AutostartEnabled — автозапуск включён.
func AutostartEnabled() bool {
	if ServiceMode {
		return install.ServiceAutoStart()
	}
	return hiddenCmd("schtasks", "/Query", "/TN", taskName).Run() == nil
}

// SetAutostart включает или выключает автозапуск.
func SetAutostart(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if ServiceMode {
		return install.SetServiceAutoStart(on, exe)
	}
	if !on {
		if !AutostartEnabled() {
			return nil
		}
		return runQuiet(hiddenCmd("schtasks", "/Delete", "/TN", taskName, "/F"))
	}
	tr := `"` + exe + `" --autostart`
	return runQuiet(hiddenCmd("schtasks", "/Create", "/TN", taskName, "/TR", tr, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F"))
}

func runQuiet(c *exec.Cmd) error {
	out, err := c.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			logf("%s: %s", c.Path, msg)
		}
	}
	return err
}
