package gui

import (
	"fmt"
	"image"
	"log"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
)

// Проверка, что окно действительно отрисовалось (issue #2).
//
// Бывает, что драйвер видеокарты даёт OpenGL без единой ошибки, а окно остаётся белым (замечено на AMD
// Radeon 780M). Fyne об этом не знает, поэтому через несколько секунд после показа окно читает свой
// кадр: настоящий интерфейс — это вкладки, текст и карточки, а кадр одного цвета значит, что ничего не
// нарисовалось. Тогда вызывается Options.OnBlank (Windows: переход на программную отрисовку).
//
// Заодно это сторож главного потока: если окно не ответило за 15 секунд, в журнал окна пишутся стеки
// всех горутин — по ним видно, на чём оно зависло.

// checkRender — окно только что показано: проверить отрисовку (один раз за запуск). Поток окна.
func (u *UI) checkRender() {
	u.visible = true
	if u.renderChecking {
		return
	}
	u.renderChecking = true
	go u.renderLoop()
}

func (u *UI) renderLoop() {
	var last string
	// Две попытки: первый кадр может ещё не дойти до экрана (медленный запуск, программная отрисовка)
	for _, wait := range []time.Duration{3 * time.Second, 4 * time.Second} {
		time.Sleep(wait)
		res := make(chan string, 1)
		fyne.Do(func() {
			if !u.visible {
				// Окно спрятали в трей — проверить при следующем показе
				u.renderChecking = false
				res <- "hidden"
				return
			}
			res <- frameVerdict(u.win.Canvas().Capture())
		})
		var v string
		select {
		case v = <-res:
		case <-time.After(15 * time.Second):
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			log.Printf("окно не отвечает 15 секунд, стеки:\n%s", buf[:n])
			return
		}
		switch v {
		case "hidden":
			return
		case "ok":
			log.Printf("окно отрисовано")
			return
		}
		last = v
	}
	log.Printf("окно пустое: %s", last)
	if u.o.OnBlank != nil {
		u.o.OnBlank(last)
	}
}

// frameVerdict — "ok", если в кадре есть интерфейс, иначе описание пустого кадра.
func frameVerdict(img image.Image) string {
	if img == nil {
		return "кадр не прочитан"
	}
	b := img.Bounds()
	if b.Dx() < 8 || b.Dy() < 8 {
		return fmt.Sprintf("кадр %dx%d", b.Dx(), b.Dy())
	}
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	near := func(a, c uint32) bool { return a>>8 <= c>>8+3 && c>>8 <= a>>8+3 }
	// Сетка с шагом в 4 пикселя: текст и границы карточек в неё попадают
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if !near(r, r0) || !near(g, g0) || !near(bl, b0) {
				return "ok"
			}
		}
	}
	return fmt.Sprintf("кадр %dx%d целиком цвета #%02x%02x%02x", b.Dx(), b.Dy(), r0>>8, g0>>8, b0>>8)
}
