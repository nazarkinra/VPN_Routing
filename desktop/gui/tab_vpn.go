package gui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
)

// Вкладка VPN: кнопка подключения, текущий сервер, серверы и подписки (VpnFragment.java).
type vpnTab struct {
	u    *UI
	root fyne.CanvasObject

	power        *powerButton
	statusTitle  *canvas.Text
	statusDetail *widget.Label
	current      *widget.Label
	check        *widget.Button
	pingAll      *widget.Button
	list         *fyne.Container
	listSig      string
}

func newVPNTab(u *UI) *vpnTab {
	t := &vpnTab{u: u}
	t.power = newPowerButton(func() { u.act("/api/vpn/toggle", nil, "", nil) })
	t.statusTitle = canvas.NewText("Не подключено", themePal().text)
	t.statusTitle.TextSize = 22
	t.statusTitle.TextStyle = fyne.TextStyle{Bold: true}
	t.statusDetail = widget.NewLabel("")
	t.statusDetail.Wrapping = fyne.TextWrapWord
	t.current = widget.NewLabel("")
	t.current.Wrapping = fyne.TextWrapWord
	t.current.Importance = widget.LowImportance
	t.check = widget.NewButtonWithIcon("Проверить", icon("speed"), func() {
		u.act("/api/vpn/check", nil, "", func(r *api.Result) {
			u.notify(fmt.Sprintf("Задержка через VPN: %d мс", r.Delay))
		})
	})
	t.check.Hide()

	t.pingAll = widget.NewButtonWithIcon("Пинг", icon("speed"), func() { u.act("/api/servers/ping", struct{}{}, "", nil) })
	refresh := widget.NewButtonWithIcon("Подписки", theme.ViewRefreshIcon(), func() {
		u.act("/api/subs/refresh", struct{}{}, "", func(r *api.Result) {
			if r.Started > 0 {
				u.notify(fmt.Sprintf("Обновляется подписок: %d", r.Started))
			}
		})
	})
	add := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), t.showAdd)
	add.Importance = widget.HighImportance

	head := canvas.NewText("VPN", themePal().text)
	head.TextSize = 24
	head.TextStyle = fyne.TextStyle{Bold: true}
	bar := container.NewHBox(head, layout.NewSpacer(), t.pingAll, refresh, add)

	connect := card(container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(112, 112), t.power),
		container.NewVBox(layout.NewSpacer(), t.check, layout.NewSpacer()),
		container.NewVBox(layout.NewSpacer(), t.statusTitle, t.statusDetail, t.current, layout.NewSpacer())))

	t.list = container.NewVBox()
	t.root = container.NewVScroll(container.NewPadded(container.NewVBox(bar, connect, t.list)))
	return t
}

func (t *vpnTab) update(st *api.State, force bool) {
	v := st.VPN
	t.power.SetState(string(v.State))
	t.statusTitle.Text = stateTitles[string(v.State)]
	t.statusTitle.Color = themePal().text
	t.statusTitle.Refresh()
	detail := ""
	t.statusDetail.Importance = widget.MediumImportance
	switch v.State {
	case "connected":
		var parts []string
		if v.Since > 0 {
			parts = append(parts, fmtDuration(time.Since(time.UnixMilli(v.Since))))
		}
		if v.Delay > 0 {
			parts = append(parts, fmt.Sprintf("%d мс", v.Delay))
		} else if v.Delay < 0 {
			parts = append(parts, "сервер не отвечает")
			t.statusDetail.Importance = widget.DangerImportance
		}
		detail = strings.Join(parts, " · ")
		if strings.Contains(v.Message, "не отвечает") || strings.Contains(v.Message, "не применён") {
			detail += "\n" + v.Message
		}
		t.check.Show()
	case "stopped":
		detail = "Нажмите на кнопку, чтобы подключиться"
		t.check.Hide()
	case "error":
		detail = v.Message
		t.statusDetail.Importance = widget.DangerImportance
		t.check.Hide()
	default:
		detail = v.Message
		t.check.Hide()
	}
	if t.statusDetail.Text != detail {
		t.statusDetail.SetText(detail)
	} else {
		t.statusDetail.Refresh()
	}
	cur := "Сервер не выбран · добавьте ссылку vless://, hysteria2:// или подписку"
	if c := st.Current; c != nil {
		cur = c.Title + " · " + c.Subtitle
	}
	if t.current.Text != cur {
		t.current.SetText(cur)
	}
	if st.Pinging {
		t.pingAll.Disable()
	} else {
		t.pingAll.Enable()
	}

	sig := signature(st.Groups)
	if !force && sig == t.listSig {
		return
	}
	t.listSig = sig
	t.list.Objects = t.buildList(st)
	t.list.Refresh()
}

