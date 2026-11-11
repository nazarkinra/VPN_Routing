package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

const testSHA = "07840c18f16a6540b1b498a51df7c65cdd3d90cb72124ee179d57057d2abb588"

func TestParseManifest(t *testing.T) {
	data := []byte(`{"versionCode": 999, "versionName": "9.9", "notes": " Новое ",
		"files": {"windows-amd64": {"file": "VPN_routing-windows.zip", "size": 123, "sha256": "` + strings.ToUpper(testSHA) + `"},
		          "linux-amd64": {"file": "https://cdn/x.tar.gz", "sha256": "` + testSHA + `"}}}`)
	info, err := ParseManifest(data, "windows-amd64", "https://github.com/x/y/raw/main/")
	if err != nil {
		t.Fatal(err)
	}
	if info.VersionCode != 999 || info.URL != "https://github.com/x/y/raw/main/VPN_routing-windows.zip" ||
		info.SHA256 != testSHA || info.Notes != "Новое" || info.Size != 123 || !info.Newer() {
		t.Fatalf("%+v", info)
	}
	info, err = ParseManifest(data, "linux-amd64", "https://base/")
	if err != nil || info.URL != "https://cdn/x.tar.gz" {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := ParseManifest(data, "linux-arm64", "https://base/"); err == nil {
		t.Fatal("нет сборки для linux-arm64")
	}
	if _, err := ParseManifest([]byte(`{"versionCode": 5, "files": {"windows-amd64": {"file": "a.zip"}}}`),
		"windows-amd64", ""); err == nil {
		t.Fatal("без контрольной суммы")
	}
	old, _ := ParseManifest([]byte(`{"versionCode": 1, "files": {"x": {"file": "a", "sha256": "`+testSHA+`"}}}`), "x", "")
	if old.Newer() {
		t.Fatal("старая версия не новее")
	}
}

func TestExtractBinary(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("VPN_routing/vpnrouting.exe")
	_, _ = w.Write([]byte("WIN"))
	_ = zw.Close()

	var tbuf bytes.Buffer
	gz := gzip.NewWriter(&tbuf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "VPN_routing/vpnrouting", Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("LNX"))
	_ = tw.Close()
	_ = gz.Close()

	want, data, name := "LNX", tbuf.Bytes(), "a.tar.gz"
	if Platform()[:7] == "windows" {
		want, data, name = "WIN", zbuf.Bytes(), "a.zip"
	}
	got, err := extractBinary(data, name)
	if err != nil || string(got) != want {
		t.Fatalf("%q %v", got, err)
	}
}
