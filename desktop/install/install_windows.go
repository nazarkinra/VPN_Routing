// Package install — установка приложения в систему и удаление.
//
// Windows: программа в «Program Files\VPN routing», ярлыки в меню «Пуск» и на рабочем столе, запись в
// «Параметры → Приложения» (там же кнопка «Удалить»).
// Linux: программа в /usr/local/bin, ярлык и значок в меню приложений, служба systemd.
package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	AppName      = "VPN routing"
	ExeName      = "vpnrouting.exe"
	uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\VPNrouting`
	taskName     = "VPN routing"
)

// Dir — папка установки.
func Dir() string {
	pf := os.Getenv("ProgramW6432")
	if pf == "" {
		pf = os.Getenv("ProgramFiles")
	}
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, AppName)
}

func startMenuLink() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, `Microsoft\Windows\Start Menu\Programs`, AppName+".lnk")
}

func desktopLink() string {
	pub := os.Getenv("PUBLIC")
	if pub == "" {
		pub = `C:\Users\Public`
	}
	return filepath.Join(pub, "Desktop", AppName+".lnk")
}

// InstalledVersion — версия из записи «Приложения» или "" (не установлено).
func InstalledVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("DisplayVersion")
	return v
}

// IsInstalledExe — программа запущена из папки установки.
func IsInstalledExe() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Dir(exe), Dir())
}

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c
}

// stopRunning закрывает запущенную установленную программу (VPN отключается) и ждёт её выхода.
func stopRunning() {
	// Служба (VPN) — через диспетчер служб; окна пользователей и программа прежних версий без службы —
	// командой quit, а если не закрылись — принудительно
	StopService(20 * time.Second)
	exe := filepath.Join(Dir(), ExeName)
	if _, err := os.Stat(exe); err != nil {
		return
	}
	_ = hidden(exe, "quit").Run()
	for i := 0; i < 40; i++ {
		if len(installedProcesses()) == 0 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	// Не закрылась по команде (зависла, другая версия) — завершить принудительно, иначе новая программа
	// не сможет занять сокет управления и файлы
	for _, pid := range installedProcesses() {
		if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid); err == nil {
			_ = windows.TerminateProcess(h, 1)
			_, _ = windows.WaitForSingleObject(h, 5000)
			windows.CloseHandle(h)
		}
	}
}

// installedProcesses — процессы, запущенные из папки установки.
func installedProcesses() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var pids []uint32
	self := uint32(os.Getpid())
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if pe.ProcessID == self || !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), ExeName) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pe.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil &&
			strings.EqualFold(filepath.Dir(windows.UTF16ToString(buf[:n])), Dir()) {
			pids = append(pids, pe.ProcessID)
		}
		windows.CloseHandle(h)
	}
	return pids
}

// Install ставит файлы (имя → содержимое) в папку установки, создаёт ярлыки и запись «Приложения».
func Install(files map[string][]byte, version string, desktopShortcut bool) error {
	if _, ok := files[ExeName]; !ok {
		return errors.New("в установщике нет " + ExeName)
	}
	stopRunning()
	if d := UserDataDir(); d != "" {
		CleanupLegacySockets(d)
	}
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var size int64
	for name, data := range files {
		size += int64(len(data))
		if err := writeReplacing(filepath.Join(dir, name), data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	exe := filepath.Join(dir, ExeName)
	if err := shortcut(startMenuLink(), exe); err != nil {
		return fmt.Errorf("ярлык в меню «Пуск»: %w", err)
	}
	if desktopShortcut {
		_ = shortcut(desktopLink(), exe)
	}
	if err := register(dir, exe, version, size); err != nil {
		return fmt.Errorf("запись в «Приложения»: %w", err)
	}

	// Служба: папка настроек, перенос настроек прежних версий, доступ этому пользователю, регистрация
	_ = hidden("schtasks", "/Delete", "/TN", taskName, "/F").Run() // автозапуск прежних версий
	if err := PrepareServiceDataDir(); err != nil {
		return fmt.Errorf("папка настроек службы: %w", err)
	}
	if d := UserDataDir(); d != "" {
		if err := MigrateUserSettings(d); err != nil {
			return fmt.Errorf("перенос настроек: %w", err)
		}
	}
	if err := AddUIUserSID(CurrentUserSID()); err != nil {
		return err
	}
	if err := InstallService(exe); err != nil {
		return fmt.Errorf("служба: %w", err)
	}
	// Значок в трее при входе — если служба запускается вместе с Windows
	_ = SetTrayAtLogon(ServiceAutoStart(), exe)
	return nil
}

// writeReplacing пишет файл; занятый файл (запущенная программа) сначала переименовывается в .old.
func writeReplacing(path string, data []byte) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	_ = os.Remove(path + ".old")
	if err := os.Rename(path, path+".old"); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func shortcut(link, target string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	ps := `$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:VR_LINK); ` +
		`$s.TargetPath = $env:VR_TARGET; $s.WorkingDirectory = $env:VR_DIR; $s.IconLocation = $env:VR_TARGET + ',0'; ` +
		`$s.Description = 'VPN со сплит-маршрутизацией и VPN МГТУ'; $s.Save()`
	c := hidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps)
	c.Env = append(os.Environ(), "VR_LINK="+link, "VR_TARGET="+target, "VR_DIR="+filepath.Dir(target))
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func register(dir, exe, version string, size int64) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range map[string]string{
		"DisplayName":     AppName,
		"DisplayVersion":  version,
		"Publisher":       "VPN routing",
		"DisplayIcon":     exe + ",0",
		"InstallLocation": dir,
		"UninstallString": `"` + exe + `" uninstall`,
		"URLInfoAbout":    "https://github.com/nazarkinra/VPN_Routing",
		"InstallDate":     time.Now().Format("20060102"),
	} {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return k.SetDWordValue("EstimatedSize", uint32(size/1024))
}

// SetInstalledVersion обновляет версию в «Приложениях» (после самообновления установленной программы).
func SetInstalledVersion(version string) {
	if !IsInstalledExe() {
		return
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallKey, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DisplayVersion", version)
		k.Close()
	}
}

// Uninstall удаляет ярлыки, запись «Приложения», задачу автозапуска и папку программы.
// dataDir — папка настроек; удаляется, если purge.
func Uninstall(dataDir string, purge bool) error {
	DeleteService()
	_ = SetTrayAtLogon(false, "")
	// Окна других пользователей и прежние версии
	for _, pid := range installedProcesses() {
		if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid); err == nil {
			_ = windows.TerminateProcess(h, 0)
			_, _ = windows.WaitForSingleObject(h, 5000)
			windows.CloseHandle(h)
		}
	}
	_ = hidden("schtasks", "/Delete", "/TN", taskName, "/F").Run()
	_ = os.Remove(startMenuLink())
	_ = os.Remove(desktopLink())
	_ = registry.DeleteKey(registry.LOCAL_MACHINE, uninstallKey)
	if purge {
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
		_ = os.RemoveAll(ServiceDataDir())
	}
	dir := Dir()
	// Работающую программу удалить нельзя: папку удаляет cmd через пару секунд после выхода
	if IsInstalledExe() {
		// Командная строка передаётся как есть: обычное экранирование Go (\") cmd.exe не понимает
		c := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
		c.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
			CmdLine:       `cmd.exe /C ping 127.0.0.1 -n 4 >nul & rmdir /S /Q "` + dir + `"`,
		}
		// Не из папки программы: текущую папку процесса cmd удалить не сможет
		c.Dir = os.TempDir()
		return c.Start()
	}
	return os.RemoveAll(dir)
}

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procMessageBox = user32.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x0
	mbYesNo         = 0x4
	mbIconError     = 0x10
	mbIconQuest     = 0x20
	mbIconInfo      = 0x40
	mbSetForeground = 0x10000
	idYes           = 6
)

func messageBox(text string, flags uintptr) int {
	t, _ := windows.UTF16PtrFromString(AppName)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags|mbSetForeground)
	return int(r)
}

// Ask — вопрос «Да / Нет».
func Ask(text string) bool { return messageBox(text, mbYesNo|mbIconQuest) == idYes }

// Info — сообщение.
func Info(text string) { messageBox(text, mbOK|mbIconInfo) }

// Error — сообщение об ошибке.
func Error(text string) { messageBox(text, mbOK|mbIconError) }

// CleanupLegacySockets удаляет сокет-файлы прежних сборок (control*.sock в папке настроек): программа
// с правами администратора создавала их с высоким уровнем целостности, и обычный процесс не может их ни
// открыть, ни удалить. Удаляет процесс с правами администратора — установщик или приложение при запуске.
func CleanupLegacySockets(dataDir string) {
	matches, _ := filepath.Glob(filepath.Join(dataDir, "control*.sock"))
	for _, m := range matches {
		if os.Remove(m) == nil {
			continue
		}
		// Сокет-файл — точка повторной обработки (reparse point): удаляем её саму, не открывая сокет
		p, err := windows.UTF16PtrFromString(m)
		if err != nil {
			continue
		}
		h, err := windows.CreateFile(p, windows.DELETE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
		if err == nil {
			windows.CloseHandle(h)
		}
	}
}

// UserDataDir — папка настроек приложения этого пользователя (%APPDATA%\VPN routing).
func UserDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, AppName)
}
