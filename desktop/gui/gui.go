// Package gui — окно приложения (Fyne): вкладки VPN, МГТУ, Напрямую, Настройки — как в Android-версии.
//
// Окно ничего не делает с сетью само: все действия — команды api (на Windows внутри этого же процесса,
// на Linux — к службе через сокет). Состояние перечитывается раз в секунду.
package gui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/draw"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/assets"
)

// Options — как запускать окно.
type Options struct {
	Client *api.Client
	// Tray — значок в трее; закрытие окна прячет его (Windows и Linux).
	Tray bool
	// Hidden — не показывать окно при запуске (автозапуск при входе в систему).
	Hidden bool
	// OnQuit — выход из приложения («Выход» в трее и настройках). nil — только закрыть окно.
	OnQuit func()
	// StartService — запустить службу, если она не отвечает. nil — кнопки нет.
	StartService func() error
	// Service — окно управляет службой (VPN работает и без окна).
	Service bool
	// OnServiceVersion — версия службы (вызывается при каждом ответе; окно сравнивает со своей).
	OnServiceVersion func(version string)
	// OnState — каждый ответ службы (Linux: значок в трее при входе в систему — вместе с автозапуском).
	OnState func(st *api.State)
}

// UI — окно приложения.
type UI struct {
	o   Options
	app fyne.App
	win fyne.Window
	// variant — тема (светлая/тёмная), в цветах которой построено окно
	variant fyne.ThemeVariant

	st      *api.State
	stMu    sync.Mutex
	offline bool

	tabs    *container.AppTabs
	vpn     *vpnTab
	corp    *corpTab
	direct  *directTab
	setTab  *settingsTab
	toast   *widget.Label
	toastAt time.Time
	banner  *fyne.Container

	trayStatus, trayToggle *fyne.MenuItem
	trayCorp               *fyne.MenuItem
	trayMenu               *fyne.Menu
	trayState              string
	offered                int
}

var appIcon = fyne.NewStaticResource("vpnrouting.png", assets.IconPNG)

// New создаёт окно (до Run). Show можно вызывать из любого потока.
func New(o Options) *UI {
	a := fapp.NewWithID("ru.vpnrouting.desktop")
	a.Settings().SetTheme(appTheme{})
	a.SetIcon(appIcon)
	u := &UI{o: o, app: a}
	u.win = a.NewWindow("VPN routing")
	u.win.SetIcon(appIcon)
	u.win.Resize(fyne.NewSize(900, 680))
	u.build()
	// Карточки, заголовки и кнопка подключения красятся цветами темы при создании. Тема системы может
	// смениться и после запуска (на Linux её сообщает портал уже после старта, плюс ночной режим по
	// расписанию) — тогда окно пересобирается, иначе тёмные карточки остаются на светлом фоне.
	u.variant = a.Settings().ThemeVariant()
	a.Settings().AddListener(func(s fyne.Settings) {
		v := s.ThemeVariant()
		fyne.Do(func() {
			if v != u.variant {
				u.variant = v
				u.rebuild()
			}
		})
	})
	if o.Tray {
		u.setupTray()
		u.win.SetCloseIntercept(func() { u.win.Hide() })
	}
	return u
}

// Run показывает окно (если не Hidden) и работает до выхода. Вызывать из главного потока.
func (u *UI) Run() {
	go u.pollLoop()
	if u.o.Hidden && u.o.Tray {
		u.app.Run()
		return
	}
	u.win.ShowAndRun()
}

// Show — показать окно поверх остальных (повторный запуск ярлыка, щелчок по значку в трее).
func (u *UI) Show() {
	fyne.Do(func() {
		u.win.Show()
		u.win.RequestFocus()
	})
}

// Quit — закрыть окно и цикл интерфейса (выход приложения).
func (u *UI) Quit() { fyne.Do(u.app.Quit) }

func (u *UI) build() {
	u.vpn = newVPNTab(u)
	u.corp = newCorpTab(u)
	u.direct = newDirectTab(u)
	u.setTab = newSettingsTab(u)
	u.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("VPN", icon("vpn"), u.vpn.root),
		container.NewTabItemWithIcon("МГТУ", icon("school"), u.corp.root),
		container.NewTabItemWithIcon("Напрямую", icon("split"), u.direct.root),
		container.NewTabItemWithIcon("Настройки", theme.SettingsIcon(), u.setTab.root),
	)
	u.tabs.SetTabLocation(container.TabLocationLeading)
	u.tabs.OnSelected = func(*container.TabItem) { u.render(true) }
	// VPNROUTING_UI_TAB=0…3 — открыть сразу нужную вкладку (для проверки внешнего вида)
	if i, err := strconv.Atoi(os.Getenv("VPNROUTING_UI_TAB")); err == nil && i >= 0 && i < len(u.tabs.Items) {
		u.tabs.SelectIndex(i)
	}

	u.toast = widget.NewLabel("")
	u.toast.Wrapping = fyne.TextWrapWord
	u.toast.Hide()
	u.banner = container.NewVBox()
	u.win.SetContent(container.NewBorder(u.banner, container.NewPadded(u.toast), nil, nil, u.tabs))
}

