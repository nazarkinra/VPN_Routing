package main

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// glInfo — какой OpenGL даёт система этой программе: производитель, видеокарта, версия драйвера. Для
// журнала окна (issue #2: окно белое без единой ошибки — нужно видеть, чей это OpenGL). Проверка идёт в
// своём невидимом окне со своим контекстом и не трогает окно Fyne.
//
// opengl32.dll — тот же, что загрузила программа: у копии с программной отрисовкой это Mesa.
func glInfo() (vendor, renderer, version string, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	user32 := windows.NewLazySystemDLL("user32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	gl := windows.NewLazyDLL("opengl32.dll")
	createWindow := user32.NewProc("CreateWindowExW")
	destroyWindow := user32.NewProc("DestroyWindow")
	getDC := user32.NewProc("GetDC")
	releaseDC := user32.NewProc("ReleaseDC")
	choosePF := gdi32.NewProc("ChoosePixelFormat")
	setPF := gdi32.NewProc("SetPixelFormat")
	wglCreate := gl.NewProc("wglCreateContext")
	wglMake := gl.NewProc("wglMakeCurrent")
	wglDelete := gl.NewProc("wglDeleteContext")
	glGetString := gl.NewProc("glGetString")
	if err = gl.Load(); err != nil {
		return
	}

	// Готовый класс STATIC — регистрировать свой не нужно
	cls, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, e := createWindow.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 1, 1, 0, 0, 0, 0)
	if hwnd == 0 {
		return "", "", "", errors.New("CreateWindowEx: " + e.Error())
	}
	defer destroyWindow.Call(hwnd)
	hdc, _, _ := getDC.Call(hwnd)
	if hdc == 0 {
		return "", "", "", errors.New("GetDC")
	}
	defer releaseDC.Call(hwnd, hdc)

	// PIXELFORMATDESCRIPTOR: как у окна Fyne — RGBA, двойная буферизация
	var pfd struct {
		size, version uint16
		flags         uint32
		b             [20]byte // iPixelType, cColorBits, …, cDepthBits [15], cStencilBits [16], …
		masks         [3]uint32
	}
	pfd.size = uint16(unsafe.Sizeof(pfd))
	pfd.version = 1
	pfd.flags = 0x4 | 0x20 | 0x1 // PFD_DRAW_TO_WINDOW | PFD_SUPPORT_OPENGL | PFD_DOUBLEBUFFER
	pfd.b[0] = 0                 // PFD_TYPE_RGBA
	pfd.b[1] = 32
	pfd.b[15] = 24
	pfd.b[16] = 8
	pf, _, _ := choosePF.Call(hdc, uintptr(unsafe.Pointer(&pfd)))
	if pf == 0 {
		return "", "", "", errors.New("ChoosePixelFormat: нет формата с OpenGL")
	}
	if r, _, e := setPF.Call(hdc, pf, uintptr(unsafe.Pointer(&pfd))); r == 0 {
		return "", "", "", errors.New("SetPixelFormat: " + e.Error())
	}
	ctx, _, e := wglCreate.Call(hdc)
	if ctx == 0 {
		return "", "", "", errors.New("wglCreateContext: " + e.Error())
	}
	defer wglDelete.Call(ctx)
	if r, _, e := wglMake.Call(hdc, ctx); r == 0 {
		return "", "", "", errors.New("wglMakeCurrent: " + e.Error())
	}
	defer wglMake.Call(0, 0)

	str := func(name uintptr) string {
		p, _, _ := glGetString.Call(name)
		if p == 0 {
			return "?"
		}
		// Указатель на строку драйвера (не память Go): через переменную, чтобы go vet не ругался
		return windows.BytePtrToString(*(**byte)(unsafe.Pointer(&p)))
	}
	return str(0x1F00), str(0x1F01), str(0x1F02), nil // GL_VENDOR, GL_RENDERER, GL_VERSION
}
