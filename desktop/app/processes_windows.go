package app

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func listProcesses() []Process {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Process
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		path := processPath(e.ProcessID)
		name := windows.UTF16ToString(e.ExeFile[:])
		// Имя — как его видит Xray: из полного пути (регистр букв может отличаться от снимка)
		if path != "" {
			name = filepath.Base(path)
		}
		out = append(out, Process{Name: name, Path: path})
	}
	return out
}

func processPath(pid uint32) string {
	if pid == 0 || pid == 4 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// hiddenProcess — службы и компоненты Windows (папка Windows) и сам VPN routing.
func hiddenProcess(p Process) bool {
	if p.Path == "" {
		return true
	}
	sys, err := windows.GetSystemDirectory() // C:\Windows\system32
	if err != nil {
		sys = `C:\Windows\system32`
	}
	win := strings.ToLower(sys)
	if i := strings.LastIndex(win, `\`); i > 0 {
		win = win[:i]
	}
	lp := strings.ToLower(p.Path)
	return strings.HasPrefix(lp, win+`\`) || strings.EqualFold(p.Name, "vpnrouting.exe")
}
