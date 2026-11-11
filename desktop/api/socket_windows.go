package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"vpnrouting/desktop/install"
)

// Канал управления на Windows — именованный канал (named pipe) \\.\pipe\vpnrouting-<SID>-<…>.
//
// Канал живёт в памяти системы: на диске ничего не создаётся, и после падения или принудительного
// завершения программы оставаться нечему (с сокет-файлами AF_UNIX так не выходило: файл, созданный
// процессом с правами администратора, обычный процесс не может ни открыть, ни удалить).
//
// Права канала: система, администраторы и эта учётная запись. Метка среднего уровня целостности: само
// приложение работает с правами администратора, а команды vpnrouting и повторный запуск ярлыка — обычные
// процессы того же пользователя, и без метки они к каналу не подключились бы.

func userSID() string {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "unknown"
	}
	return tu.User.Sid.String()
}

// ServicePipe — канал службы «VPN routing» (установленная программа).
const ServicePipe = `\\.\pipe\vpnrouting`

// ServiceMode — эта программа работает службой: слушает ServicePipe.
var ServiceMode bool

// userPipe — канал переносной версии (без службы), своя у каждого пользователя и папки настроек.
func userPipe(dataDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dataDir))))
	return `\\.\pipe\vpnrouting-` + userSID() + "-" + hex.EncodeToString(sum[:4])
}

// SocketPath — канал, который слушает эта программа.
func SocketPath(dataDir string) string {
	if ServiceMode {
		return ServicePipe
	}
	return userPipe(dataDir)
}

// SocketCandidates — куда подключаться: служба, затем переносная версия этого пользователя.
func SocketCandidates(dataDir string) []string { return []string{ServicePipe, userPipe(dataDir)} }

// Listen создаёт канал управления.
func Listen(path string) (net.Listener, error) {
	// Служба: система, администраторы и пользователи, установившие программу (install.UIUserSIDs).
	// Переносная версия: только её пользователь.
	allow := "(A;;GA;;;SY)(A;;GA;;;BA)"
	if path == ServicePipe {
		for _, sid := range install.UIUserSIDs() {
			allow += "(A;;GRGW;;;" + sid + ")"
		}
	} else {
		allow += "(A;;GA;;;" + userSID() + ")"
	}
	return winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: "D:P" + allow + "S:(ML;;NW;;;ME)"})
}

// dialSocket — подключение к каналу.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}

// CleanupLegacySockets удаляет сокет-файлы прежних сборок (см. install.CleanupLegacySockets).
func CleanupLegacySockets(dataDir string) { install.CleanupLegacySockets(dataDir) }
