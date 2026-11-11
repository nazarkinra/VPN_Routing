package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func isPrivileged() bool { return os.Geteuid() == 0 }

func relaunchPrivileged(_ []string) error {
	return errors.New("запустите приложение через sudo: sudo vpnrouting")
}

func spawnDetached(exe string, args []string) error {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

func showError(msg string) { fmt.Fprintln(os.Stderr, msg) }

// preflight — TUN на Linux создаётся через /dev/net/tun.
func preflight() error {
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		return errors.New("Нет устройства /dev/net/tun: загрузите модуль tun (sudo modprobe tun).")
	}
	return nil
}
