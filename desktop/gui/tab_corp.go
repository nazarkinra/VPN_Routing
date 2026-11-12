package gui

import (
	"io"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
)

// Вкладка МГТУ: профиль с переключателем, состояние, статистика, учётные данные, файл, журнал (CorpFragment.java).
type corpTab struct {
	u    *UI
	root fyne.CanvasObject

	power        *powerButton
	profile      *widget.Label
	stateTitle   *canvas.Text
	stateDetail  *widget.Label
	stats        *fyne.Container
	statDuration *widget.Label
	statAddress  *widget.Label
	statIn       *widget.Label
	statOut      *widget.Label
	credUser     *widget.Label
	credPass     *widget.Label
	fileTitle    *widget.Label
	fileInfo     *widget.Label
	restore      *widget.Button
	log          *widget.Label
	logAt        time.Time

	lastIn, lastOut int64
	lastAt          time.Time
}

func newCorpTab(u *UI) *corpTab {
	t := &corpTab{u: u}
	// Кнопка питания VPN МГТУ — как на вкладке VPN, но включает только туннель университета
	t.power = newPowerButton(t.toggle)
	t.stateTitle = canvas.NewText("Не подключено", themePal().text)
	t.stateTitle.TextSize = 22
	t.stateTitle.TextStyle = fyne.TextStyle{Bold: true}
	t.stateDetail = widget.NewLabel("")
	t.stateDetail.Wrapping = fyne.TextWrapWord
	t.profile = widget.NewLabel("Профиль не загружен")
	t.profile.Wrapping = fyne.TextWrapWord
	t.profile.Importance = widget.LowImportance

	t.statDuration, t.statAddress, t.statIn, t.statOut = statValue(), statValue(), statValue(), statValue()
	t.stats = container.NewVBox(sectionHead("СТАТИСТИКА", ""), card(container.NewGridWithColumns(2,
		statCell("Длительность", t.statDuration), statCell("Адрес в сети", t.statAddress),
		statCell("Получено", t.statIn), statCell("Отправлено", t.statOut))))
	t.stats.Hide()

	t.credUser = title("Не заданы")
	t.credPass = wrapped("Логин и пароль не заданы")
	editCred := widget.NewButtonWithIcon("Изменить", theme.DocumentCreateIcon(), t.editCredentials)
	editCred.Importance = widget.LowImportance

	t.fileTitle = title("Файл .ovpn не загружен")
	t.fileInfo = wrapped("Нажмите «Импорт» и выберите .ovpn, выданный университетом")
	importBtn := widget.NewButtonWithIcon("Импорт", theme.FileIcon(), t.importOvpn)
	importBtn.Importance = widget.LowImportance
	t.restore = widget.NewButtonWithIcon("Вернуть прежний", theme.ViewRefreshIcon(), func() {
		u.act("/api/bmstu/restore", struct{}{}, "", nil)
	})
	t.restore.Importance = widget.LowImportance
	t.restore.Hide()

	t.log = widget.NewLabelWithStyle("Журнал пуст: туннель ещё не запускался.", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	t.log.Wrapping = fyne.TextWrapBreak
	t.log.SizeName = theme.SizeNameCaptionText
	logScroll := container.NewVScroll(t.log)
	logScroll.SetMinSize(fyne.NewSize(0, 220))
	copyLog := widget.NewButtonWithIcon("Копировать", theme.ContentCopyIcon(), func() {
		u.act("/api/bmstu/log", nil, "", func(r *api.Result) { u.copyText(r.Log, "Журнал скопирован") })
	})
	copyLog.Importance = widget.LowImportance

	head := canvas.NewText("VPN МГТУ", themePal().text)
	head.TextSize = 24
	head.TextStyle = fyne.TextStyle{Bold: true}

	connect := card(container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(112, 112), t.power), nil,
		container.NewVBox(layout.NewSpacer(), t.stateTitle, t.stateDetail, t.profile, layout.NewSpacer())))
	body := container.NewVBox(
		container.NewHBox(head, layout.NewSpacer()),
		connect,
		t.stats,
		sectionHead("УЧЁТНЫЕ ДАННЫЕ", ""),
		card(container.NewBorder(nil, nil, iconBox("lock", themePal().accent, false), editCred,
			container.New(&tightVBox{}, t.credUser, t.credPass))),
		sectionHead("ФАЙЛ ПРОФИЛЯ", ""),
		card(container.NewBorder(nil, nil, widget.NewIcon(theme.FileIcon()), container.NewHBox(t.restore, importBtn),
			container.New(&tightVBox{}, t.fileTitle, t.fileInfo))),
		sectionHead("ЖУРНАЛ", "", copyLog),
		card(logScroll),
		wrapped("Через туннель МГТУ идёт только bmstu.ru и его поддомены. VPN МГТУ и основной VPN включаются "+
			"независимо: каждый своей кнопкой. Пока туннель не поднят, сайты МГТУ открываются напрямую."),
	)
	t.root = container.NewVScroll(container.NewPadded(body))
	return t
}

