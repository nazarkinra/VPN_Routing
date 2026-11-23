package gui

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestFrameVerdict(t *testing.T) {
	white := image.NewRGBA(image.Rect(0, 0, 200, 150))
	for i := range white.Pix {
		white.Pix[i] = 0xff
	}
	if v := frameVerdict(white); v == "ok" || !strings.Contains(v, "#ffffff") {
		t.Fatalf("белый кадр: %q", v)
	}
	// Почти белый (шум сжатия, сглаживание) — всё ещё пустой
	white.Set(40, 40, color.RGBA{0xfd, 0xfe, 0xff, 0xff})
	if v := frameVerdict(white); v == "ok" {
		t.Fatal("почти белый кадр принят за интерфейс")
	}
	// Одна строчка текста — интерфейс есть
	for x := 20; x < 120; x++ {
		white.Set(x, 60, color.RGBA{0x20, 0x20, 0x20, 0xff})
	}
	if v := frameVerdict(white); v != "ok" {
		t.Fatalf("кадр с текстом: %q", v)
	}
	if v := frameVerdict(nil); v == "ok" {
		t.Fatal("нет кадра — не ok")
	}
	if v := frameVerdict(image.NewRGBA(image.Rect(0, 0, 0, 0))); v == "ok" {
		t.Fatal("пустой кадр 0x0 — не ok")
	}
}
