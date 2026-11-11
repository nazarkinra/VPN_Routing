package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Палитра — как в Android-версии (values/colors.xml и values-night/colors.xml).
var (
	colOK    = color.NRGBA{0x1E, 0x9E, 0x63, 0xFF}
	colWait  = color.NRGBA{0xE0, 0x8A, 0x1E, 0xFF}
	colError = color.NRGBA{0xD9, 0x3B, 0x41, 0xFF}
	colIdle  = color.NRGBA{0x8A, 0x93, 0xA6, 0xFF}
	colCorp  = color.NRGBA{0x0A, 0x6C, 0xC2, 0xFF}
)

type palette struct {
	bg, surface, surfaceVariant, outline, text, text2, accent color.NRGBA
}

var light = palette{
	bg:             color.NRGBA{0xF3, 0xF5, 0xF9, 0xFF},
	surface:        color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},
	surfaceVariant: color.NRGBA{0xE7, 0xEB, 0xF2, 0xFF},
	outline:        color.NRGBA{0xD5, 0xDB, 0xE5, 0xFF},
	text:           color.NRGBA{0x14, 0x19, 0x22, 0xFF},
	text2:          color.NRGBA{0x68, 0x71, 0x84, 0xFF},
	accent:         color.NRGBA{0x3F, 0x63, 0xF5, 0xFF},
}

var dark = palette{
	bg:             color.NRGBA{0x0F, 0x12, 0x18, 0xFF},
	surface:        color.NRGBA{0x18, 0x1C, 0x24, 0xFF},
	surfaceVariant: color.NRGBA{0x23, 0x29, 0x37, 0xFF},
	outline:        color.NRGBA{0x2E, 0x35, 0x45, 0xFF},
	text:           color.NRGBA{0xE8, 0xEC, 0xF3, 0xFF},
	text2:          color.NRGBA{0x9A, 0xA3, 0xB5, 0xFF},
	accent:         color.NRGBA{0x7D, 0x97, 0xFF, 0xFF},
}

// appTheme — светлая или тёмная по настройке системы.
type appTheme struct{}

func (appTheme) pal(v fyne.ThemeVariant) palette {
	if v == theme.VariantDark {
		return dark
	}
	return light
}

func (t appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := t.pal(v)
	switch n {
	case theme.ColorNameBackground:
		return p.bg
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameMenuBackground,
		theme.ColorNameOverlayBackground, theme.ColorNameHeaderBackground:
		return p.surface
	case theme.ColorNameForeground:
		return p.text
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return p.text2
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return p.accent
	case theme.ColorNameForegroundOnPrimary:
		if v == theme.VariantDark {
			return p.bg
		}
		return color.White
	case theme.ColorNameSelection:
		return withAlpha(p.accent, 0x30)
	case theme.ColorNameHover:
		return withAlpha(p.text, 0x10)
	case theme.ColorNamePressed:
		return withAlpha(p.text, 0x20)
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return p.outline
	case theme.ColorNameDisabledButton:
		return p.surfaceVariant
	case theme.ColorNameSuccess:
		return colOK
	case theme.ColorNameWarning:
		return colWait
	case theme.ColorNameError:
		return colError
	}
	return theme.DefaultTheme().Color(n, v)
}

func (appTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (appTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (appTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameInnerPadding:
		return 8
	}
	return theme.DefaultTheme().Size(n)
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// themePal — палитра текущей темы (для своих элементов: карточек, подписей).
func themePal() palette {
	return appTheme{}.pal(fyne.CurrentApp().Settings().ThemeVariant())
}
