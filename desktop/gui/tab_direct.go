package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
)

// Вкладка «Напрямую»: что идёт без VPN, свои сайты, встроенные правила (DirectFragment.java).
type directTab struct {
	u       *UI
	root    fyne.CanvasObject
	search  *widget.Entry
	restore *widget.Button
	list    *fyne.Container
	sig     string
}

func newDirectTab(u *UI) *directTab {
	t := &directTab{u: u}
	t.search = widget.NewEntry()
	t.search.SetPlaceHolder("Домен или сайт")
	t.search.ActionItem = widget.NewIcon(theme.SearchIcon())
	t.search.OnChanged = func(string) { u.render(true) }
	t.restore = widget.NewButtonWithIcon("Вернуть встроенные", theme.ViewRefreshIcon(), func() {
		u.act("/api/direct/restore-builtins", struct{}{}, "", nil)
	})
	t.restore.Hide()
	add := widget.NewButtonWithIcon("Добавить сайт", theme.ContentAddIcon(), t.showAdd)
	add.Importance = widget.HighImportance

	head := canvas.NewText("Напрямую", themePal().text)
	head.TextSize = 24
	head.TextStyle = fyne.TextStyle{Bold: true}
	bar := container.NewBorder(nil, nil, head, container.NewHBox(t.restore, add),
		container.NewPadded(t.search))
	t.list = container.NewVBox()
	t.root = container.NewBorder(container.NewPadded(bar), nil, nil, nil,
		container.NewVScroll(container.NewPadded(t.list)))
	return t
}

func (t *directTab) update(st *api.State, force bool) {
	d := st.Direct
	if d.Disabled > 0 {
		t.restore.SetText(fmt.Sprintf("Вернуть встроенные (%d)", d.Disabled))
		t.restore.Show()
	} else {
		t.restore.Hide()
	}
	q := strings.ToLower(strings.TrimSpace(t.search.Text))
	sig := signature(d) + "|" + q
	if !force && sig == t.sig {
		return
	}
	t.sig = sig

	match := func(e api.Entry) bool {
		return q == "" || strings.Contains(strings.ToLower(e.Title), q) || strings.Contains(strings.ToLower(e.Subtitle), q)
	}
	var objs []fyne.CanvasObject
	if q == "" {
		objs = append(objs, wrapped("Эти адреса открываются без VPN, с вашего обычного IP. Остальной трафик идёт через выбранный сервер."))
	}
	section := func(name string, items []api.Entry, tint string, emptyText string) {
		var rows []fyne.CanvasObject
		for _, e := range items {
			if match(e) {
				if len(rows) > 0 {
					rows = append(rows, widget.NewSeparator())
				}
				rows = append(rows, t.entryRow(e, tint))
			}
		}
		if len(rows) == 0 && emptyText == "" {
			return
		}
		count := ""
		if len(rows) > 0 {
			count = fmt.Sprint((len(rows) + 1) / 2)
		}
		objs = append(objs, sectionHead(name, count))
		if len(rows) == 0 {
			objs = append(objs, card(wrapped(emptyText)))
		} else {
			objs = append(objs, card(rows...))
		}
	}
	empty := ""
	if q == "" {
		empty = "Пока пусто. Добавьте сайт, если он не работает через VPN: банк, госуслуги, портал вуза."
	}
	section("ДОБАВЛЕННЫЕ ВАМИ", d.User, "accent", empty)
	section("ЧЕРЕЗ VPN МГТУ", d.Bmstu, "corp", "")
	section("ДОМЕННЫЕ ЗОНЫ", d.Zones, "", "")
	section("САЙТЫ И СЕРВИСЫ", d.Sites, "", "")
	section("СЕТИ И ПРОТОКОЛЫ", d.Nets, "", "")
	if len(objs) == 0 {
		objs = append(objs, card(wrapped("Ничего не найдено")))
	}
	t.list.Objects = objs
	t.list.Refresh()
}

func (t *directTab) entryRow(e api.Entry, tint string) fyne.CanvasObject {
	var box fyne.CanvasObject
	switch tint {
	case "accent":
		box = iconBox(e.Icon, themePal().accent, true)
	case "corp":
		box = iconBox(e.Icon, colCorp, true)
	default:
		box = iconBox(e.Icon, themePal().accent, false)
	}
	var action fyne.CanvasObject
	switch {
	case e.Value != "" && !e.Builtin:
		action = iconButton(theme.DeleteIcon(), "Удалить", func() {
			t.u.confirm("Убрать из списка", "Убрать "+e.Title+" из списка? Сайт снова пойдёт через VPN.", "Удалить", func() {
				t.u.act("/api/direct/remove", map[string]string{"value": e.Value}, "", nil)
			})
		})
	case e.Builtin:
		// Встроенные правила тоже можно убрать; вернуть — кнопкой «Вернуть встроенные»
		action = iconButton(theme.DeleteIcon(), "Убрать встроенное правило", func() {
			t.u.confirm("Убрать "+e.Title+"?", "Это встроенное правило. Сайты из него пойдут через VPN. "+
				"Вернуть его можно кнопкой «Вернуть встроенные».", "Удалить", func() {
				t.u.act("/api/direct/disable-builtin", map[string]string{"value": e.Value}, "", nil)
			})
		})
	default:
		lock := canvas.NewImageFromResource(icon("lock"))
		lock.FillMode = canvas.ImageFillContain
		action = container.NewGridWrap(fyne.NewSize(18, 18), lock)
	}
	return container.NewBorder(nil, nil, box, container.NewCenter(action), rowText(e.Title, e.Subtitle))
}

func (t *directTab) showAdd() {
	e := widget.NewEntry()
	e.SetPlaceHolder("example.com, адрес страницы или IP")
	content := container.NewVBox(wrapped("Сайт и все его поддомены будут открываться без VPN. "+
		"Можно вставить адрес страницы целиком или IP-подсеть."), e)
	var d dialog.Dialog
	submit := func(ok bool) {
		if !ok || strings.TrimSpace(e.Text) == "" {
			return
		}
		go func() {
			r, err := t.u.o.Client.Do("/api/direct/add", map[string]string{"value": e.Text})
			fyne.Do(func() {
				if err != nil {
					t.u.info("Не удалось добавить", err.Error())
					return
				}
				t.u.notify(r.Message)
				t.u.refreshNow()
			})
		}()
	}
	d = dialog.NewCustomConfirm("Добавить сайт", "Добавить", "Отмена", content, submit, t.u.win)
	e.OnSubmitted = func(string) { d.Hide(); submit(true) }
	d.Resize(fyne.NewSize(480, 0))
	d.Show()
	t.u.win.Canvas().Focus(e)
	_ = layout.NewSpacer
}
