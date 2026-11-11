package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Служба Windows «VPN routing» (как HappService у Happ): VPN работает в ней от имени системы (LocalSystem),
// а окно — обычная программа пользователя без прав администратора. Права нужны один раз — при установке.

const (
	ServiceName = "VPNrouting"
	runKey      = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	runValue    = "VPN routing"
	// Права на службу: система и администраторы — полностью; вошедшие в систему пользователи — запускать,
	// останавливать и смотреть состояние (окно запускает службу без запроса прав, если она остановлена)
	serviceSDDL = "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWRPWPLOCRRC;;;IU)"
	// Папка настроек службы: только система и администраторы (окно читает всё через канал службы)
	dataDirSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
)

// ServiceDataDir — папка настроек службы: %ProgramData%\VPN routing.
func ServiceDataDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, AppName)
}

// ServiceInstalled — служба зарегистрирована.
func ServiceInstalled() bool {
	m, err := mgr.Connect()
	if err != nil {
		return serviceExistsLimited()
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// serviceExistsLimited — проверка без прав администратора (mgr.Connect просит полный доступ).
func serviceExistsLimited() bool {
	h, err := openServiceLimited(windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	windows.CloseServiceHandle(h)
	return true
}

func openServiceLimited(access uint32) (windows.Handle, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, err
	}
	defer windows.CloseServiceHandle(scm)
	name, _ := windows.UTF16PtrFromString(ServiceName)
	return windows.OpenService(scm, name, access)
}

// StartService запускает службу (вошедшему пользователю это разрешено правами службы).
func StartService() error {
	h, err := openServiceLimited(windows.SERVICE_START | windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	if err := windows.StartService(h, 0, nil); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return nil
}

// InstallService регистрирует службу (или обновляет её настройки) и запускает её.
func InstallService(exe string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	cfg := mgr.Config{
		DisplayName:  AppName,
		Description:  "VPN со сплит-маршрутизацией (Xray) и VPN МГТУ. Окно — «VPN routing» в меню «Пуск».",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}
	s, err := m.OpenService(ServiceName)
	if err == nil {
		old, err := s.Config()
		if err == nil {
			cfg.StartType = old.StartType // выбор «запускать вместе с Windows» сохраняется
		}
		cfg.BinaryPathName = `"` + exe + `" --service`
		if err := s.UpdateConfig(cfg); err != nil {
			s.Close()
			return err
		}
	} else {
		s, err = m.CreateService(ServiceName, exe, cfg, "--service")
		if err != nil {
			return err
		}
	}
	defer s.Close()
	// Сбой или выход после самообновления — перезапуск через 3 секунды
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if sd, err := windows.SecurityDescriptorFromString(serviceSDDL); err == nil {
		if dacl, _, err := sd.DACL(); err == nil {
			_ = windows.SetSecurityInfo(windows.Handle(s.Handle), windows.SE_SERVICE,
				windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
		}
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return nil
}

// StopService останавливает службу и ждёт (не дольше timeout).
func StopService(timeout time.Duration) {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return
	}
	defer s.Close()
	st, err := s.Control(svc.Stop)
	deadline := time.Now().Add(timeout)
	for err == nil && st.State != svc.Stopped && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		st, err = s.Query()
	}
}

// DeleteService останавливает и удаляет службу.
func DeleteService() {
	StopService(15 * time.Second)
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	if s, err := m.OpenService(ServiceName); err == nil {
		_ = s.Delete()
		s.Close()
	}
}

// ServiceAutoStart — служба запускается вместе с Windows.
func ServiceAutoStart() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return false
	}
	defer s.Close()
	cfg, err := s.Config()
	return err == nil && cfg.StartType == mgr.StartAutomatic
}

// SetServiceAutoStart — запускать службу вместе с Windows (и значок в трее при входе) или по требованию.
func SetServiceAutoStart(on bool, exe string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	cfg.StartType = mgr.StartManual
	if on {
		cfg.StartType = mgr.StartAutomatic
	}
	if err := s.UpdateConfig(cfg); err != nil {
		return err
	}
	return SetTrayAtLogon(on, exe)
}

// SetTrayAtLogon — значок в трее при входе любого пользователя (HKLM\…\Run).
func SetTrayAtLogon(on bool, exe string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		err := k.DeleteValue(runValue)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return k.SetStringValue(runValue, `"`+exe+`" --hidden`)
}

// PrepareServiceDataDir создаёт папку настроек службы с доступом только для системы и администраторов.
func PrepareServiceDataDir() error {
	dir := ServiceDataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(dataDirSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// ---------- Пользователи окна ----------

// CurrentUserSID — SID пользователя этого процесса (у установщика с правами администратора — тот же).
func CurrentUserSID() string {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return ""
	}
	return tu.User.Sid.String()
}

func uiUsersFile() string { return filepath.Join(ServiceDataDir(), "ui-users") }

// UIUserSIDs — пользователи, которым служба открывает свой канал (окно и команды vpnrouting).
func UIUserSIDs() []string {
	b, err := os.ReadFile(uiUsersFile())
	if err != nil {
		return nil
	}
	var r []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "S-1-") {
			r = append(r, l)
		}
	}
	return r
}

// AddUIUserSID добавляет пользователя в список.
func AddUIUserSID(sid string) error {
	if sid == "" {
		return nil
	}
	users := UIUserSIDs()
	for _, u := range users {
		if u == sid {
			return nil
		}
	}
	return os.WriteFile(uiUsersFile(), []byte(strings.Join(append(users, sid), "\n")+"\n"), 0o600)
}

// ---------- Перенос настроек ----------

// MigrateUserSettings переносит настройки прежних версий (%APPDATA%\VPN routing\settings.json) в папку
// службы, если там ещё пусто. Пароль МГТУ был зашифрован ключом пользователя — служба его не прочитала бы,
// поэтому он перешифровывается ключом компьютера (папка службы доступна только системе и администраторам).
func MigrateUserSettings(userDir string) error {
	return migrateSettingsFile(filepath.Join(userDir, "settings.json"), filepath.Join(ServiceDataDir(), "settings.json"))
}

func migrateSettingsFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if bm, ok := m["bmstu"].(map[string]any); ok {
		if pass, ok := bm["pass"].(string); ok && strings.HasPrefix(pass, "dpapi:") {
			if plain, err := dpapiDecrypt(pass[len("dpapi:"):]); err == nil {
				if enc, err := dpapiEncryptMachine(plain); err == nil {
					bm["pass"] = "dpapi:" + enc
				}
			} else {
				bm["pass"] = "" // не расшифровался — логин и пароль МГТУ придётся ввести заново
			}
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dst, out, 0o600)
}

func dpapiDecrypt(b64 string) ([]byte, error) {
	enc, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(enc) == 0 {
		return nil, errors.New("пустые данные")
	}
	in := windows.DataBlob{Size: uint32(len(enc)), Data: &enc[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}

func dpapiEncryptMachine(plain []byte) (string, error) {
	if len(plain) == 0 {
		return "", errors.New("пустой пароль")
	}
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN|windows.CRYPTPROTECT_LOCAL_MACHINE, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

// LaunchAsUser запускает программу от имени вошедшего пользователя без прав администратора (из
// установщика с правами администратора): через explorer.exe, который работает без повышения.
func LaunchAsUser(exe string) error {
	return exec.Command(filepath.Join(os.Getenv("SystemRoot"), "explorer.exe"), exe).Start()
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
