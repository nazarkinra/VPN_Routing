package gui

import (
	"fmt"
	"image/color"
	"strings"
	"time"
)

// Форматирование чисел, времени и объёмов (Format.java Android-версии).

func fmtBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d Б", b)
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	v := float64(b)
	i := -1
	for {
		v /= 1024
		i++
		if v < 1024 || i == len(units)-1 {
			break
		}
	}
	s := fmt.Sprintf("%.1f", v)
	if v >= 100 {
		s = fmt.Sprintf("%.0f", v)
	}
	return strings.Replace(s, ".", ",", 1) + " " + units[i]
}

func fmtDuration(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func fmtDate(unixSec int64) string { return time.Unix(unixSec, 0).Format("02.01.2006") }

func fmtAgo(ms int64) string {
	if ms <= 0 {
		return "не обновлялась"
	}
	diff := time.Since(time.UnixMilli(ms))
	switch {
	case diff < time.Minute:
		return "обновлено только что"
	case diff < time.Hour:
		return fmt.Sprintf("обновлено %d мин назад", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("обновлено %d ч назад", int(diff.Hours()))
	}
	return "обновлено " + fmtDate(ms/1000)
}

// pingColor — зелёный до 300 мс, оранжевый до 800, красный — медленно или нет ответа.
func pingColor(ms int64) color.Color {
	switch {
	case ms < 0:
		return colError
	case ms < 300:
		return colOK
	case ms < 800:
		return colWait
	}
	return colError
}
