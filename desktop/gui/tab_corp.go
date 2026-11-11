package gui

import (
	"image/color"
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

	enabled      *widget.Check
	binding      bool
	profile      *widget.Label
	stateDot     *canvas.Circle
	stateTitle   *widget.Label
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
	t.enabled = widget.NewCheck("VPN МГТУ включён", func(on bool) {
		if t.binding {
			return
		}
		u.act("/api/bmstu/enabled", map[string]bool{"on": on}, "", func(*api.Result) {
			if st := u.state(); on && st != nil && !st.Bmstu.HasCredentials {
				t.editCredentials()
			}
		})
	})
	t.profile = widget.NewLabel("Профиль не загружен")
	t.profile.Importance = widget.LowImportance
	t.stateDot = canvas.NewCircle(colIdle)
	t.stateTitle = widget.NewLabelWithStyle("Отключено", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	t.stateDetail = wrapped("")

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

	profileCard := card(
		container.NewBorder(nil, nil, iconBox("school", colCorp, true), t.enabled,
			container.New(&tightVBox{}, title("МГТУ им. Н. Э. Баумана"), t.profile)),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), t.stateDot)), nil,
			container.New(&tightVBox{}, t.stateTitle, t.stateDetail)),
	)
	body := container.NewVBox(
		container.NewHBox(head, layout.NewSpacer()),
		sectionHead("ПРОФИЛЬ", ""), profileCard,
		t.stats,
		sectionHead("УЧЁТНЫЕ ДАННЫЕ", ""),
		card(container.NewBorder(nil, nil, iconBox("lock", themePal().accent, false), editCred,
			container.New(&tightVBox{}, t.credUser, t.credPass))),
		sectionHead("ФАЙЛ ПРОФИЛЯ", ""),
		card(container.NewBorder(nil, nil, widget.NewIcon(theme.FileIcon()), container.NewHBox(t.restore, importBtn),
			container.New(&tightVBox{}, t.fileTitle, t.fileInfo))),
		sectionHead("ЖУРНАЛ", "", copyLog),
		card(logScroll),
		wrapped("Через туннель МГТУ идёт только bmstu.ru и его поддомены. Туннель работает вместе с основным VPN. "+
			"Пока туннель не поднят, сайты МГТУ открываются напрямую."),
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
	vpnActive := st.VPN.State == "connected" || st.VPN.State == "connecting"
	t.binding = true
	t.enabled.SetChecked(b.Enabled)
	t.binding = false

	var head, detail string
	var c color.Color
	switch {
	case !b.Enabled:
		head, detail, c = "Отключено", "Включите, чтобы сайты МГТУ шли через корпоративную сеть", colIdle
	case !b.HasOvpn:
		head, detail, c = "Нужен файл профиля", "Импортируйте .ovpn от университета в разделе «Файл профиля»", colWait
	case !b.HasCredentials:
		head, detail, c = "Нужны логин и пароль", "Укажите их в разделе «Учётные данные»", colWait
	case !vpnActive:
		head, detail, c = "Ожидает основной VPN", "Подключится вместе с основным VPN на вкладке VPN", colIdle
	case s.State == "up":
		head, detail, c = "Подключено", "bmstu.ru идёт через сеть МГТУ", colOK
	case s.State == "connecting":
		head, detail, c = "Подключение…", "Рукопожатие с сервером OpenVPN", colWait
	case s.State == "waiting":
		head, detail, c = "Нет соединения", s.Message, colError
	default:
		head, detail, c = "Отключено", s.Message, colIdle
	}
	t.stateTitle.SetText(head)
	if t.stateDetail.Text != detail {
		t.stateDetail.SetText(detail)
	}
	t.stateDot.FillColor = c
	t.stateDot.Refresh()

	if b.HasOvpn {
		t.profile.SetText(b.Profile)
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
	if s.State == "up" && vpnActive {
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (t *corpTab) editCredentials() {
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
			t.u.act("/api/bmstu/credentials", map[string]string{"user": user.Text, "pass": pass.Text}, "Учётные данные сохранены", nil)
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
