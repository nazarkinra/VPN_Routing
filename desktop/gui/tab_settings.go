package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/app"
)

// Вкладка «Настройки» (то, что на Android делает система: автозапуск, «Постоянная VPN»), обновления,
// журнал и диагностика.
type settingsTab struct {
	u    *UI
	root fyne.CanvasObject

	binding     bool
	autostart   *widget.Check
	autoHint    *widget.Label
	connect     *widget.Check
	dnsLeak     *widget.Check
	dnsLeakRow  fyne.CanvasObject
	ipv6        *widget.Check
	commands    *widget.Label
	updTitle    *widget.Label
	updDetail   *widget.Label
	updCheck    *widget.Button
	updInstall  *widget.Button
	updProgress *widget.ProgressBar
	configPath  *widget.Label
	version     *widget.Label
	appLog      *widget.Label
	logAt       time.Time
	prefs       app.Prefs
}

func newSettingsTab(u *UI) *settingsTab {
	t := &settingsTab{u: u}
	setPref := func(f func(p *app.Prefs, on bool)) func(bool) {
		return func(on bool) {
			if t.binding {
				return
			}
			p := t.prefs
			f(&p, on)
			u.act("/api/prefs", p, "", nil)
		}
	}
	t.autostart = widget.NewCheck("Запускать при входе в систему", func(on bool) {
		if t.binding {
			return
		}
		msg := "Автозапуск выключен"
		if on {
			msg = "Автозапуск включён"
		}
		u.act("/api/autostart", map[string]bool{"on": on}, msg, nil)
	})
	t.autoHint = wrapped("")
	t.connect = widget.NewCheck("Подключаться при запуске", setPref(func(p *app.Prefs, on bool) { p.ConnectOnStart = on }))
	t.dnsLeak = widget.NewCheck("Не выпускать DNS мимо туннеля", setPref(func(p *app.Prefs, on bool) { p.BlockDNSLeak = on }))
	t.ipv6 = widget.NewCheck("IPv6 через туннель", setPref(func(p *app.Prefs, on bool) { p.IPv6 = on }))
	t.dnsLeakRow = container.NewVBox(t.dnsLeak, wrapped("Windows спрашивает DNS всех сетей сразу; фильтр оставляет только туннель "+
		"(российские домены всё равно резолвятся через DNS вашей сети)."))

	t.commands = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})

	t.updTitle = title("Версия")
	t.updDetail = wrapped("Проверка раз в 12 часов, файл версии — в репозитории на GitHub")
	t.updCheck = widget.NewButtonWithIcon("Проверить обновления", theme.ViewRefreshIcon(), func() {
		u.act("/api/update/check", struct{}{}, "", func(r *api.Result) {
			if r.Newer {
				u.offered = 0
			}
		})
	})
	t.updCheck.Importance = widget.LowImportance
	t.updInstall = widget.NewButton("Обновить", u.installUpdate)
	t.updInstall.Importance = widget.HighImportance
	t.updInstall.Hide()
	t.updProgress = widget.NewProgressBar()
	t.updProgress.Hide()

	t.configPath = wrapped("")
	copyConfig := widget.NewButtonWithIcon("Копировать", theme.ContentCopyIcon(), func() {
		u.act("/api/config", nil, "", func(r *api.Result) {
			u.copyText(r.Config, "Конфиг скопирован (без ключей и паролей)")
		})
	})
	copyConfig.Importance = widget.LowImportance

	t.appLog = widget.NewLabelWithStyle("—", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	t.appLog.Wrapping = fyne.TextWrapBreak
	t.appLog.SizeName = theme.SizeNameCaptionText
	logScroll := container.NewVScroll(t.appLog)
	logScroll.SetMinSize(fyne.NewSize(0, 200))
	copyLog := widget.NewButtonWithIcon("Копировать", theme.ContentCopyIcon(), func() {
		u.act("/api/applog", nil, "", func(r *api.Result) { u.copyText(r.Log, "Журнал скопирован") })
	})
	copyLog.Importance = widget.LowImportance

	t.version = title("VPN routing")
	quit := widget.NewButtonWithIcon("Выход", theme.LogoutIcon(), t.quit)

	head := canvas.NewText("Настройки", themePal().text)
	head.TextSize = 24
	head.TextStyle = fyne.TextStyle{Bold: true}
	body := container.NewVBox(
		container.NewHBox(head, layout.NewSpacer()),
		sectionHead("ЗАПУСК", ""),
		card(t.autostart, t.autoHint, widget.NewSeparator(), t.connect, wrapped("Аналог «Постоянной VPN» на Android")),
		sectionHead("СЕТЬ", ""),
		card(t.dnsLeakRow, t.ipv6, wrapped("Иначе IPv6-трафик идёт мимо VPN. Если в системе IPv6 выключен, настройка не действует.")),
		sectionHead("ОБНОВЛЕНИЯ", ""),
		card(container.NewBorder(nil, nil, iconBox("vpn", themePal().accent, true), container.NewHBox(t.updInstall, t.updCheck),
			container.New(&tightVBox{}, t.updTitle, t.updDetail)), t.updProgress),
		sectionHead("ДИАГНОСТИКА", ""),
		card(container.NewBorder(nil, nil, widget.NewIcon(theme.FileIcon()), copyConfig,
			container.New(&tightVBox{}, title("Конфиг ядра"), t.configPath))),
		sectionHead("ЖУРНАЛ ПРИЛОЖЕНИЯ", "", copyLog),
		card(logScroll),
		sectionHead("БЫСТРЫЕ КОМАНДЫ", ""),
		card(t.commands),
		sectionHead("О ПРОГРАММЕ", ""),
		card(container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(40, 40), canvas.NewImageFromResource(appIcon)), quit,
			container.New(&tightVBox{}, t.version, wrapped("Xray-core со сплит-маршрутизацией и клиент OpenVPN МГТУ.")))),
	)
	t.root = container.NewVScroll(container.NewPadded(body))
	return t
}

