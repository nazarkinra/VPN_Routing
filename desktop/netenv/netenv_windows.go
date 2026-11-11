package netenv

import (
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	ipUnicastIf   = 31 // IP_UNICAST_IF
	ipv6UnicastIf = 31 // IPV6_UNICAST_IF
)

func detect(tun string) (*Uplink, error) {
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return nil, err
	}
	var best *winipcfg.MibIPforwardRow2
	var bestRow *winipcfg.MibIfRow2
	bestMetric := ^uint32(0)
	for i := range rows {
		r := &rows[i]
		if r.DestinationPrefix.PrefixLength != 0 {
			continue
		}
		ifrow, err := r.InterfaceLUID.Interface()
		if err != nil || ifrow.OperStatus != winipcfg.IfOperStatusUp {
			continue
		}
		if tun != "" && strings.EqualFold(ifrow.Alias(), tun) {
			continue
		}
		metric := r.Metric
		if ipif, err := r.InterfaceLUID.IPInterface(windows.AF_INET); err == nil {
			metric += ipif.Metric
		}
		if metric < bestMetric {
			best, bestRow, bestMetric = r, ifrow, metric
		}
	}
	if best == nil {
		return nil, errors.New("нет подключения к сети (маршрут по умолчанию не найден)")
	}
	u := &Uplink{Index: int(best.InterfaceIndex), Name: bestRow.Alias()}
	if servers, err := best.InterfaceLUID.DNS(); err == nil {
		for _, a := range servers {
			if a.Is4() && !isTunDNS(a.String()) {
				u.DNS = a.String()
				break
			}
		}
	}
	return u, nil
}

func bind(fd uintptr, v6 bool, u *Uplink) error {
	h := windows.Handle(fd)
	if v6 {
		return windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIf, u.Index)
	}
	// Для IPv4 индекс передаётся в сетевом порядке байт
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(u.Index))
	return windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, int(binary.NativeEndian.Uint32(b[:])))
}

// HasSystemdResolved — только для Linux.
func HasSystemdResolved() bool { return false }
