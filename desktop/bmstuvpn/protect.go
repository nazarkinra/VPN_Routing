package bmstuvpn

import (
	"sync/atomic"
	"syscall"
)

// На ПК, в отличие от Android, нет VpnService.protect(): весь трафик системы, в том числе этого
// приложения, идёт в TUN. Поэтому собственные сокеты клиента МГТУ (UDP/TCP к серверу OpenVPN и прямой
// выход прокси) привязываются к физическому интерфейсу — это делает функция, заданная SetDialControl
// (пакет netenv: SO_BINDTODEVICE на Linux, IP_UNICAST_IF на Windows).

// DialControl — net.Dialer.Control для сокетов мимо туннеля.
type DialControl func(network, address string, c syscall.RawConn) error

var dialControl atomic.Pointer[DialControl]

// SetDialControl задаёт привязку сокетов; nil — без привязки.
func SetDialControl(f DialControl) {
	if f == nil {
		dialControl.Store(nil)
		return
	}
	dialControl.Store(&f)
}

// protectControl — для net.Dialer.Control собственных соединений (OpenVPN, прямой выход прокси).
func protectControl(network, address string, c syscall.RawConn) error {
	if f := dialControl.Load(); f != nil {
		return (*f)(network, address, c)
	}
	return nil
}
