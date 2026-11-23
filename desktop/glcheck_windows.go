package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"vpnrouting/desktop/app"
)

// Окно (Fyne) рисуется через OpenGL 2.1. Если драйвер видеокарты его не даёт (нет драйвера, «Базовый
// видеоадаптер Microsoft», виртуальная машина, удалённый рабочий стол), Fyne только пишет ошибку в stderr,
// которого у оконной программы нет, — и окно остаётся белым.
//
// Поэтому журнал Fyne пишется в gui.log в папке настроек пользователя, а при ошибке OpenGL окно
// перезапускается с программной отрисовкой: Mesa llvmpipe (softgl\ рядом с программой).
//
// opengl32.dll программа импортирует статически, и Windows берёт его из папки программы раньше системного.
// Поэтому окно с программной отрисовкой — это копия программы в %LOCALAPPDATA%\VPN routing\softgl рядом с
// DLL из Mesa (в «Program Files» пользователь писать не может, а положить Mesa рядом с основной программой
// значит отключить видеокарту всем). Копия обновляется при каждом запуске, если программа сменилась.
//
// Включается и вручную: VPNROUTING_SOFTGL=1 или пустой файл softgl в %APPDATA%\VPN routing.

const envOrigExe = "VPNROUTING_ORIG_EXE"

var (
	guiDataDir string
	// glRestart — перезапустить окно (после включения программной отрисовки); nil — нельзя.
	glRestart func()
	glOnce    sync.Once
)

func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe
}

// mainExe — сама программа (а не её копия с программной отрисовкой): её запускать заново.
func mainExe() string {
	if p := os.Getenv(envOrigExe); p != "" && runningSoftGLCopy() {
		return p
	}
	return exePath()
}

func softGLCopyDir() string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return ""
	}
	return filepath.Join(base, "VPN routing", app.SoftGLDir)
}

func runningSoftGLCopy() bool {
	d := softGLCopyDir()
	return d != "" && strings.EqualFold(filepath.Dir(exePath()), d)
}

func softGLMarker(dir string) string { return filepath.Join(dir, "softgl") }

func softGLWanted(dir string) bool {
	if os.Getenv("VPNROUTING_SOFTGL") == "1" {
		return true
	}
	_, err := os.Stat(softGLMarker(dir))
	return err == nil
}

// redirectSoftGL — до всего остального: если нужна программная отрисовка, запустить окно из копии
// программы рядом с Mesa. true — копия запущена, этому процессу остаётся выйти.
func redirectSoftGL(dir string, args []string) bool {
	if runningSoftGLCopy() || !softGLWanted(dir) {
		return false
	}
	exe := exePath()
	src := filepath.Join(filepath.Dir(exe), app.SoftGLDir)
	if _, err := os.Stat(filepath.Join(src, "opengl32.dll")); err != nil {
		return false // Mesa нет — обычное окно (при ошибке OpenGL будет сообщение)
	}
	dst := softGLCopyDir()
	files := map[string]string{
		exe:                                      filepath.Join(dst, filepath.Base(exe)),
		filepath.Join(src, "opengl32.dll"):       filepath.Join(dst, "opengl32.dll"),
		filepath.Join(src, "libgallium_wgl.dll"): filepath.Join(dst, "libgallium_wgl.dll"),
		// Переносной версии (VPN в этом же процессе) нужен и драйвер TUN
		filepath.Join(filepath.Dir(exe), "wintun.dll"): filepath.Join(dst, "wintun.dll"),
	}
	for from, to := range files {
		if err := syncFile(from, to); err != nil && !os.IsNotExist(err) {
			app.Logf("программная отрисовка: %v", err)
			return false
		}
	}
	_ = os.Setenv(envOrigExe, exe)
	_ = os.Setenv("GALLIUM_DRIVER", "llvmpipe")
	if err := spawnDetached(files[exe], args); err != nil {
		app.Logf("программная отрисовка: %v", err)
		return false
	}
	return true
}

// syncFile копирует файл, если копии нет или она отличается размером или временем изменения.
func syncFile(from, to string) error {
	fi, err := os.Stat(from)
	if err != nil {
		return err
	}
	if ti, err := os.Stat(to); err == nil && ti.Size() == fi.Size() && ti.ModTime().Equal(fi.ModTime()) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(to + ".old")
	_ = os.Rename(to, to+".old") // запущенную копию заменить нельзя, переименовать — можно
	if err := os.Rename(tmp, to); err != nil {
		return err
	}
	_ = os.Remove(to + ".old")
	return os.Chtimes(to, time.Now(), fi.ModTime())
}

