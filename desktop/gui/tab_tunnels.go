package gui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/core"
)

// Вкладка «Туннели» (как TunnelsFragment.java в Android 2.1): основной VPN и дополнительные туннели —
// каждый со своим сервером, программами и доменами. Изменения применяются на ходу.

// tunnelColors — цвета номеров туннелей (номер 1 — основной VPN); после десятого идут по кругу.
var tunnelColors = []color.NRGBA{
	{0x3F, 0x63, 0xF5, 0xFF}, {0x8E, 0x44, 0xD9, 0xFF}, {0xE0, 0x72, 0x1E, 0xFF}, {0x1E, 0x9E, 0xA0, 0xFF},
	{0xD9, 0x3B, 0x8C, 0xFF}, {0x5A, 0x8F, 0x1E, 0xFF}, {0xB5, 0x65, 0x1D, 0xFF}, {0x2E, 0x7B, 0xD6, 0xFF},
	{0xC0, 0x39, 0x2B, 0xFF}, {0x6D, 0x5B, 0xD0, 0xFF},
}

func tunnelColor(number int) color.NRGBA {
	return tunnelColors[max(0, number-1)%len(tunnelColors)]
}

// numberBadge — номер туннеля в цветном круге.
func numberBadge(n int, c color.Color) fyne.CanvasObject {
	circle := canvas.NewCircle(c)
	t := canvas.NewText(fmt.Sprint(n), color.White)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 15
	return container.NewGridWrap(fyne.NewSize(36, 36), container.NewStack(circle, container.NewCenter(t)))
}

// statusLine — строка состояния своего цвета.
func statusLine(text string, c color.Color) fyne.CanvasObject {
	t := canvas.NewText(text, c)
	t.TextSize = 12
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

func pluralDomains(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d домен", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d домена", n)
	}
	return fmt.Sprintf("%d доменов", n)
}

// tunnelState — текст и цвет состояния туннеля (как в Android).
func tunnelState(t api.TunnelView, mainActive bool) (string, color.Color) {
	switch {
	case !t.Enabled:
		return "Выключен", themePal().text2
	case !t.HasServer:
		return "Выберите сервер", colWait
	case len(t.Apps) == 0 && len(t.Domains) == 0:
		return "Добавьте программы или домены", colWait
	case !mainActive:
		return "Ждёт включения основного VPN", themePal().text2
	case !t.Running:
		return "Подключение…", colWait
	case !t.Status.OK:
		return t.Status.Text, colError
	case t.Status.Delay == 0:
		return t.Status.Text, colWait
	}
	return t.Status.Text, colOK
}

type tunnelsTab struct {
	u    *UI
	root fyne.CanvasObject
	list *fyne.Container
	// intro — пояснение создаётся один раз: у новой многострочной подписи высота известна только после
	// первой раскладки, и при пересборке списка она наезжала бы на следующий заголовок
	intro *widget.Label
	sig   string
}

func newTunnelsTab(u *UI) *tunnelsTab {
	t := &tunnelsTab{u: u}
	add := widget.NewButtonWithIcon("Добавить туннель", theme.ContentAddIcon(), t.showAdd)
	add.Importance = widget.HighImportance
	head := canvas.NewText("Туннели", themePal().text)
	head.TextSize = 24
	head.TextStyle = fyne.TextStyle{Bold: true}
	bar := container.NewBorder(nil, nil, head, add)
	t.list = container.NewVBox()
	t.intro = wrapped("Дополнительные туннели работают параллельно основному VPN, каждый через свой " +
		"сервер. Выбранные программы и домены уходят в свой туннель, всё остальное — по обычным правилам основного VPN. " +
		"Туннели работают, пока включён основной VPN.")
	t.root = container.NewBorder(container.NewPadded(bar), nil, nil, nil,
		container.NewVScroll(container.NewPadded(t.list)))
	return t
}

func mainActive(st *api.State) bool {
	return st.VPN.State == "connected" || st.VPN.State == "connecting"
}

