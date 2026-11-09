package bmstuvpn

import (
	"log"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/xtls/xray-core/transport/internet"
)

// Protector выводит сокет из VPN (реализуется на Java через VpnService.protect).
//
// Одного исключения приложения из своего VPN (addDisallowedApplication) мало: на части устройств
// и в эмуляторах (Nox) оно не действует, и соединения ядра к VPN-серверу уходят в собственный
// туннель — получается петля. protect() для каждого сокета работает везде.
type Protector interface {
	Protect(fd int) bool
}

type protectorBox struct{ p Protector }

var (
	protector    atomic.Pointer[protectorBox]
	registerOnce sync.Once
)

// SetProtector задаёт защиту сокетов для ядра Xray и клиента МГТУ; nil — отключить.
func SetProtector(p Protector) {
	if p == nil {
		protector.Store(nil)
	} else {
		protector.Store(&protectorBox{p: p})
	}
	registerOnce.Do(func() {
		// Все исходящие сокеты Xray (TCP и UDP, включая DNS) проходят через этот хук
		_ = internet.RegisterDialerController(func(network, address string, c syscall.RawConn) error {
			return protectRaw(c)
		})
	})
}

// protectRaw защищает сокет, если защита задана. Ошибка protect() только записывается в журнал:
// без поднятого VPN защищать нечего, а отказ в соединении был бы хуже.
func protectRaw(c syscall.RawConn) error {
	box := protector.Load()
	if box == nil {
		return nil
	}
	return c.Control(func(fd uintptr) {
		if !box.p.Protect(int(fd)) {
			log.Printf("bmstuvpn: protect(%d) failed", fd)
		}
	})
}

// protectControl — для net.Dialer.Control собственных соединений (OpenVPN, прямой выход прокси).
func protectControl(_, _ string, c syscall.RawConn) error {
	return protectRaw(c)
}
