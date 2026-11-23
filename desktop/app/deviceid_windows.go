package app

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// platformID — MachineGuid: задаётся при установке Windows, один для всех учётных записей и службы.
func platformID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
