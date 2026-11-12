package app

import (
	"sort"
	"strings"
)

// Process — запущенная программа для выбора на вкладке «Напрямую» → «Приложения».
type Process struct {
	// Name — имя для правила (Telegram.exe, firefox): так его видит Xray.
	Name string `json:"name"`
	// Path — путь к программе (для подсказки).
	Path string `json:"path"`
}

// Processes — запущенные программы без повторов и без системных служб, по алфавиту.
func Processes() []Process {
	seen := map[string]bool{}
	var out []Process
	for _, p := range listProcesses() {
		key := strings.ToLower(p.Name)
		if p.Name == "" || seen[key] || hiddenProcess(p) {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