func (t *tunnelsTab) update(st *api.State, force bool) {
	sig := signature(st.Tunnels) + "|" + signature(st.VPN.State) + "|" + signature(st.Current) + "|" + st.VPN.Message
	if !force && sig == t.sig {
		return
	}
	t.sig = sig

	objs := []fyne.CanvasObject{t.intro}

	// Основной туннель
	server := "Сервер не выбран"
	if st.Current != nil {
		server = st.Current.Title
	}
	var status string
	var col color.Color
	switch st.VPN.State {
	case "connected":
		status, col = "Подключено · весь остальной трафик", colOK
	case "connecting":
		status, col = "Подключение…", colWait
	case "error":
		status, col = st.VPN.Message, colError
	default:
		status, col = "Выключен — дополнительные туннели тоже не работают", colIdle
	}
	objs = append(objs, sectionHead("ОСНОВНОЙ ТУННЕЛЬ", ""),
		card(container.NewBorder(nil, nil, numberBadge(1, col), nil,
			container.New(&tightVBox{}, title(server), statusLine(status, col)))))

	// Дополнительные
	objs = append(objs, sectionHead("ДОПОЛНИТЕЛЬНЫЕ ТУННЕЛИ", fmt.Sprint(len(st.Tunnels))))
	if len(st.Tunnels) == 0 {
		objs = append(objs, card(wrapped("Туннелей пока нет. Добавьте туннель, выберите для него сервер, "+
			"затем программы и домены, которые должны идти через него.")))
	}
	for _, tv := range st.Tunnels {
		objs = append(objs, t.tunnelCard(tv, mainActive(st)))
	}
	t.list.Objects = objs
	t.list.Refresh()
}

func (t *tunnelsTab) tunnelCard(tv api.TunnelView, mainOn bool) fyne.CanvasObject {
	badgeColor := color.Color(colIdle)
	if tv.Enabled {
		badgeColor = tunnelColor(tv.Number)
	}
	server := "Сервер не выбран"
	if tv.HasServer {
		server = tv.ServerTitle + " · " + tv.ServerSubtitle
		if tv.ServerID == "" {
			server = tv.ServerTitle + " · сервера больше нет в списке"
		}
	}
	var rules []string
	if len(tv.Apps) > 0 {
		rules = append(rules, plural(len(tv.Apps)))
	}
	if len(tv.Domains) > 0 {
		rules = append(rules, pluralDomains(len(tv.Domains)))
	}
	rulesText := "Нет программ и доменов"
	if len(rules) > 0 {
		rulesText = strings.Join(rules, " · ")
	}
	text, col := tunnelState(tv, mainOn)

	id := tv.ID
	enabled := widget.NewCheck("", nil)
	enabled.SetChecked(tv.Enabled)
	enabled.OnChanged = func(on bool) {
		t.u.act("/api/tunnels/set", map[string]any{"id": id, "name": tv.Name, "enabled": on}, "", nil)
	}
	setup := widget.NewButtonWithIcon("Настроить", theme.SettingsIcon(), func() { t.u.showTunnel(id) })
	setup.Importance = widget.LowImportance

	info := container.New(&tightVBox{}, title(tv.Name), subtitle(server), subtitle(rulesText), statusLine(text, col))
	row := container.NewBorder(nil, nil, container.NewCenter(numberBadge(tv.Number, badgeColor)),
		container.NewHBox(container.NewCenter(enabled), container.NewCenter(setup)), info)
	return card(newTapBox(row, func() { t.u.showTunnel(id) }))
}

func (t *tunnelsTab) showAdd() {
	e := widget.NewEntry()
	n := 2
	if st := t.u.state(); st != nil {
		n = len(st.Tunnels) + 2
	}
	e.SetText(fmt.Sprintf("Туннель %d", n))
	content := container.NewVBox(wrapped("Например: «Стриминг», «Работа», «Игры». Сервер, программы и домены "+
		"выберете дальше."), e)
	var d dialog.Dialog
	submit := func(ok bool) {
		if !ok {
			return
		}
		t.u.act("/api/tunnels/add", map[string]string{"name": e.Text}, "", func(r *api.Result) {
			t.u.refreshThen(func() { t.u.showTunnel(r.ID) })
		})
	}
	d = dialog.NewCustomConfirm("Добавить туннель", "Добавить", "Отмена", content, submit, t.u.win)
	e.OnSubmitted = func(string) { d.Hide(); submit(true) }
	d.Resize(fyne.NewSize(460, 0))
	d.Show()
	t.u.win.Canvas().Focus(e)
}