func (t *settingsTab) update(st *api.State, force bool) {
	t.prefs = st.Prefs
	win := st.Platform == "windows"
	t.binding = true
	t.autostart.SetChecked(st.Autostart.Enabled)
	t.connect.SetChecked(st.Prefs.ConnectOnStart)
	t.dnsLeak.SetChecked(st.Prefs.BlockDNSLeak)
	t.ipv6.SetChecked(st.Prefs.IPv6)
	t.binding = false
	if st.Autostart.Supported {
		t.autostart.Enable()
	} else {
		t.autostart.Disable()
	}
	if win {
		t.autostart.Text = "Запускать вместе с Windows"
		if t.u.o.Service {
			t.autoHint.SetText("Служба VPN стартует при загрузке, значок в трее появляется при входе. Подключаться ли сразу — настройка ниже.")
		} else {
			t.autoHint.SetText("Без запроса прав: задача планировщика с наивысшими правами. Окно не открывается — значок в трее.")
		}
		t.dnsLeakRow.Show()
	} else {
		t.autostart.Text = "Запускать при загрузке (служба systemd)"
		t.autoHint.SetText("Служба vpnrouting.service, как bmstu-vpn на Raspberry Pi. VPN работает и без открытого окна.")
		t.dnsLeakRow.Hide()
	}
	t.autostart.Refresh()
	cli := "vpnrouting"
	if win {
		cli = "vpnrouting.exe"
	}
	t.commands.SetText(fmt.Sprintf("%[1]s connect      включить VPN\n%[1]s disconnect   выключить VPN\n"+
		"%[1]s toggle       переключить\n%[1]s status       состояние\n%[1]s open         открыть это окно", cli))
	t.version.SetText("VPN routing " + st.Version)
	t.configPath.SetText(st.ConfigPath + " — UUID, ключи и пароли скрыты, файл можно показать кому угодно")

	up := st.Update
	t.updInstall.Hide()
	t.updProgress.Hide()
	t.updCheck.Show()
	switch {
	case up.Ready:
		t.updTitle.SetText("Обновлено")
		t.updDetail.SetText("Приложение перезапускается…")
		t.updCheck.Hide()
	case up.Downloading:
		name := ""
		if up.Info != nil {
			name = up.Info.VersionName
		}
		t.updTitle.SetText("Загрузка версии " + name)
		t.updCheck.Hide()
		if up.Total > 0 {
			t.updProgress.Show()
			t.updProgress.SetValue(float64(up.Done) / float64(up.Total))
			t.updDetail.SetText(fmtBytes(up.Done) + " из " + fmtBytes(up.Total))
		} else {
			t.updDetail.SetText(fmtBytes(up.Done))
		}
	case up.Newer && up.Info != nil:
		t.updTitle.SetText("Доступна версия " + up.Info.VersionName)
		d := up.Info.Notes
		if up.Info.Size > 0 {
			d += " · " + fmtBytes(up.Info.Size)
		}
		t.updDetail.SetText(d)
		t.updInstall.Show()
	default:
		t.updTitle.SetText("Версия " + up.Current)
		t.updDetail.SetText("Проверка раз в 12 часов, файл версии — в репозитории на GitHub")
	}
	if up.Error != "" {
		t.updDetail.SetText("Обновление не скачалось: " + up.Error)
	}
	if up.Checking {
		t.updCheck.Disable()
	} else {
		t.updCheck.Enable()
	}

	if force || time.Since(t.logAt) > 2*time.Second {
		t.logAt = time.Now()
		go func() {
			var r api.Result
			if err := t.u.o.Client.Call("/api/applog", nil, &r); err == nil {
				fyne.Do(func() {
					if t.appLog.Text != r.Log {
						t.appLog.SetText(r.Log)
					}
				})
			}
		}()
	}
}

// quit — на Windows «Выход» закрывает приложение целиком (VPN выключается); на Linux окно и служба
// раздельны — можно закрыть только окно или выключить VPN совсем.
func (t *settingsTab) quit() {
	st := t.u.state()
	switch {
	case t.u.o.Service:
		// Windows со службой: окно закрывается, VPN выключается; служба остаётся для следующего запуска
		t.u.confirm("Выход", "Выключить VPN и закрыть окно? Чтобы VPN продолжал работать, просто закройте окно — "+
			"значок останется в трее.", "Выход", func() {
			if t.u.o.OnQuit != nil {
				t.u.o.OnQuit()
			}
		})
	case st != nil && st.Platform == "windows":
		t.u.confirm("Выход", "Выключить VPN и закрыть приложение?", "Выход", func() {
			if t.u.o.OnQuit != nil {
				t.u.o.OnQuit()
			}
		})
	default:
		t.u.confirm("Выход", "Выключить VPN и остановить службу? Чтобы VPN продолжал работать, просто закройте окно.",
			"Выключить", func() {
				go func() {
					_, _ = t.u.o.Client.Do("/api/quit", nil)
					fyne.Do(t.u.app.Quit)
				}()
			})
	}
}
