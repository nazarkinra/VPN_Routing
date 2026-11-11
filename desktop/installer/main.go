//go:build windows

// Установщик для Windows: VPN_routing-setup.exe. Внутри — vpnrouting.exe и wintun.dll (папка payload,
// её заполняет build.ps1). Ставит программу в «Program Files\VPN routing», регистрирует службу «VPN routing»
// (VPN работает в ней, окну права администратора не нужны), создаёт ярлыки в меню «Пуск» и на рабочем столе
// и запись в «Параметры → Приложения» (оттуда же удаление).
//
//	VPN_routing-setup.exe        установка с вопросами
//	VPN_routing-setup.exe /S     тихая установка (без вопросов и без запуска)
package main

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vpnrouting/desktop/buildinfo"
	"vpnrouting/desktop/install"
)

//go:embed payload
var payload embed.FS

func main() {
	silent := false
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, "/S") || a == "--silent" {
			silent = true
		}
	}
	files := map[string][]byte{}
	for _, name := range []string{install.ExeName, "wintun.dll", "wintun-LICENSE.txt"} {
		// build.ps1 кладёт файлы сжатыми (.gz): установщик вдвое меньше
		if b, err := payload.ReadFile("payload/" + name + ".gz"); err == nil {
			if zr, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
				if data, err := io.ReadAll(zr); err == nil {
					files[name] = data
				}
			}
		}
	}
	if len(files[install.ExeName]) == 0 || len(files["wintun.dll"]) == 0 {
		fail("Установщик собран без программы: запустите desktop\\build.ps1.", silent)
	}

	if !silent {
		q := "Установить VPN routing " + buildinfo.VersionName + "?\n\nПапка: " + install.Dir() +
			"\nЯрлыки появятся в меню «Пуск» и на рабочем столе."
		if old := install.InstalledVersion(); old != "" {
			q = "Обновить VPN routing " + old + " до версии " + buildinfo.VersionName + "?\n\n" +
				"Работающий VPN ненадолго отключится. Настройки сохранятся."
		}
		if !install.Ask(q) {
			return
		}
	}
	if err := install.Install(files, buildinfo.VersionName, true); err != nil {
		fail("Не удалось установить: "+err.Error(), silent)
	}
	if silent {
		return
	}
	if install.Ask("VPN routing установлен.\nОн есть в меню «Пуск» и на рабочем столе, удалить можно в «Параметры → Приложения».\n\nЗапустить сейчас?") {
		// Окно — от имени пользователя, без прав администратора (VPN работает в службе)
		_ = install.LaunchAsUser(filepath.Join(install.Dir(), install.ExeName))
	}
}

func fail(msg string, silent bool) {
	if !silent {
		install.Error(msg)
	}
	os.Exit(1)
}
