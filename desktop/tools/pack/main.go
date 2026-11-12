// Упаковка выпуска ПК-версии (вызывается из desktop/publish.ps1): архивы в корне репозитория и
// desktop-version.json, по которому приложение узнаёт о новой версии и проверяет скачанный файл.
//
//	go run ./tools/pack -dist dist -out .. -notes "что нового"
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vpnrouting/desktop/app"
)

type file struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	VersionCode int             `json:"versionCode"`
	VersionName string          `json:"versionName"`
	Notes       string          `json:"notes"`
	Date        string          `json:"date"`
	Files       map[string]file `json:"files"`
}

type entry struct{ src, name string }

func main() {
	dist := flag.String("dist", "dist", "папка сборки (build.ps1)")
	out := flag.String("out", "..", "куда положить архивы и desktop-version.json")
	notes := flag.String("notes", "", "что нового")
	readme := flag.String("readme", "README.md", "описание, кладётся в архив")
	flag.Parse()

	manifestPath := filepath.Join(*out, "desktop-version.json")
	if b, err := os.ReadFile(manifestPath); err == nil {
		var prev manifest
		if json.Unmarshal(b, &prev) == nil && app.VersionCode <= prev.VersionCode {
			fail(fmt.Errorf("VersionCode %d не больше опубликованного (%d): увеличьте его в buildinfo/buildinfo.go",
				app.VersionCode, prev.VersionCode))
		}
	}

	m := manifest{VersionCode: app.VersionCode, VersionName: app.VersionName, Notes: *notes,
		Date: time.Now().Format("2006-01-02"), Files: map[string]file{}}

	win := []entry{
		{filepath.Join(*dist, "windows", "vpnrouting.exe"), "vpnrouting.exe"},
		{filepath.Join(*dist, "windows", "wintun.dll"), "wintun.dll"},
		{filepath.Join(*dist, "windows", "wintun-LICENSE.txt"), "wintun-LICENSE.txt"},
		// Программная отрисовка окна (Mesa): самообновление тоже берёт её из этого архива
		{filepath.Join(*dist, "windows", "softgl", "opengl32.dll"), "softgl/opengl32.dll"},
		{filepath.Join(*dist, "windows", "softgl", "libgallium_wgl.dll"), "softgl/libgallium_wgl.dll"},
		{filepath.Join(*dist, "windows", "softgl", "README.txt"), "softgl/README.txt"},
		{*readme, "README.md"},
	}
	m.Files["windows-amd64"] = pack(filepath.Join(*out, "VPN_routing-windows.zip"), win, writeZip)
	// arm64 для Linux с окном пока не собирается: нужен кросс-компилятор и библиотеки arm64
	for _, arch := range []string{"amd64"} {
		lin := []entry{{filepath.Join(*dist, "linux-"+arch, "vpnrouting"), "vpnrouting"},
			{"linux/install.sh", "install.sh"}, {"linux/uninstall.sh", "uninstall.sh"}, {*readme, "README.md"}}
		name := "VPN_routing-linux-" + arch + ".tar.gz"
		m.Files["linux-"+arch] = pack(filepath.Join(*out, name), lin, writeTarGz)
	}

	// Установщик для Windows — рядом с архивами (для самообновления он не нужен: там zip)
	setup := filepath.Join(*out, "VPN_routing-setup.exe")
	if data, err := os.ReadFile(filepath.Join(*dist, "VPN_routing-setup.exe")); err != nil {
		fail(err)
	} else if err := os.WriteFile(setup, data, 0o755); err != nil {
		fail(err)
	} else {
		sum := sha256.Sum256(data)
		m.Files["windows-setup"] = file{File: filepath.Base(setup), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	}

	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(manifestPath, append(b, '\n'), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("Готово: версия %s (код %d)\n", m.VersionName, m.VersionCode)
	for k, f := range m.Files {
		fmt.Printf("  %-14s %s  %.1f МБ\n", k, f.File, float64(f.Size)/(1<<20))
	}
}

func pack(path string, entries []entry, write func(io.Writer, []entry) error) file {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	if err := write(f, entries); err != nil {
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	sum := sha256.Sum256(data)
	return file{File: filepath.Base(path), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// Внутри архива всё лежит в папке VPN_routing/.
func writeZip(w io.Writer, entries []entry) error {
	zw := zip.NewWriter(w)
	for _, e := range entries {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return err
		}
		h := &zip.FileHeader{Name: "VPN_routing/" + e.name, Method: zip.Deflate, Modified: time.Now()}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeTarGz(w io.Writer, entries []entry) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if e.name == "vpnrouting" || strings.HasSuffix(e.name, ".sh") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: "VPN_routing/" + e.name, Mode: mode, Size: int64(len(data)),
			ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "Ошибка:", err)
	os.Exit(1)
}
