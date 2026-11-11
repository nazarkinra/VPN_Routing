package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	user32            = windows.NewLazySystemDLL("user32.dll")
	procMessageBox    = user32.NewProc("MessageBoxW")
)

func init() {
	// Программа собрана без консоли (-H windowsgui); для команд в терминале подключаемся к консоли родителя
	// (если вывод уже перенаправлен в файл или канал — оставляем как есть)
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		if t, _ := windows.GetFileType(h); t == windows.FILE_TYPE_DISK || t == windows.FILE_TYPE_PIPE {
			return
		}
	}
	if len(os.Args) > 1 {
		const attachParent = ^uintptr(0) // ATTACH_PARENT_PROCESS
		if r, _, _ := procAttachConsole.Call(attachParent); r != 0 {
			if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
				os.Stdout, os.Stderr = f, f
			}
		}
	}
}

func isPrivileged() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// relaunchPrivileged перезапускает приложение с правами администратора (запрос UAC).
func relaunchPrivileged(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(joinArgs(args))
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_SHOWNORMAL)
}

func joinArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = syscall.EscapeArg(a)
	}
	return strings.Join(q, " ")
}

func spawnDetached(exe string, args []string) error {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	return c.Start()
}

func showError(msg string) {
	title, _ := windows.UTF16PtrFromString("VPN routing")
	text, _ := windows.UTF16PtrFromString(msg)
	const mbIconError = 0x10
	procMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
}

// preflight — для TUN нужен wintun.dll рядом с программой (https://www.wintun.net).
func preflight() error {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "wintun.dll")); err != nil {
		return errors.New("Нет файла wintun.dll рядом с программой. Скачайте Wintun с https://www.wintun.net " +
			"и положите wintun.dll из папки bin\\amd64 рядом с vpnrouting.exe.")
	}
	return nil
}
