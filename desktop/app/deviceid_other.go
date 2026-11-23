//go:build !windows

package app

import (
	"os"
	"strings"
)

// platformID — machine-id systemd (есть и в ALT, и в Ubuntu); у старых систем — копия от dbus.
func platformID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	return ""
}
