// Значок приложения и ресурсы Windows (значок exe, версия, манифест) — вызывается из build.ps1.
//
//	go run ./tools/res -syso rsrc_windows_amd64.syso -desc "VPN routing"           программа
//	go run ./tools/res -syso installer/rsrc_windows_amd64.syso -admin -desc "…"    установщик
//
// Рисунок — assets/vpnrouting.png (тот же значок, что у Android-приложения; его делает tools/icon).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"

	"golang.org/x/image/draw"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"vpnrouting/desktop/app"
	"vpnrouting/desktop/assets"
)

// render — значок size×size из мастер-рисунка (assets/vpnrouting.png, его делает tools/icon).
func render(size int) image.Image {
	src, err := png.Decode(bytes.NewReader(assets.IconPNG))
	check(err)
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}
func main() {
	sysoOut := flag.String("syso", "", "сохранить ресурсы Windows (.syso)")
	admin := flag.Bool("admin", false, "манифест: требовать права администратора")
	desc := flag.String("desc", "VPN routing", "описание файла")
	name := flag.String("name", "vpnrouting.exe", "исходное имя файла")
	flag.Parse()

	if *sysoOut == "" {
		return
	}
	var images []image.Image
	for _, s := range []int{16, 20, 24, 32, 40, 48, 64, 256} {
		images = append(images, render(s))
	}
	icon, err := winres.NewIconFromImages(images)
	check(err)
	rs := &winres.ResourceSet{}
	check(rs.SetIcon(winres.Name("APPICON"), icon))

	m := winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "VPNrouting." + strings.TrimSuffix(*name, ".exe")},
		Description:         *desc,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
	}
	if *admin {
		m.ExecutionLevel = winres.RequireAdministrator
	}
	rs.SetManifest(m)

	var vi version.Info
	ver := fmt.Sprintf("%s.0.%d", app.VersionName, app.VersionCode)
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	for k, v := range map[string]string{
		version.ProductName:      "VPN routing",
		version.FileDescription:  *desc,
		version.OriginalFilename: *name,
		version.ProductVersion:   app.VersionName,
		version.FileVersion:      app.VersionName,
	} {
		check(vi.Set(version.LangDefault, k, v))
	}
	rs.SetVersionInfo(vi)

	f, err := os.Create(*sysoOut)
	check(err)
	check(rs.WriteObject(f, winres.ArchAMD64))
	check(f.Close())
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}
