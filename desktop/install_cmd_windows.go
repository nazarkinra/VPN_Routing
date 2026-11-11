package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"vpnrouting/desktop/app"
	"vpnrouting/desktop/install"
)

// runInstall — «vpnrouting install» (поставить программу из этой папки, например из zip-архива) и
// «vpnrouting uninstall» (кнопка «Удалить» в «Параметры → Приложения»).
func runInstall(dir, command string, args []string) int {
	if !isPrivileged() {
		if err := relaunchPrivileged(append([]string{command}, args...)); err != nil {
			install.Error("Нужны права администратора: " + err.Error())
			return 1
		}
		return 0
	}
	silent, purge := false, false
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--silent", "/s":
			silent = true
		case "--purge":
			purge = true
		}
	}
	if command == "uninstall" {
		return uninstallWindows(dir, silent, purge)
	}
	exe, err := os.Executable()
	if err != nil {
		install.Error(err.Error())
		return 1
	}
	files := map[string][]byte{}
	for _, name := range []string{install.ExeName, "wintun.dll", "wintun-LICENSE.txt"} {
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), name)); err == nil {
			files[name] = b
		}
	}
	if _, ok := files["wintun.dll"]; !ok {
		install.Error("Рядом с программой нет wintun.dll: распакуйте архив целиком.")
		return 1
	}
	if err := install.Install(files, app.VersionName, true); err != nil {
		install.Error("Не удалось установить: " + err.Error())
		return 1
	}
	if install.Ask("VPN routing установлен в «" + install.Dir() + "».\nОн есть в меню «Пуск» и на рабочем столе.\n\nЗапустить сейчас?") {
		_ = install.LaunchAsUser(filepath.Join(install.Dir(), install.ExeName))
	}
	return 0
}

// uninstallWindows; silent — без вопросов (настройки удаляются только с --purge).
func uninstallWindows(dir string, silent, purge bool) int {
	if !silent && !install.Ask("Удалить VPN routing с компьютера? VPN будет отключён.") {
		return 0
	}
	if !silent {
		purge = install.Ask("Удалить и настройки — серверы, подписки, свои сайты, логин и пароль МГТУ?\n\n" +
			"«Нет» — оставить их на случай повторной установки.")
	}
	if c, ok := running(dir); ok {
		_, _ = c.Do("/api/quit", nil)
		for i := 0; i < 40; i++ {
			if _, ok := running(dir); !ok {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if err := install.Uninstall(dir, purge); err != nil {
		install.Error("Не всё удалилось: " + err.Error())
		return 1
	}
	if !silent {
		install.Info("VPN routing удалён.")
	}
	return 0
}
