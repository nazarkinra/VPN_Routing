package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"vpnrouting/desktop/buildinfo"
	"vpnrouting/desktop/install"
)

// Обновление ПК-версии из репозитория на GitHub (аналог UpdateManager.java).
//
// Рядом с VPN_routing.apk в корне репозитория лежит desktop-version.json (его пишет desktop/publish.ps1):
//
//	{"versionCode": 2, "versionName": "1.1", "notes": "что нового", "date": "2026-11-10",
//	 "files": {"windows-amd64": {"file": "VPN_routing-windows.zip", "size": 1, "sha256": "…"},
//	           "linux-amd64": {…}, "linux-arm64": {…}}}
//
// Приложение сравнивает versionCode со своим, скачивает архив своей системы, сверяет размер и SHA-256,
// подменяет свою программу новой и перезапускается.

const (
	// Версия — в пакете buildinfo (её читает и установщик)
	VersionCode = buildinfo.VersionCode
	VersionName = buildinfo.VersionName

	defaultUpdateBase = "https://raw.githubusercontent.com/nazarkinra/VPN_Routing/main/"
	// Архивы лежат в Git LFS: ссылка github.com/…/raw/… перенаправляет на сам файл, а не на указатель LFS
	defaultFilesBase  = "https://github.com/nazarkinra/VPN_Routing/raw/main/"
	updateManifest    = "desktop-version.json"
	autoCheckInterval = 12 * time.Hour
	maxManifest       = 64 * 1024
	maxArchive        = 200 << 20
)

// Platform — ключ архива в файле версии: windows-amd64, linux-amd64, linux-arm64.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// UpdateInfo — сведения о версии из файла версии.
type UpdateInfo struct {
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
	Notes       string `json:"notes"`
	URL         string `json:"-"`
	Size        int64  `json:"size"`
	SHA256      string `json:"-"`
}

// Newer — опубликованная версия новее установленной.
func (i *UpdateInfo) Newer() bool { return i.VersionCode > VersionCode }

type manifestFile struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	VersionCode int                     `json:"versionCode"`
	VersionName string                  `json:"versionName"`
	Notes       string                  `json:"notes"`
	Files       map[string]manifestFile `json:"files"`
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseManifest разбирает файл версии; file — имя относительно filesBase или полный адрес https://.
func ParseManifest(data []byte, platform, filesBase string) (*UpdateInfo, error) {
	var m manifest
	// Блокнот и PowerShell сохраняют UTF-8 с BOM — его JSON-разбор не принимает
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(data, &m); err != nil || m.VersionCode == 0 {
		return nil, errors.New("файл версии повреждён")
	}
	f, ok := m.Files[platform]
	if !ok {
		return nil, fmt.Errorf("в выпуске %s нет сборки для %s", m.VersionName, platform)
	}
	sha := strings.ToLower(f.SHA256)
	if !shaRE.MatchString(sha) {
		return nil, errors.New("в файле версии нет контрольной суммы")
	}
	url := f.File
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		url = filesBase + url
	}
	name := m.VersionName
	if name == "" {
		name = "?"
	}
	return &UpdateInfo{VersionCode: m.VersionCode, VersionName: name, Notes: strings.TrimSpace(m.Notes),
		URL: url, Size: f.Size, SHA256: sha}, nil
}

// UpdateStatus — состояние обновления для интерфейса.
type UpdateStatus struct {
	Current  string      `json:"current"`
	Checking bool        `json:"checking"`
	Info     *UpdateInfo `json:"info"`
	// Newer — найденная версия новее установленной.
	Newer bool `json:"newer"`
	// Offer — предложить обновление (новее установленной и не отложено кнопкой «Позже»).
	Offer       bool   `json:"offer"`
	Downloading bool   `json:"downloading"`
	Done        int64  `json:"done"`
	Total       int64  `json:"total"`
	Error       string `json:"error"`
	// Ready — программа заменена, приложение перезапускается.
	Ready bool `json:"ready"`
}

// Updater — проверка и установка обновлений.
type Updater struct {
	store *Store
	dir   string
	// Restart — перезапуск приложения новой программой (задаётся main).
	Restart func()

	mu sync.Mutex
	st UpdateStatus
}

func NewUpdater(store *Store, dataDir string) *Updater {
	return &Updater{store: store, dir: dataDir, st: UpdateStatus{Current: VersionName}}
}

func bases() (manifestURL, filesBase string) {
	if b := os.Getenv("VPNROUTING_UPDATE_BASE"); b != "" {
		if !strings.HasSuffix(b, "/") {
			b += "/"
		}
		return b + updateManifest, b
	}
	return defaultUpdateBase + updateManifest, defaultFilesBase
}

func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "VPNrouting/"+VersionName+" ("+Platform()+")")
	req.Header.Set("Cache-Control", "no-cache")
	return (&http.Client{Timeout: 10 * time.Minute}).Do(req)
}

// Status — снимок для интерфейса.
func (u *Updater) Status() UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

