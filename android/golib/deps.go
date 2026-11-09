//go:build tools

// Пакеты, которые собирает gomobile bind вместе с bmstuvpn (см. build.ps1):
// ядро Xray (AndroidLibXrayLite v26.10.31) и привязки gomobile.
package golib

import (
	_ "github.com/2dust/AndroidLibXrayLite"
	_ "golang.org/x/mobile/bind"
)