func statValue() *widget.Label {
	l := widget.NewLabelWithStyle("—", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return l
}

func statCell(name string, value *widget.Label) fyne.CanvasObject {
	k := subtitle(name)
	return container.New(&tightVBox{}, container.NewPadded(k), value)
}

func (t *corpTab) update(st *api.State, force bool) {
	b, s := st.Bmstu, st.Bmstu.Status

	// Кнопка питания: состояние только VPN МГТУ, основной VPN на неё не влияет
	power, head, detail := "stopped", "Не подключено", "Нажмите на кнопку, чтобы подключиться"
	important := widget.MediumImportance
	switch {
	case b.Enabled && s.State == "up":
		power, head, detail = "connected", "Подключено", "bmstu.ru идёт через сеть МГТУ"
		if s.UpSince > 0 {
			detail = fmtDuration(time.Since(time.UnixMilli(s.UpSince))) + " · " + detail
		}
	case b.Enabled && s.State == "waiting":
		power, head, detail = "connecting", "Нет соединения", s.Message
		important = widget.DangerImportance
	case b.Enabled:
		power, head, detail = "connecting", "Подключение…", "Рукопожатие с сервером OpenVPN"
	case s.State == "error":
		power, head, detail = "error", "Ошибка подключения", s.Message
		important = widget.DangerImportance
	case !b.HasOvpn:
		detail = "Сначала импортируйте .ovpn от университета в разделе «Файл профиля»"
	case !b.HasCredentials:
		detail = "Сначала укажите логин и пароль в разделе «Учётные данные»"
	}
	t.power.SetState(power)
	t.stateTitle.Text = head
	t.stateTitle.Color = themePal().text
	t.stateTitle.Refresh()
	t.stateDetail.Importance = important
	if t.stateDetail.Text != detail {
		t.stateDetail.SetText(detail)
	} else {
		t.stateDetail.Refresh()
	}

	if b.HasOvpn {
		t.profile.SetText("МГТУ им. Н. Э. Баумана · " + b.Profile)
		t.fileTitle.SetText("Загруженный .ovpn")
		t.fileInfo.SetText(b.FileInfo)
	} else {
		t.profile.SetText("Профиль не загружен")
		t.fileTitle.SetText("Файл .ovpn не загружен")
		t.fileInfo.SetText("Нажмите «Импорт» и выберите .ovpn, выданный университетом")
	}
	if b.HasPrev {
		t.restore.Show()
	} else {
		t.restore.Hide()
	}
	if b.User != "" {
		t.credUser.SetText(b.User)
	} else {
		t.credUser.SetText("Не заданы")
	}
	if b.HasCredentials {
		t.credPass.SetText("Пароль: ••••••••")
	} else {
		t.credPass.SetText("Логин и пароль не заданы")
	}

	// Статистика, как в OpenVPN Connect: время, адрес, трафик и скорость
	if s.State == "up" && b.Enabled {
		t.stats.Show()
		if s.UpSince > 0 {
			t.statDuration.SetText(fmtDuration(time.Since(time.UnixMilli(s.UpSince))))
		}
		t.statAddress.SetText(orDash(s.LocalIP))
		now := time.Now()
		in, out := fmtBytes(s.BytesIn), fmtBytes(s.BytesOut)
		if !t.lastAt.IsZero() && now.Sub(t.lastAt) > 500*time.Millisecond {
			dt := now.Sub(t.lastAt).Seconds()
			in += "  ·  " + fmtBytes(int64(float64(max(0, s.BytesIn-t.lastIn))/dt)) + "/с"
			out += "  ·  " + fmtBytes(int64(float64(max(0, s.BytesOut-t.lastOut))/dt)) + "/с"
		}
		t.lastIn, t.lastOut, t.lastAt = s.BytesIn, s.BytesOut, now
		t.statIn.SetText(in)
		t.statOut.SetText(out)
	} else {
		t.stats.Hide()
		t.lastAt = time.Time{}
	}

	if force || time.Since(t.logAt) > 2*time.Second {
		t.logAt = time.Now()
		go func() {
			var r api.Result
			if err := t.u.o.Client.Call("/api/bmstu/log", nil, &r); err == nil {
				text := r.Log
				if text == "" {
					text = "Журнал пуст: туннель ещё не запускался."
				}
				fyne.Do(func() {
					if t.log.Text != text {
						t.log.SetText(text)
					}
				})
			}
		}()
	}
}

// toggle — кнопка питания VPN МГТУ. Без файла профиля или учётных данных — сначала они.
func (t *corpTab) toggle() {
	st := t.u.state()
	if st == nil {
		return
	}
	switch {
	case st.Bmstu.Enabled:
		t.u.act("/api/bmstu/disconnect", struct{}{}, "", nil)
	case !st.Bmstu.HasOvpn:
		t.importOvpn()
	case !st.Bmstu.HasCredentials:
		t.editCredentialsThen(func() { t.u.act("/api/bmstu/connect", struct{}{}, "", nil) })
	default:
		t.u.act("/api/bmstu/connect", struct{}{}, "", nil)
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (t *corpTab) editCredentials() { t.editCredentialsThen(nil) }

// editCredentialsThen — диалог логина и пароля; after — после сохранения (например, подключиться).
func (t *corpTab) editCredentialsThen(after func()) {
	st := t.u.state()
	user := widget.NewEntry()
	pass := widget.NewPasswordEntry()
	passLabel := "Пароль"
	if st != nil {
		user.SetText(st.Bmstu.User)
		if st.Bmstu.HasCredentials {
			passLabel = "Пароль (пусто — не менять)"
		}
	}
	d := dialog.NewForm("Учётные данные", "Сохранить", "Отмена", []*widget.FormItem{
		widget.NewFormItem("Логин МГТУ", user),
		widget.NewFormItem(passLabel, pass),
	}, func(ok bool) {
		if ok {
			t.u.act("/api/bmstu/credentials", map[string]string{"user": user.Text, "pass": pass.Text}, "Учётные данные сохранены",
				func(*api.Result) {
					if after != nil {
						after()
					}
				})
		}
	}, t.u.win)
	d.Resize(fyne.NewSize(460, 0))
	d.Show()
	t.u.win.Canvas().Focus(user)
}

func (t *corpTab) importOvpn() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, 100001))
		if err != nil {
			t.u.info("Не удалось прочитать файл", err.Error())
			return
		}
		if len(data) > 100000 {
			t.u.info("Файл не подходит", "Файл слишком большой для конфига OpenVPN.")
			return
		}
		go func() {
			res, err := t.u.o.Client.Do("/api/bmstu/ovpn", map[string]string{"text": string(data)})
			fyne.Do(func() {
				if err != nil {
					t.u.info("Файл не подходит", err.Error())
					return
				}
				t.u.notify(strings.TrimSpace(res.Message))
				t.u.refreshNow()
			})
		}()
	}, t.u.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".ovpn", ".conf"}))
	d.Resize(fyne.NewSize(760, 520))
	d.Show()
}
