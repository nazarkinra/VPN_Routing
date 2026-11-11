// Значок ПК-версии из того же рисунка, что у Android-приложения (assets/icon-source.webp — исходник
// 2000×1116 с плиткой посередине). Вырезает плитку, делает прозрачными углы за скруглением и сохраняет
// assets/vpnrouting.png 512×512 — из него собираются значок exe, ярлык Linux и значок в трее.
//
//	go run ./tools/icon
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Границы плитки в исходнике (найдены по перепаду яркости) и радиус её скругления
var tile = image.Rect(661, 203, 1340, 903)

const radius = 112.0

func main() {
	src := "assets/icon-source.webp"
	out := "assets/vpnrouting.png"
	if len(os.Args) > 1 {
		src = os.Args[1]
	}
	f, err := os.Open(src)
	check(err)
	img, _, err := image.Decode(f)
	check(err)
	f.Close()

	// Плитка с прозрачными углами
	w, h := tile.Dx(), tile.Dy()
	cut := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := roundedAlpha(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), radius)
			if a <= 0 {
				continue
			}
			c := color.NRGBAModel.Convert(img.At(tile.Min.X+x, tile.Min.Y+y)).(color.NRGBA)
			c.A = uint8(float64(c.A) * a)
			cut.SetNRGBA(x, y, c)
		}
	}
	// В квадрат: плитка чуть выше, чем шире (объёмный нижний край) — по бокам прозрачные поля
	side := max(w, h)
	square := image.NewNRGBA(image.Rect(0, 0, side, side))
	draw.Draw(square, image.Rect((side-w)/2, (side-h)/2, (side-w)/2+w, (side-h)/2+h), cut, image.Point{}, draw.Src)

	dst := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Src, nil)
	o, err := os.Create(out)
	check(err)
	check(png.Encode(o, dst))
	check(o.Close())
	fmt.Println("готово:", out)
}

// roundedAlpha — покрытие точки скруглённым прямоугольником w×h со сглаживанием в 1 пиксель.
func roundedAlpha(x, y, w, h, r float64) float64 {
	dx := math.Max(0, math.Max(r-x, x-(w-r)))
	dy := math.Max(0, math.Max(r-y, y-(h-r)))
	d := math.Hypot(dx, dy) - r // >0 — снаружи
	return math.Max(0, math.Min(1, 0.5-d))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}
