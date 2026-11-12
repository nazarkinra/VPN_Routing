package main

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/Xresource.h>
#include <stdlib.h>
#include <string.h>

// Xft.dpi из ресурсов X-сервера (RESOURCE_MANAGER): так масштаб узнают GTK, Qt и сама GLFW.
// GNOME и KDE пишут туда масштаб интерфейса и размер шрифта (96 — 100%, 120 — 125%). 0 — не задан.
static double vpnr_xft_dpi(void) {
	Display *d = XOpenDisplay(NULL);
	if (!d) return 0;
	double dpi = 0;
	char *rms = XResourceManagerString(d);
	if (rms) {
		XrmInitialize();
		XrmDatabase db = XrmGetStringDatabase(rms);
		if (db) {
			XrmValue value;
			char *type = NULL;
			if (XrmGetResource(db, "Xft.dpi", "Xft.Dpi", &type, &value) && type && strcmp(type, "String") == 0)
				dpi = atof(value.addr);
			XrmDestroyDatabase(db);
		}
	}
	XCloseDisplay(d);
	return dpi;
}
*/
import "C"

import (
	"os"
	"strconv"
)

// applyDesktopScale — масштаб окна, как у остальных программ. Fyne на Linux сам считает масштаб по
// физическому DPI экрана и не видит системную настройку («Масштаб» 125% в GNOME): на ноутбуке с плотным
// экраном окно получалось почти вдвое крупнее соседних. Здесь масштаб берётся из Xft.dpi, а определение
// по DPI отключается. FYNE_SCALE, заданный вручную, не трогается.
func applyDesktopScale() {
	if os.Getenv("FYNE_SCALE") != "" {
		return
	}
	scale := 0.0
	if dpi := float64(C.vpnr_xft_dpi()); dpi >= 48 && dpi <= 480 {
		scale = dpi / 96
	} else if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		// Wayland без Xft.dpi: окно X11 (Xwayland) масштабирует сам композитор
		scale = 1
	}
	if scale == 0 {
		return
	}
	_ = os.Setenv("FYNE_DISABLE_DPI_DETECTION", "1")
	_ = os.Setenv("FYNE_SCALE", strconv.FormatFloat(scale, 'f', 2, 64))
}