// prepareGUI — до создания окна: журнал Fyne в gui.log.
func prepareGUI(dir string) {
	guiDataDir = dir
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "gui.log")
	_ = os.Rename(path, path+".1")
	var w io.Writer = io.Discard
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		w = f
	}
	log.SetOutput(io.MultiWriter(w, glWatch{}))
	v := windows.RtlGetVersion()
	mode := "OpenGL видеокарты"
	if runningSoftGLCopy() {
		mode = "программная отрисовка (Mesa llvmpipe)"
	}
	log.Printf("VPN routing %s, окно; Windows %d.%d.%d; %s", version, v.MajorVersion, v.MinorVersion, v.BuildNumber, mode)
	go func() {
		if vendor, renderer, ver, err := glInfo(); err != nil {
			log.Printf("OpenGL: %v", err)
		} else {
			log.Printf("OpenGL: %s; %s; %s", vendor, renderer, ver)
		}
	}()
}

// hwGLMarker — пользователь сам выбрал отрисовку видеокартой: пустое окно не переключает её на программную.
func hwGLMarker(dir string) string { return filepath.Join(dir, "hwgl") }

// softGLAvailable — рядом с программой есть Mesa (входит в установщик и zip).
func softGLAvailable() bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(mainExe()), app.SoftGLDir, "opengl32.dll"))
	return err == nil
}

// guiRenderOptions — переключатель отрисовки и реакция на пустое окно для gui.Options (issue #2).
func guiRenderOptions() (bool, func(bool) bool, func(string)) {
	if !softGLAvailable() {
		return runningSoftGLCopy(), nil, nil
	}
	return runningSoftGLCopy(), setSoftGL, onBlank
}

// setSoftGL — выбор пользователя (трей, настройки): запомнить и перезапустить окно, если можно.
func setSoftGL(on bool) bool {
	soft, hw := softGLMarker(guiDataDir), hwGLMarker(guiDataDir)
	if on {
		touch(soft)
		_ = os.Remove(hw)
		log.Printf("программная отрисовка включена пользователем")
	} else {
		_ = os.Remove(soft)
		touch(hw)
		log.Printf("отрисовка видеокартой выбрана пользователем")
	}
	if glRestart == nil {
		return false
	}
	go glRestart()
	return true
}

func touch(path string) {
	if f, err := os.Create(path); err == nil {
		f.Close()
	}
}

// onBlank — окно показано, но пустое (OpenGL без ошибок, а ничего не нарисовано).
func onBlank(detail string) {
	if runningSoftGLCopy() {
		return // и программная отрисовка пустая — переключать некуда, в журнале уже есть подробности
	}
	if _, err := os.Stat(hwGLMarker(guiDataDir)); err == nil {
		log.Printf("отрисовку видеокартой выбрал пользователь — не переключаю")
		return
	}
	glOnce.Do(func() { onGLFailure(detail) })
}

// glWatch ищет в журнале Fyne ошибки OpenGL, после которых окно остаётся пустым.
type glWatch struct{}

func (glWatch) Write(p []byte) (int, error) {
	s := string(p)
	if strings.Contains(s, "failed to initialise OpenGL") || strings.Contains(s, "Failed to compile shader") {
		glOnce.Do(func() { go onGLFailure(strings.TrimSpace(s)) })
	}
	return len(p), nil
}

func onGLFailure(detail string) {
	log.Printf("ошибка OpenGL: окно не может отрисоваться")
	mesa := filepath.Join(filepath.Dir(mainExe()), app.SoftGLDir, "opengl32.dll")
	if _, err := os.Stat(mesa); err == nil && !runningSoftGLCopy() {
		// Запомнить: следующие запуски сразу с программной отрисовкой
		touch(softGLMarker(guiDataDir))
		log.Printf("включена программная отрисовка")
		if glRestart != nil {
			glRestart()
			return
		}
		showError("Видеодрайвер не смог отрисовать окно, поэтому включена программная отрисовка. " +
			"Закройте VPN routing (значок в трее → «Выход») и откройте снова.")
		return
	}
	showError(fmt.Sprintf("Окно VPN routing не может отрисоваться: видеодрайвер не поддерживает OpenGL 2.1.\n\n"+
		"Обновите драйвер видеокарты (сайт производителя или Центр обновления Windows) либо установите "+
		"«Пакет совместимости OpenCL, OpenGL и Vulkan» из Microsoft Store.\n\n"+
		"VPN при этом работает: включать и выключать его можно из значка в трее.\n\n"+
		"Подробности: %s", filepath.Join(guiDataDir, "gui.log")))
}