// rebuild — пересоздать содержимое окна в цветах текущей темы, оставив открытой ту же вкладку.
func (u *UI) rebuild() {
	sel := u.tabs.SelectedIndex()
	u.offline = false // плашка «служба не запущена» появится снова при следующем опросе
	u.build()
	u.tabs.SelectIndex(sel)
	u.render(true)
}

// ---------- Состояние ----------

func (u *UI) state() *api.State {
	u.stMu.Lock()
	defer u.stMu.Unlock()
	return u.st
}

func (u *UI) pollLoop() {
	for {
		st, err := u.o.Client.State()
		fyne.Do(func() { u.apply(st, err) })
		time.Sleep(time.Second)
	}
}

// refreshNow — перечитать состояние сразу (после команды).
func (u *UI) refreshNow() {
	go func() {
		st, err := u.o.Client.State()
		fyne.Do(func() { u.apply(st, err) })
	}()
}

func (u *UI) apply(st *api.State, err error) {
	if err != nil {
		u.showOffline(err)
		return
	}
	if u.offline {
		u.offline = false
		u.banner.RemoveAll()
		u.banner.Refresh()
	}
	u.stMu.Lock()
	u.st = st
	u.stMu.Unlock()
	if u.o.OnServiceVersion != nil && st.Version != "" {
		u.o.OnServiceVersion(st.Version)
	}
	if u.o.OnState != nil {
		u.o.OnState(st)
	}
	u.render(false)
	if !u.toastAt.IsZero() && time.Since(u.toastAt) > 4*time.Second {
		u.toast.Hide()
		u.toastAt = time.Time{}
	}
}

func (u *UI) render(force bool) {
	st := u.state()
	if st == nil {
		return
	}
	switch u.tabs.SelectedIndex() {
	case 0:
		u.vpn.update(st, force)
	case 1:
		u.corp.update(st, force)
	case 2:
		u.direct.update(st, force)
	case 3:
		u.setTab.update(st, force)
	}
	u.updateTray(st)
	u.maybeOfferUpdate(st)
}

