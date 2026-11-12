// Package install — установка приложения в систему и удаление (Linux).
//
// Программа — /usr/local/bin/vpnrouting, ярлык «VPN routing» в меню приложений (раздел «Сеть»),
// значок, служба systemd vpnrouting.service (запущена и включена при загрузке, как bmstu-vpn на Pi).
// Ярлык запускает «vpnrouting open» от имени пользователя: он открывает окно работающей службы.
package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"vpnrouting/desktop/assets"
)

const (
	AppName     = "VPN routing"
	BinPath     = "/usr/local/bin/vpnrouting"
	UnitName    = "vpnrouting.service"
	unitPath    = "/etc/systemd/system/vpnrouting.service"
	desktopPath = "/usr/share/applications/vpnrouting.desktop"
	iconPNG     = "/usr/share/icons/hicolor/512x512/apps/vpnrouting.png"
	// iconPixmap — для окружений, которые не смотрят в hicolor
	iconPixmap = "/usr/share/pixmaps/vpnrouting.png"
	confDir    = "/etc/vpnrouting"
	// UsersFile — пользователи, которым служба кладёт ключ доступа к окну (~/.config/vpnrouting/control.json).
	UsersFile = "/etc/vpnrouting/ui-users"
)

// Unit — служба systemd: запуск без окна, перезапуск при сбое и после самообновления.
func Unit(exe string) string {
	return strings.Join([]string{
		"[Unit]",
		"Description=VPN routing (Xray split routing + BMSTU OpenVPN)",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"ExecStart=" + exe + " --autostart --no-browser",
		"Restart=on-failure",
		"RestartSec=3",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")
}

var desktopEntry = strings.Join([]string{
	"[Desktop Entry]",
	"Type=Application",
	"Name=VPN routing",
	"GenericName=VPN",
	"Comment=VPN со сплит-маршрутизацией и VPN МГТУ",
	"Exec=" + BinPath,
	"Icon=vpnrouting",
	"Terminal=false",
	"Categories=Network;",
	"Keywords=VPN;VLESS;Xray;Proxy;МГТУ;",
	"StartupNotify=false",
	// WM class (X11) / app_id (Wayland) окна Fyne — его заголовок. Без этой строки GNOME не связывает
	// окно с ярлыком и показывает в панели задач шестерёнку вместо значка.
	"StartupWMClass=" + WindowTitle,
	"",
}, "\n")

// WindowTitle — заголовок окна (gui), он же WM class окна.
const WindowTitle = "VPN routing"

// RefreshDesktopEntry обновляет ярлык установленной программы, если он устарел: самообновление
// заменяет только программу, а ярлык остаётся от прежней версии. Вызывается службой (root).
func RefreshDesktopEntry() {
	old, err := os.ReadFile(desktopPath)
	if err != nil || string(old) == desktopEntry {
		return
	}
	if err := os.WriteFile(desktopPath, []byte(desktopEntry), 0o644); err != nil {
		return
	}
	_ = exec.Command("update-desktop-database", "-q", "/usr/share/applications").Run()
}

// Installed — программа установлена.
func Installed() bool {
	_, err := os.Stat(BinPath)
	return err == nil
}

// ServiceInstalled — есть служба systemd.
func ServiceInstalled() bool {
	_, err := os.Stat(unitPath)
	return err == nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install копирует программу self в /usr/local/bin, создаёт ярлык и службу, запускает её.
// uiUser — пользователь, которому открывать окно из меню (обычно $SUDO_USER).
func Install(self, uiUser string) error {
	if os.Geteuid() != 0 {
		return errors.New("установка нужна от root: sudo ./install.sh")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("нужен systemd (команда systemctl не найдена)")
	}
	// Остановить прежнюю версию: службу и запущенный вручную экземпляр
	_ = run("systemctl", "stop", UnitName)
	_ = exec.Command(self, "quit").Run()
	if Installed() {
		_ = exec.Command(BinPath, "quit").Run()
	}
	time.Sleep(time.Second)

	if err := copyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("программа: %w", err)
	}
	for path, data := range map[string][]byte{iconPNG: assets.IconPNG, iconPixmap: assets.IconPNG, desktopPath: []byte(desktopEntry)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	_ = exec.Command("gtk-update-icon-cache", "-q", "-t", "/usr/share/icons/hicolor").Run()
	_ = exec.Command("update-desktop-database", "-q", "/usr/share/applications").Run()

	if uiUser != "" && uiUser != "root" {
		if err := AddUIUser(uiUser); err != nil {
			return err
		}
	}
	if err := os.WriteFile(unitPath, []byte(Unit(BinPath)), 0o644); err != nil {
		return err
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "enable", "--now", UnitName)
}

// AddUIUser разрешает пользователю открывать окно и управлять VPN без sudo (ярлык в меню, команды).
func AddUIUser(name string) error {
	if _, err := user.Lookup(name); err != nil {
		return fmt.Errorf("пользователь %s не найден", name)
	}
	users := UIUsers()
	for _, u := range users {
		if u == name {
			return nil
		}
	}
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(UsersFile, []byte(strings.Join(append(users, name), "\n")+"\n"), 0o644)
}

// UIUsers — пользователи из /etc/vpnrouting/ui-users.
func UIUsers() []string {
	f, err := os.Open(UsersFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	var r []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" && !strings.HasPrefix(s, "#") {
			r = append(r, s)
		}
	}
	return r
}

// Uninstall удаляет службу, ярлык, значок и программу; purge — и настройки (/var/lib/vpnrouting).
func Uninstall(dataDir string, purge bool) error {
	if os.Geteuid() != 0 {
		return errors.New("удаление нужно от root: sudo vpnrouting uninstall")
	}
	_ = run("systemctl", "disable", "--now", UnitName)
	for _, p := range []string{unitPath, desktopPath, iconPNG, iconPixmap, BinPath, BinPath + ".old"} {
		_ = os.Remove(p)
	}
	_ = os.RemoveAll(confDir)
	_ = run("systemctl", "daemon-reload")
	_ = exec.Command("update-desktop-database", "-q", "/usr/share/applications").Run()
	if purge && dataDir != "" {
		return os.RemoveAll(dataDir)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if a, err := filepath.EvalSymlinks(src); err == nil {
		if b, err := filepath.EvalSymlinks(dst); err == nil && a == b {
			return nil // уже установленная программа
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// SetInstalledVersion — на Linux версия нигде не записана.
func SetInstalledVersion(string) {}
