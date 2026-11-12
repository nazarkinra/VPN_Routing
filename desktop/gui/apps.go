package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"vpnrouting/desktop/api"
	"vpnrouting/desktop/app"
	"vpnrouting/desktop/core"
)

// Маршрутизация по приложениям (как в Happ): все программы по правилам, только выбранные по правилам
// (остальные напрямую) или все, кроме выбранных (выбранные напрямую).

var appModes = []string{"Все приложения", "Только выбранные", "Кроме выбранных"}

func appModeIndex(mode string) int {
	switch mode {
	case core.AppsInclude:
		return 1
	case core.AppsExclude:
		return 2
	}
	return 0
}

func appModeValue(i int) string {
	switch i {
	case 1:
		return core.AppsInclude
	case 2:
		return core.AppsExclude
	}
	return core.AppsAll
}

// appsSummary — строка для карточки на вкладке «Напрямую».
func appsSummary(a app.Apps) (string, string) {
	switch a.Mode {
	case core.AppsInclude:
		if len(a.Include) == 0 {
			return "Только выбранные", "Приложения не выбраны — пока по правилам идут все"
		}
		return "Только выбранные", fmt.Sprintf("%s по правилам VPN, остальные — напрямую", plural(len(a.Include)))
	case core.AppsExclude:
		if len(a.Exclude) == 0 {
			return "Кроме выбранных", "Приложения не выбраны — по правилам идут все"
		}
		return "Кроме выбранных", fmt.Sprintf("%s напрямую, остальные — по правилам VPN", plural(len(a.Exclude)))
	}
	return "Все приложения", "Все программы идут по правилам VPN"
}

func plural(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d приложение", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d приложения", n)
	}
	return fmt.Sprintf("%d приложений", n)
}

func modeHint(mode string) string {
	switch mode {
	case core.AppsInclude:
		return "Отмеченные программы идут по правилам VPN (сайты из «Напрямую» — напрямую, остальное — через сервер). " +
			"Все остальные программы — напрямую, без VPN."
	case core.AppsExclude:
		return "Отмеченные программы идут напрямую, без VPN (и мимо VPN МГТУ). Все остальные — по правилам VPN."
	}
	return "Все программы идут по правилам VPN. Выберите режим, чтобы отметить программы."
}

// showApps — окно выбора программ: режим, поиск, запущенные программы и добавление по имени.
func (u *UI) showApps() {
	st := u.state()
	if st == nil {
		return
	}
	cur := st.Apps
	mode := cur.Mode
	lists := map[string]map[string]bool{core.AppsInclude: {}, core.AppsExclude: {}}
	order := map[string][]string{}
	for _, n := range cur.Include {
		lists[core.AppsInclude][strings.ToLower(n)] = true
		order[core.AppsInclude] = append(order[core.AppsInclude], n)
	}
	for _, n := range cur.Exclude {
		lists[core.AppsExclude][strings.ToLower(n)] = true
		order[core.AppsExclude] = append(order[core.AppsExclude], n)
	}
	var running []app.Process

	hint := wrapped(modeHint(mode))
	search := widget.NewEntry()
	search.SetPlaceHolder("Поиск программы")
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	manual := widget.NewEntry()
	manual.SetPlaceHolder("Имя программы, например Telegram.exe или firefox")
	list := container.NewVBox()
	status := wrapped("Загрузка списка программ…")

	selected := func() []string { return order[mode] }
	isSelected := func(name string) bool {
		if mode == core.AppsAll {
			return false
		}
		return lists[mode][strings.ToLower(name)]
	}
	setSelected := func(name string, on bool) {
		if mode == core.AppsAll {
			return
		}
		key := strings.ToLower(name)
		if on == lists[mode][key] {
			return
		}
		if on {
			lists[mode][key] = true
			order[mode] = append(order[mode], name)
			return
		}
		delete(lists[mode], key)
		kept := order[mode][:0:0]
		for _, n := range order[mode] {
			if strings.ToLower(n) != key {
				kept = append(kept, n)
			}
		}
		order[mode] = kept
	}

	var rebuild func()
	rebuild = func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		var objs []fyne.CanvasObject
		row := func(name, path string) {
			if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(path), q) {
				return
			}
			n := name
			c := widget.NewCheck("", func(on bool) { setSelected(n, on) })
			c.SetChecked(isSelected(name))
			if mode == core.AppsAll {
				c.Disable()
			}
			sub := path
			if sub == "" {
				sub = "не запущена сейчас"
			}
			if len(objs) > 0 {
				objs = append(objs, widget.NewSeparator())
			}
			objs = append(objs, container.NewBorder(nil, nil, c, nil, rowText(name, sub)))
		}
		shown := map[string]bool{}
		// Сначала отмеченные (в том числе не запущенные сейчас), затем остальные запущенные
		if mode != core.AppsAll {
			paths := map[string]string{}
			for _, p := range running {
				paths[strings.ToLower(p.Name)] = p.Path
			}
			for _, n := range selected() {
				shown[strings.ToLower(n)] = true
				row(n, paths[strings.ToLower(n)])
			}
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

	modes := widget.NewRadioGroup(appModes, nil)
	modes.Horizontal = true
	modes.SetSelected(appModes[appModeIndex(mode)])
	modes.OnChanged = func(s string) {
		for i, m := range appModes {
			if m == s {
				mode = appModeValue(i)
			}
		}
		hint.SetText(modeHint(mode))
		rebuild()
	}
	search.OnChanged = func(string) { rebuild() }
	add := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), func() {
		name := strings.TrimSpace(manual.Text)
		if name == "" {
			return
		}
		if mode == core.AppsAll {
			mode = core.AppsExclude
			modes.SetSelected(appModes[2])
		}
		setSelected(name, true)
		manual.SetText("")
		rebuild()
	})
	manual.OnSubmitted = func(string) { add.OnTapped() }

	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(0, 320))
	content := container.NewBorder(
		container.NewVBox(modes, hint, search),
		container.NewVBox(widget.NewSeparator(), status,
			container.NewBorder(nil, nil, nil, add, manual)),
		nil, nil, card(scroll))

	d := dialog.NewCustomConfirm("Приложения", "Сохранить", "Отмена", content, func(ok bool) {
		if !ok {
			return
		}
		a := app.Apps{Mode: mode, Include: order[core.AppsInclude], Exclude: order[core.AppsExclude]}
		u.act("/api/apps/set", a, "Маршрутизация по приложениям сохранена", nil)
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
			running = r.Processes
			status.SetText("Запущенные программы. Нужной нет — добавьте по имени файла.")
			rebuild()
		})
	}()
}
