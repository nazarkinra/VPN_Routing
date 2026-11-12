package app

import (
	"os"
	"path/filepath"
	"strings"
)

// listProcesses — /proc/*/exe: имя файла программы, как его видит Xray (служба работает от root).
func listProcesses() []Process {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []Process
	for _, e := range entries {
		if !e.IsDir() || strings.TrimLeft(e.Name(), "0123456789") != "" {
			continue
		}
		path, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil || path == "" {
			continue // потоки ядра и чужие процессы без доступа
		}
		path = strings.TrimSuffix(path, " (deleted)")
		out = append(out, Process{Name: filepath.Base(path), Path: path})
	}
	return out
}

// hiddenProcess — системные службы и сам VPN routing.
func hiddenProcess(p Process) bool {
	for _, prefix := range []string{"/usr/lib/systemd/", "/lib/systemd/", "/usr/libexec/", "/usr/sbin/", "/sbin/",
		"/usr/lib/xorg/", "/usr/bin/gnome-", "/usr/bin/dbus-", "/usr/bin/pipewire", "/usr/bin/wireplumber"} {
		if strings.HasPrefix(p.Path, prefix) {
			return true
		}
	}
	return p.Name == "vpnrouting"
}