// ---------- Окно туннеля ----------

// findTunnel — туннель из последнего состояния.
func (u *UI) findTunnel(id string) (api.TunnelView, bool) {
	if st := u.state(); st != nil {
		for _, t := range st.Tunnels {
			if t.ID == id {
				return t, true
			}
		}
	}
	return api.TunnelView{}, false
}

// refreshThen — перечитать состояние сразу и затем выполнить f в потоке окна.
func (u *UI) refreshThen(f func()) {
	go func() {
		st, err := u.o.Client.State()
		fyne.Do(func() {
			u.apply(st, err)
			f()
		})
	}()
}

// serverChoices — все серверы (отдельные и из подписок) для выбора сервера туннеля.
func serverChoices(st *api.State) (labels []string, ids []string) {
	seen := map[string]int{}
	for _, g := range st.Groups {
		for _, s := range g.Servers {
			label := s.Title
			if s.Active {
				label += " (основной)"
			}
			if g.Sub != nil {
				label += " — " + g.Sub.Title
			}
			seen[label]++
			if seen[label] > 1 {
				label = fmt.Sprintf("%s (%d)", label, seen[label])
			}
			labels = append(labels, label)
			ids = append(ids, s.ID)
		}
	}
	return
}

// showTunnel — настройка туннеля: включение, сервер, программы, домены, переименование и удаление.
func (u *UI) showTunnel(id string) {
	content := container.NewVBox()
	// Отступ справа — под полосу прокрутки, иначе она закрывает край карточек
	scroll := container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 0, 0, 12), content))
	scroll.SetMinSize(fyne.NewSize(600, 520))
	var d dialog.Dialog

	var rebuild func()
	// do — команда туннеля; после неё состояние перечитывается и окно туннеля перерисовывается
	do := func(path string, body any, ok string) {
		u.act(path, body, ok, func(*api.Result) { u.refreshThen(rebuild) })
	}

	rebuild = func() {
		tv, found := u.findTunnel(id)
		st := u.state()
		if !found || st == nil {
			if d != nil {
				d.Hide()
			}
			return
		}
		set := func(name, serverID string, enabled bool) {
			do("/api/tunnels/set", map[string]any{"id": id, "name": name, "serverId": serverID, "enabled": enabled}, "")
		}

		// Название и включение
		name := widget.NewEntry()
		name.SetText(tv.Name)
		rename := widget.NewButton("Переименовать", func() {
			if strings.TrimSpace(name.Text) != "" && name.Text != tv.Name {
				set(name.Text, "", tv.Enabled)
			}
		})
		name.OnSubmitted = func(string) { rename.OnTapped() }
		// Обработчик — после SetChecked: иначе каждая перерисовка отправляла бы команду
		enabled := widget.NewCheck("Туннель включён", nil)
		enabled.SetChecked(tv.Enabled)
		enabled.OnChanged = func(on bool) { set(tv.Name, "", on) }
		stText, stCol := tunnelState(tv, mainActive(st))
		objs := []fyne.CanvasObject{
			container.NewBorder(nil, nil, numberBadge(tv.Number, tunnelColor(tv.Number)), rename, name),
			card(enabled, statusLine(stText, stCol)),
		}

		// Сервер
		labels, ids := serverChoices(st)
		sel := widget.NewSelect(labels, nil)
		sel.PlaceHolder = "Выберите сервер из списка на вкладке VPN"
		for i, sid := range ids {
			if sid == tv.ServerID {
				sel.SetSelectedIndex(i)
			}
		}
		sel.OnChanged = func(string) {
			if i := sel.SelectedIndex(); i >= 0 && ids[i] != tv.ServerID {
				set(tv.Name, ids[i], tv.Enabled)
			}
		}
		serverObjs := []fyne.CanvasObject{sel}
		if tv.HasServer && tv.ServerID == "" {
			serverObjs = append(serverObjs, wrapped("Сейчас выбран "+tv.ServerTitle+": его больше нет в списке серверов."))
		}
		if len(labels) == 0 {
			serverObjs = []fyne.CanvasObject{wrapped("Список серверов пуст: добавьте серверы на вкладке VPN.")}
		}
		objs = append(objs, sectionHead("СЕРВЕР ТУННЕЛЯ", ""), card(serverObjs...))

		// Программы
		pick := widget.NewButtonWithIcon("Выбрать программы", icon("apps"), func() { u.showTunnelApps(id, rebuild) })
		appRows := []fyne.CanvasObject{
			wrapped("Весь трафик этих программ идёт через туннель (кроме сайтов из «Напрямую» и локальной сети)."),
			container.NewHBox(pick),
		}
		for _, a := range tv.Apps {
			app := a
			del := iconButton(theme.DeleteIcon(), "Убрать", func() {
				var rest []string
				for _, x := range tv.Apps {
					if x != app {
						rest = append(rest, x)
					}
				}
				do("/api/tunnels/apps", map[string]any{"id": id, "apps": append([]string{}, rest...)}, "")
			})
			appRows = append(appRows, widget.NewSeparator(),
				container.NewBorder(nil, nil, iconBox("apps", tunnelColor(tv.Number), true), del, title(app)))
		}
		objs = append(objs, sectionHead("ПРОГРАММЫ", fmt.Sprint(len(tv.Apps))), card(appRows...))

		// Домены и подсети
		domain := widget.NewEntry()
		domain.SetPlaceHolder("example.com, адрес страницы или IP-подсеть")
		addDomain := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), func() {
			if strings.TrimSpace(domain.Text) == "" {
				return
			}
			go func() {
				r, err := u.o.Client.Do("/api/tunnels/domain/add", map[string]string{"id": id, "value": domain.Text})
				fyne.Do(func() {
					if err != nil {
						u.info("Не добавлено", err.Error())
						return
					}
					u.notify(r.Message)
					u.refreshThen(rebuild)
				})
			}()
		})
		domain.OnSubmitted = func(string) { addDomain.OnTapped() }
		domRows := []fyne.CanvasObject{
			wrapped("Сайт и все его поддомены идут через туннель из любых программ."),
			container.NewBorder(nil, nil, nil, addDomain, domain),
		}
		for _, v := range tv.Domains {
			value := v
			sub, ic := "сайт и все поддомены", "globe"
			if core.IsCIDR(value) {
				sub, ic = "IP-подсеть", "dns"
			}
			del := iconButton(theme.DeleteIcon(), "Удалить", func() {
				do("/api/tunnels/domain/remove", map[string]string{"id": id, "value": value}, "")
			})
			domRows = append(domRows, widget.NewSeparator(),
				container.NewBorder(nil, nil, iconBox(ic, themePal().accent, false), del,
					rowText(core.DisplayDomain(value), sub)))
		}
		objs = append(objs, sectionHead("ДОМЕНЫ И СЕТИ", fmt.Sprint(len(tv.Domains))), card(domRows...))

		remove := widget.NewButtonWithIcon("Удалить туннель", theme.DeleteIcon(), func() {
			u.confirm("Удалить туннель", "Удалить туннель «"+tv.Name+"»? Его программы и домены пойдут через основной VPN.",
				"Удалить", func() {
					d.Hide()
					u.act("/api/tunnels/remove", map[string]string{"id": id}, "Туннель удалён", nil)
				})
		})
		remove.Importance = widget.DangerImportance
		objs = append(objs,
			wrapped("Домены туннеля важнее правил «Напрямую»: так через туннель можно пустить и сайт в зоне .ru. "+
				"Если домен есть в нескольких туннелях, срабатывает туннель с меньшим номером. "+
				"Программа может быть только в одном туннеле."),
			container.NewHBox(remove))

		content.Objects = objs
		content.Refresh()
	}

	tv, ok := u.findTunnel(id)
	if !ok {
		return
	}
	d = dialog.NewCustom("Туннель "+fmt.Sprint(tv.Number), "Закрыть", scroll, u.win)
	rebuild()
	d.Resize(fyne.NewSize(680, 660))
	d.Show()
}

