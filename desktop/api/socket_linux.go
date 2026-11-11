package api

import (
	"context"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"vpnrouting/desktop/install"
)

// SocketPath — сокет управления службы (root): /run/vpnrouting/control.sock.
func SocketPath(string) string { return "/run/vpnrouting/control.sock" }

// dialSocket — подключение к сокету.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}

// CleanupLegacySockets — на Linux нечего убирать.
func CleanupLegacySockets(string) {}

// SocketCandidates — на Linux сокет один.
func SocketCandidates(dataDir string) []string { return []string{SocketPath(dataDir)} }

// Listen создаёт сокет управления. Подключаться к нему может любой процесс (права 0666), но служба сразу
// проверяет, чей он (SO_PEERCRED): root или пользователь из /etc/vpnrouting/ui-users (их добавляет установка).
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		ln.Close()
		return nil, err
	}
	return &peerListener{Listener: ln}, nil
}

type peerListener struct{ net.Listener }

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if allowedPeer(c) {
			return c, nil
		}
		_ = c.Close()
	}
}

func allowedPeer(c net.Conn) bool {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	var cred *unix.Ucred
	_ = raw.Control(func(fd uintptr) {
		cred, _ = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if cred == nil {
		return false
	}
	if cred.Uid == 0 || int(cred.Uid) == os.Geteuid() {
		return true
	}
	// Запуск без установки (sudo ./vpnrouting): окно того, кто запустил sudo
	if su := os.Getenv("SUDO_UID"); su != "" && su == strconv.Itoa(int(cred.Uid)) {
		return true
	}
	for _, name := range install.UIUsers() {
		if u, err := user.Lookup(name); err == nil {
			if uid, _ := strconv.Atoi(u.Uid); uid == int(cred.Uid) {
				return true
			}
		}
	}
	return false
}