// showOffline — служба не отвечает (Linux: остановлена или не установлена).
func (u *UI) showOffline(err error) {
	if u.offline {
		return
	}
	u.offline = true
	msg := widget.NewLabelWithStyle("Служба VPN routing не запущена", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	detail := wrapped(err.Error() + ". Окно подключится к ней само, как только она заработает.")
	objs := []fyne.CanvasObject{msg, detail}
	if u.o.StartService != nil {
		var btn *widget.Button
		btn = widget.NewButtonWithIcon("Запустить службу", theme.MediaPlayIcon(), func() {
			btn.Disable()
			go func() {
				err := u.o.StartService()
				fyne.Do(func() {
					btn.Enable()
					if err != nil {
						u.notify("Служба не запустилась: " + err.Error())
					}
				})
			}()
		})
		btn.Importance = widget.HighImportance
		objs = append(objs, container.NewHBox(btn))
	}
	u.banner.RemoveAll()
	u.banner.Add(container.NewPadded(card(objs...)))
	u.banner.Refresh()
}

// ---------- Команды ----------

// notify — сообщение внизу окна на несколько секунд (как Toast на Android).
func (u *UI) notify(text string) {
	if text == "" {
		return
	}
	u.toast.SetText(text)
	u.toast.Show()
	u.toastAt = time.Now()
}

// act выполняет команду в фоне: сообщение об ошибке или ответ — внизу окна; done (если есть) — в потоке окна.
func (u *UI) act(path string, body any, okText string, done func(*api.Result)) {
	go func() {
		r, err := u.o.Client.Do(path, body)
		fyne.Do(func() {
			switch {
			case err != nil:
				u.notify(err.Error())
			case r.Message != "":
				u.notify(r.Message)
			case okText != "":
				u.notify(okText)
			}
			if err == nil && done != nil {
				done(r)
			}
			u.refreshNow()
		})
	}()
}

func (u *UI) copyText(text, done string) {
	u.app.Clipboard().SetContent(text)
	u.notify(done)
}

func (u *UI) confirm(title, message, ok string, f func()) {
	d := dialog.NewConfirm(title, message, func(yes bool) {
		if yes {
			f()
		}
	}, u.win)
	d.SetConfirmText(ok)
	d.SetDismissText("Отмена")
	d.Show()
}

func (u *UI) info(title, message string) {
	dialog.NewInformation(title, message, u.win).Show()
}

// signature — отпечаток данных: список перерисовывается, только если он изменился.
func signature(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------- Обновления ----------

// maybeOfferUpdate — предложение обновиться, как диалог при запуске Android-приложения.
func (u *UI) maybeOfferUpdate(st *api.State) {
	up := st.Update
	if !up.Offer || up.Downloading || up.Info == nil || u.offered == up.Info.VersionCode {
		return
	}
	u.offered = up.Info.VersionCode
	text := "Установлена версия " + up.Current + "."
	if up.Info.Size > 0 {
		text += "\nРазмер: " + fmtBytes(up.Info.Size) + "."
	}
	if up.Info.Notes != "" {
		text += "\n\nЧто нового:\n" + up.Info.Notes
	}
	text += "\n\nVPN ненадолго отключится и включится снова."
	lbl := widget.NewLabel(text)
	lbl.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("Доступна версия "+up.Info.VersionName, "Обновить", "Позже", lbl, func(yes bool) {
		if yes {
			u.installUpdate()
		} else {
			u.act("/api/update/skip", nil, "", nil)
		}
	}, u.win)
	d.Resize(fyne.NewSize(460, 0))
	d.Show()
}

func (u *UI) installUpdate() {
	u.act("/api/update/install", nil, "Загрузка обновления…", func(*api.Result) { u.tabs.SelectIndex(3) })
}

// ---------- Трей (Windows; Linux — StatusNotifierItem, в GNOME через расширение AppIndicator) ----------

func (u *UI) setupTray() {
	desk, ok := u.app.(desktop.App)
	if !ok {
		return
	}
	u.trayStatus = fyne.NewMenuItem("Не подключено", nil)
	u.trayStatus.Disabled = true
	u.trayToggle = fyne.NewMenuItem("Подключить VPN", func() { u.act("/api/vpn/toggle", nil, "", nil) })
	// VPN МГТУ — отдельным пунктом, независимо от основного
	u.trayCorp = fyne.NewMenuItem("Подключить VPN МГТУ", func() { u.act("/api/bmstu/toggle", nil, "", nil) })
	open := fyne.NewMenuItem("Открыть окно", u.Show)
	quit := fyne.NewMenuItem("Выход", func() {
		if u.o.OnQuit != nil {
			u.o.OnQuit()
		} else {
			u.app.Quit()
		}
	})
	quit.IsQuit = true
	u.trayMenu = fyne.NewMenu("VPN routing", u.trayStatus, fyne.NewMenuItemSeparator(), u.trayToggle, u.trayCorp, open,
		fyne.NewMenuItemSeparator(), quit)
	desk.SetSystemTrayMenu(u.trayMenu)
	desk.SetSystemTrayIcon(trayIcon("stopped"))
	desk.SetSystemTrayWindow(u.win)
}

var stateTitles = map[string]string{
	"stopped": "Не подключено", "connecting": "Подключение…", "connected": "Подключено", "error": "Ошибка подключения",
}

func (u *UI) updateTray(st *api.State) {
	if u.trayMenu == nil {
		return
	}
	s := string(st.VPN.State)
	corp := ""
	if st.Bmstu.Enabled {
		corp = "connecting"
		if st.Bmstu.Status.State == "up" {
			corp = "connected"
		}
	}
	key := s + "/" + corp
	if key == u.trayState {
		return
	}
	u.trayState = key
	u.trayStatus.Label = "VPN: " + strings.ToLower(stateTitles[s])
	if corp != "" {
		u.trayStatus.Label += " · МГТУ: " + strings.ToLower(stateTitles[corp])
	}
	if s == "connected" || s == "connecting" {
		u.trayToggle.Label = "Отключить VPN"
	} else {
		u.trayToggle.Label = "Подключить VPN"
	}
	if corp != "" {
		u.trayCorp.Label = "Отключить VPN МГТУ"
	} else {
		u.trayCorp.Label = "Подключить VPN МГТУ"
	}
	u.trayMenu.Refresh()
	// Точка на значке: основной VPN, а если он выключен — VPN МГТУ
	icon := s
	if (s == "stopped" || s == "error") && corp != "" {
		icon = corp
	}
	if desk, ok := u.app.(desktop.App); ok {
		desk.SetSystemTrayIcon(trayIcon(icon))
	}
}

var trayCache = map[string]fyne.Resource{}

// trayIcon — значок приложения 32×32 с точкой состояния в правом нижнем углу.
func trayIcon(state string) fyne.Resource {
	if r, ok := trayCache[state]; ok {
		return r
	}
	dotColor := map[string]color.NRGBA{"connected": colOK, "connecting": colWait, "error": colError}[state]
	if dotColor.A == 0 {
		dotColor = colIdle
	}
	const n = 32
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	if src, err := png.Decode(bytes.NewReader(assets.IconPNG)); err == nil {
		draw.CatmullRom.Scale(img, img.Bounds(), src, src.Bounds(), draw.Src, nil)
	}
	for y := 16; y < n; y++ {
		for x := 16; x < n; x++ {
			d := math.Hypot(float64(x)+0.5-24.5, float64(y)+0.5-24.5)
			switch {
			case d <= 5.2:
				img.SetNRGBA(x, y, dotColor)
			case d <= 6.8:
				img.SetNRGBA(x, y, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	r := fyne.NewStaticResource("tray-"+state+".png", buf.Bytes())
	trayCache[state] = r
	return r
}

// spacer заданной высоты.
func vspace(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, h))
	return r
}