// AutoCheckLoop проверяет обновления при запуске и затем раз в 12 часов.
func (u *Updater) AutoCheckLoop() {
	for {
		if time.Since(time.UnixMilli(u.store.UpdateLastCheck())) > autoCheckInterval {
			_, _ = u.Check(false)
		}
		time.Sleep(time.Hour)
	}
}

// Check читает файл версии. manual — по кнопке: обновление предлагается, даже если было отложено.
func (u *Updater) Check(manual bool) (*UpdateInfo, error) {
	u.mu.Lock()
	if u.st.Checking || u.st.Downloading {
		u.mu.Unlock()
		return nil, errors.New("проверка уже идёт")
	}
	u.st.Checking = true
	u.mu.Unlock()

	info, err := u.fetch()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.st.Checking = false
	if err != nil {
		logf("update check: %v", err)
		return nil, err
	}
	u.store.SetUpdateLastCheck(time.Now().UnixMilli())
	u.st.Info = info
	u.st.Newer = info.Newer()
	u.st.Offer = info.Newer() && (manual || u.store.UpdateSkipped() < info.VersionCode)
	return info, nil
}

func (u *Updater) fetch() (*UpdateInfo, error) {
	manifestURL, filesBase := bases()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := httpGet(ctx, fmt.Sprintf("%s?t=%d", manifestURL, time.Now().UnixMilli()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("файл версии не найден (репозиторий закрыт или ПК-версия ещё не опубликована)")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("сервер ответил %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifest {
		return nil, errors.New("файл версии слишком большой")
	}
	return ParseManifest(data, Platform(), filesBase)
}

// Skip — «Позже»: при автопроверке эту версию больше не предлагать.
func (u *Updater) Skip() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.st.Info != nil {
		u.store.SetUpdateSkipped(u.st.Info.VersionCode)
	}
	u.st.Offer = false
}

// Install скачивает найденную версию в фоне, подменяет программу и перезапускает приложение.
func (u *Updater) Install() error {
	u.mu.Lock()
	info := u.st.Info
	if info == nil || !info.Newer() {
		u.mu.Unlock()
		return errors.New("новой версии нет: сначала проверьте обновления")
	}
	if u.st.Downloading {
		u.mu.Unlock()
		return nil
	}
	u.st.Downloading, u.st.Done, u.st.Total, u.st.Error, u.st.Offer = true, 0, info.Size, "", false
	u.mu.Unlock()

	go func() {
		err := u.install(info)
		u.mu.Lock()
		u.st.Downloading = false
		if err != nil {
			u.st.Error = err.Error()
			logf("update: %v", err)
		} else {
			u.st.Ready = true
		}
		u.mu.Unlock()
		if err == nil && u.Restart != nil {
			logf("обновлено до %s, перезапуск", info.VersionName)
			u.Restart()
		}
	}()
	return nil
}

type progressWriter struct {
	u *Updater
	n int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	w.u.mu.Lock()
	w.u.st.Done = w.n
	w.u.mu.Unlock()
	return len(p), nil
}

func (u *Updater) install(info *UpdateInfo) error {
	resp, err := httpGet(context.Background(), info.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("сервер ответил %d", resp.StatusCode)
	}
	if info.Size <= 0 && resp.ContentLength > 0 {
		u.mu.Lock()
		u.st.Total = resp.ContentLength
		u.mu.Unlock()
	}
	var buf bytes.Buffer
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(&buf, h, &progressWriter{u: u}), io.LimitReader(resp.Body, maxArchive))
	if err != nil {
		return err
	}
	if info.Size > 0 && n != info.Size {
		return fmt.Errorf("файл скачался не полностью (%d из %d байт)", n, info.Size)
	}
	if hex.EncodeToString(h.Sum(nil)) != info.SHA256 {
		return errors.New("контрольная сумма не совпала: файл повреждён или подменён")
	}
	bin, err := extractBinary(buf.Bytes(), info.URL)
	if err != nil {
		return err
	}
	if err := replaceExecutable(bin); err != nil {
		return err
	}
	// Версия в «Параметры → Приложения», если программа установлена установщиком
	install.SetInstalledVersion(info.VersionName)
	return nil
}

// extractBinary достаёт программу из архива: vpnrouting.exe из .zip, vpnrouting из .tar.gz.
func extractBinary(data []byte, name string) ([]byte, error) {
	want := "vpnrouting"
	if runtime.GOOS == "windows" {
		want = "vpnrouting.exe"
	}
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, errors.New("архив повреждён")
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, maxArchive))
			}
		}
		return nil, errors.New("в архиве нет " + want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("архив повреждён")
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, errors.New("в архиве нет " + want)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == want {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// replaceExecutable подменяет работающую программу: старая переименовывается в .old (Windows и Linux
// это позволяют), новая встаёт на её место. .old удаляется при следующем запуске.
func replaceExecutable(bin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("нет доступа к папке программы: %w", err)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// CleanupOldExecutable удаляет программу, оставшуюся от прошлого обновления.
func CleanupOldExecutable() {
	if exe, err := os.Executable(); err == nil {
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		_ = os.Remove(exe + ".old")
	}
}