func (t *vpnTab) buildList(st *api.State) []fyne.CanvasObject {
	var objs []fyne.CanvasObject
	own := st.Groups[0]
	if len(st.Groups) == 1 && len(own.Servers) == 0 {
		return []fyne.CanvasObject{card(wrapped("Серверов пока нет. Добавьте ссылку на сервер (vless://, hysteria2://) " +
			"или адрес подписки — все серверы из неё появятся в списке."))}
	}
	if len(own.Servers) > 0 {
		objs = append(objs, sectionHead("МОИ СЕРВЕРЫ", fmt.Sprint(len(own.Servers))))
		var rows []fyne.CanvasObject
		for i, s := range own.Servers {
			if i > 0 {
				rows = append(rows, widget.NewSeparator())
			}
			rows = append(rows, t.serverRow(s))
		}
		objs = append(objs, card(rows...))
	}
	for _, g := range st.Groups[1:] {
		objs = append(objs, sectionHead("ПОДПИСКА", fmt.Sprint(len(g.Servers))))
		objs = append(objs, t.subCard(g))
	}
	return objs
}

func (t *vpnTab) serverRow(s api.ServerView) fyne.CanvasObject {
	radio := widget.NewIcon(theme.RadioButtonIcon())
	if s.Active {
		radio.SetResource(theme.NewPrimaryThemedResource(theme.RadioButtonCheckedIcon()))
	}
	var right []fyne.CanvasObject
	switch {
	case s.Pinging:
		right = append(right, chip("…", themePal().text2))
	case s.Ping != nil && *s.Ping < 0:
		right = append(right, chip("нет ответа", colError))
	case s.Ping != nil:
		right = append(right, chip(fmt.Sprintf("%d мс", *s.Ping), pingColor(*s.Ping)))
	}
	var more *widget.Button
	more = iconButton(theme.MoreVerticalIcon(), "Ещё", func() {
		items := []*fyne.MenuItem{
			fyne.NewMenuItem("Проверить пинг", func() { t.u.act("/api/servers/ping", map[string]string{"id": s.ID}, "", nil) }),
			fyne.NewMenuItem("Копировать ссылку", func() {
				t.u.act("/api/servers/link", map[string]string{"id": s.ID}, "", func(r *api.Result) { t.u.copyText(r.URL, "Скопировано") })
			}),
		}
		if s.Sub == "" {
			items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Удалить", func() {
				t.u.confirm("Удалить сервер", "Удалить сервер «"+s.Title+"»?", "Удалить", func() {
					t.u.act("/api/servers/remove", map[string]string{"id": s.ID}, "", nil)
				})
			}))
		}
		showMenu(t.u.win, more, items...)
	})
	right = append(right, more)
	row := container.NewBorder(nil, nil, container.NewCenter(radio), container.NewHBox(right...), rowText(s.Title, s.Subtitle))
	return newTapBox(row, func() {
		if !s.Active {
			t.u.act("/api/servers/select", map[string]string{"id": s.ID}, "", nil)
		}
	})
}