// showTunnelApps — выбор программ туннеля: запущенные программы, отмеченные и добавление по имени.
// Программа может быть только в одном туннеле: у занятых другим видно, в каком.
func (u *UI) showTunnelApps(id string, after func()) {
	tv, ok := u.findTunnel(id)
	st := u.state()
	if !ok || st == nil {
		return
	}
	elsewhere := map[string]string{}
	for _, other := range st.Tunnels {
		if other.ID != id {
			for _, a := range other.Apps {
				elsewhere[strings.ToLower(a)] = other.Name
			}
		}
	}
	chosen := map[string]bool{}
	order := []string{}
	for _, a := range tv.Apps {
		chosen[strings.ToLower(a)] = true
		order = append(order, a)
	}
	toggle := func(name string, on bool) {
		key := strings.ToLower(name)
		if on == chosen[key] {
			return
		}
		if on {
			chosen[key] = true
			order = append(order, name)
			return
		}
		delete(chosen, key)
		kept := []string{}
		for _, n := range order {
			if strings.ToLower(n) != key {
				kept = append(kept, n)
			}
		}
		order = kept
	}

	var running []struct{ Name, Path string }
	search := widget.NewEntry()
	search.SetPlaceHolder("Поиск программы")
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	manual := widget.NewEntry()
	manual.SetPlaceHolder("Имя программы, например Telegram.exe или firefox")
	list := container.NewVBox()
	status := wrapped("Загрузка списка программ…")

	rebuild := func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		var objs []fyne.CanvasObject
		row := func(name, path string) {
			if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(path), q) {
				return
			}
			n := name
			c := widget.NewCheck("", func(on bool) { toggle(n, on) })
			c.SetChecked(chosen[strings.ToLower(name)])
			sub := path
			if other, ok := elsewhere[strings.ToLower(name)]; ok && !chosen[strings.ToLower(name)] {
				sub = "сейчас в туннеле «" + other + "»"
			} else if sub == "" {
				sub = "не запущена сейчас"
			}
			if len(objs) > 0 {
				objs = append(objs, widget.NewSeparator())
			}
			objs = append(objs, container.NewBorder(nil, nil, c, nil, rowText(name, sub)))
		}
		shown := map[string]bool{}
		paths := map[string]string{}
		for _, p := range running {
			paths[strings.ToLower(p.Name)] = p.Path
		}
		for _, n := range order {
			shown[strings.ToLower(n)] = true
			row(n, paths[strings.ToLower(n)])
		}
		for _, p := range running {
			if !shown[strings.ToLower(p.Name)] {
				row(p.Name, p.Path)
			}
		}
		if len(objs) == 0 {
			objs = append(objs, wrapped("Ничего не найдено. Программу можно добавить по имени ниже."))
		}
		list.Objects = objs
		list.Refresh()
	}
	search.OnChanged = func(string) { rebuild() }
	add := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), func() {
		if name := strings.TrimSpace(manual.Text); name != "" {
			toggle(name, true)
			manual.SetText("")
			rebuild()
		}
	})
	manual.OnSubmitted = func(string) { add.OnTapped() }

	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(0, 320))
	hint := wrapped("Отмеченные программы целиком идут через туннель «" + tv.Name + "». Сайты из «Напрямую» и " +
		"локальная сеть у них по-прежнему идут напрямую. Программа может быть только в одном туннеле: отметка здесь " +
		"убирает её из другого.")
	content := container.NewBorder(container.NewVBox(hint, search),
		container.NewVBox(widget.NewSeparator(), status, container.NewBorder(nil, nil, nil, add, manual)),
		nil, nil, card(scroll))
	d := dialog.NewCustomConfirm("Программы: "+tv.Name, "Сохранить", "Отмена", content, func(ok bool) {
		if !ok {
			return
		}
		u.act("/api/tunnels/apps", map[string]any{"id": id, "apps": append([]string{}, order...)},
			"Программы туннеля сохранены", func(*api.Result) {
				if after != nil {
					u.refreshThen(after)
				}
			})
	}, u.win)
	d.Resize(fyne.NewSize(640, 640))
	rebuild()
	d.Show()

	go func() {
		var r api.Result
		err := u.o.Client.Call("/api/apps/processes", nil, &r)
		fyne.Do(func() {
			if err != nil {
				status.SetText("Не удалось получить список программ: " + err.Error())
				return
			}
			running = running[:0]
			for _, p := range r.Processes {
				running = append(running, struct{ Name, Path string }{p.Name, p.Path})
			}
			status.SetText("Запущенные программы. Нужной нет — добавьте по имени файла.")
			rebuild()
		})
	}()
}
