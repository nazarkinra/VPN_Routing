package netenv

import (
	"bufio"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func detect(tun string) (*Uplink, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	var best *netlink.Route
	bestName := ""
	for i := range routes {
		r := &routes[i]
		if r.Dst != nil {
			if ones, _ := r.Dst.Mask.Size(); ones != 0 {
				continue
			}
		}
		if r.LinkIndex == 0 {
			continue
		}
		link, err := netlink.LinkByIndex(r.LinkIndex)
		if err != nil {
			continue
		}
		name := link.Attrs().Name
		if name == tun {
			continue
		}
		if best == nil || r.Priority < best.Priority {
			best, bestName = r, name
		}
	}
	if best == nil {
		return nil, errors.New("нет подключения к сети (маршрут по умолчанию не найден)")
	}
	return &Uplink{Index: best.LinkIndex, Name: bestName, DNS: systemDNS()}, nil
}

// systemDNS — DNS сети. При systemd-resolved в /etc/resolv.conf стоит его заглушка 127.0.0.53,
// а настоящие серверы перечислены в /run/systemd/resolve/resolv.conf.
func systemDNS() string {
	for _, path := range []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 || fields[0] != "nameserver" {
				continue
			}
			ip, err := netip.ParseAddr(fields[1])
			if err != nil || !ip.Is4() || ip.IsLoopback() || isTunDNS(ip.String()) {
				continue
			}
			f.Close()
			return ip.String()
		}
		f.Close()
	}
	return ""
}

func bind(fd uintptr, _ bool, u *Uplink) error {
	return unix.BindToDevice(int(fd), u.Name)
}

// HasSystemdResolved — системным DNS управляет systemd-resolved (тогда ядро Xray может перевести
// его на TUN через resolvectl).
func HasSystemdResolved() bool {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return false
	}
	out, err := exec.Command("systemctl", "is-active", "systemd-resolved").Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}