func (t *vpnTab) subCard(g api.Group) fyne.CanvasObject {
	sub := g.Sub
	var info []string
	used := sub.Upload + sub.Download
	switch {
	case sub.Total > 0:
		info = append(info, fmtBytes(used)+" из "+fmtBytes(sub.Total))
	case used > 0:
		info = append(info, fmtBytes(used)+" · безлимит")
	}
	if sub.Expire > 0 {
		if time.Unix(sub.Expire, 0).Before(time.Now()) {
			info = append(info, "истекла "+fmtDate(sub.Expire))
		} else {
			info = append(info, "до "+fmtDate(sub.Expire))
		}
	}
	if sub.Refreshing {
		info = append(info, "обновляется…")
	} else {
		info = append(info, fmtAgo(sub.UpdatedAt))
	}
	if sub.Unsupported > 0 {
		info = append(info, fmt.Sprintf("пропущено: %d", sub.Unsupported))
	}
	refresh := iconButton(theme.ViewRefreshIcon(), "Обновить", func() {
		t.u.act("/api/subs/refresh", map[string]string{"id": sub.ID}, "", nil)
		t.u.refreshNow()
	})
	if sub.Refreshing {
		refresh.Disable()
	}
	var more *widget.Button
	more = iconButton(theme.MoreVerticalIcon(), "Ещё", func() {
		showMenu(t.u.win, more,
			fyne.NewMenuItem("Переименовать", func() {
				e := widget.NewEntry()
				e.SetText(sub.Title)
				dialog.ShowForm("Название подписки", "Сохранить", "Отмена",
					[]*widget.FormItem{widget.NewFormItem("Название", e)}, func(ok bool) {
						if ok && strings.TrimSpace(e.Text) != "" {
							t.u.act("/api/subs/rename", map[string]string{"id": sub.ID, "name": e.Text}, "", nil)
						}
					}, t.u.win)
			}),
			fyne.NewMenuItem("Копировать адрес подписки", func() {
				t.u.act("/api/subs/link", map[string]string{"id": sub.ID}, "", func(r *api.Result) { t.u.copyText(r.URL, "Скопировано") })
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Удалить подписку", func() {
				t.u.confirm("Удалить подписку", "Удалить подписку «"+sub.Title+"» и все её серверы?", "Удалить", func() {
					t.u.act("/api/subs/remove", map[string]string{"id": sub.ID}, "", nil)
				})
			}))
	})
	headText := rowText(sub.Title, strings.Join(info, " · "))
	parts := []fyne.CanvasObject{container.NewBorder(nil, nil, iconBox("globe", themePal().accent, true),
		container.NewHBox(refresh, more), headText)}
	if sub.Error != "" {
		e := widget.NewLabel(sub.Error)
		e.Wrapping = fyne.TextWrapWord
		e.Importance = widget.DangerImportance
		parts = append(parts, e)
	}
	if sub.Announce != "" {
		a := widget.NewLabel(sub.Announce)
		a.Wrapping = fyne.TextWrapWord
		parts = append(parts, a)
	}
	if len(g.Servers) == 0 {
		parts = append(parts, wrapped("Серверов нет"))
	}
	for _, s := range g.Servers {
		parts = append(parts, widget.NewSeparator(), t.serverRow(s))
	}
	return card(parts...)
}

// showAdd — ссылки vless://, hysteria2:// (по одной на строке) или адрес подписки.
func (t *vpnTab) showAdd() {
	e := widget.NewMultiLineEntry()
	e.SetPlaceHolder("vless://…  hysteria2://…  https://… (подписка)")
	e.SetMinRowsVisible(5)
	e.Wrapping = fyne.TextWrapBreak
	paste := widget.NewButtonWithIcon("Вставить из буфера", theme.ContentPasteIcon(), func() {
		text := strings.TrimSpace(t.u.app.Clipboard().Content())
		if text == "" {
			t.u.notify("Буфер обмена пуст.")
			return
		}
		e.SetText(text)
	})
	content := container.NewVBox(wrapped("Ссылки vless://, hysteria2:// (по одной на строке) или адрес подписки https://…"),
		e, container.NewHBox(paste))
	d := dialog.NewCustomConfirm("Добавить", "Добавить", "Отмена", content, func(ok bool) {
		if !ok || strings.TrimSpace(e.Text) == "" {
			return
		}
		t.u.act("/api/servers/add", map[string]string{"text": e.Text}, "", func(r *api.Result) {
			switch {
			case len(r.Errors) > 0:
				title := "Не добавлено"
				if r.Added > 0 {
					title = "Добавлено не всё"
				}
				t.u.info(title, strings.Join(r.Errors, "\n"))
			case r.Added == 1:
				t.u.notify("Сервер добавлен")
			case r.Added > 1:
				t.u.notify(fmt.Sprintf("Добавлено серверов: %d", r.Added))
			case len(r.Subscriptions) == 0:
				t.u.notify("Ссылок не найдено.")
			}
			if len(r.Subscriptions) > 0 {
				t.u.notify("Загрузка подписки…")
			}
		})
	}, t.u.win)
	d.Resize(fyne.NewSize(560, 0))
	d.Show()
	t.u.win.Canvas().Focus(e)
}
