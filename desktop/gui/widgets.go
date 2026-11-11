package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---------- Нажимаемая область ----------

// tapBox — любой элемент, по которому можно щёлкнуть (строка сервера, карточка профиля).
type tapBox struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	onTap    func()
	hoverBg  *canvas.Rectangle
	hovering bool
}

func newTapBox(content fyne.CanvasObject, onTap func()) *tapBox {
	t := &tapBox{content: content, onTap: onTap, hoverBg: canvas.NewRectangle(color.Transparent)}
	t.hoverBg.CornerRadius = 10
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(t.hoverBg, t.content))
}

func (t *tapBox) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapBox) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *tapBox) MouseIn(*desktop.MouseEvent) {
	t.hoverBg.FillColor = theme.Color(theme.ColorNameHover)
	t.hoverBg.Refresh()
}

func (t *tapBox) MouseMoved(*desktop.MouseEvent) {}

func (t *tapBox) MouseOut() {
	t.hoverBg.FillColor = color.Transparent
	t.hoverBg.Refresh()
}

// ---------- Карточка, подписи, значки ----------

// card — белая (в тёмной теме — тёмная) подложка со скруглением, как MaterialCardView.
func card(objs ...fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(themePal().surface)
	bg.CornerRadius = 14
	bg.StrokeColor = themePal().outline
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(container.NewVBox(objs...)))
}

// sectionHead — заголовок раздела списка: «МОИ СЕРВЕРЫ  3».
func sectionHead(title string, count string, extra ...fyne.CanvasObject) fyne.CanvasObject {
	t := canvas.NewText(title, themePal().text2)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 12
	c := canvas.NewText(count, themePal().text2)
	c.TextSize = 12
	objs := []fyne.CanvasObject{t, layout.NewSpacer()}
	objs = append(objs, extra...)
	objs = append(objs, c)
	return container.NewPadded(container.NewHBox(objs...))
}

func title(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}

func subtitle(text string) *canvas.Text {
	t := canvas.NewText(text, themePal().text2)
	t.TextSize = 12
	return t
}

// wrapped — серый поясняющий текст с переносом строк.
func wrapped(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	return l
}

// rowText — заголовок и подзаголовок одной строки списка. Подзаголовок переносится, если длинный.
func rowText(head, sub string) fyne.CanvasObject {
	h := title(head)
	if sub == "" {
		return h
	}
	s := widget.NewLabel(sub)
	s.Wrapping = fyne.TextWrapWord
	s.Importance = widget.LowImportance
	s.SizeName = theme.SizeNameCaptionText
	return container.New(&tightVBox{}, h, s)
}

// tightVBox — VBox без зазора между строками (заголовок и подпись вплотную).
type tightVBox struct{}

func (tightVBox) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		s := o.MinSize()
		w = max(w, s.Width)
		h += s.Height
	}
	return fyne.NewSize(w, h-theme.Padding()*float32(len(objs)-1))
}

func (tightVBox) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objs {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h - theme.Padding()
	}
}

// chip — плашка с текстом цвета c (пинг, «нет ответа»).
func chip(text string, c color.Color) fyne.CanvasObject {
	t := canvas.NewText(text, c)
	t.TextSize = 12
	t.TextStyle = fyne.TextStyle{Bold: true}
	r, g, b, _ := c.RGBA()
	bg := canvas.NewRectangle(color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0x26})
	bg.CornerRadius = 9
	return container.NewCenter(container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(2, 2, 8, 8), t)))
}

// dot — цветная точка состояния.
func dot(c color.Color, size float32) fyne.CanvasObject {
	d := canvas.NewCircle(c)
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(size, size), d))
}

// iconBox — значок в цветном квадрате, как в списках Android-версии.
func iconBox(name string, tint color.NRGBA, tinted bool) fyne.CanvasObject {
	bg := canvas.NewRectangle(themePal().surfaceVariant)
	var res fyne.Resource = icon(name)
	if tinted {
		bg.FillColor = withAlpha(tint, 0x2A)
		res = coloredIcon(name, hexColor(tint))
	}
	bg.CornerRadius = 10
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	return container.NewGridWrap(fyne.NewSize(36, 36), container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), img)))
}

func hexColor(c color.NRGBA) string {
	const h = "0123456789ABCDEF"
	b := []byte{'#', h[c.R>>4], h[c.R&15], h[c.G>>4], h[c.G&15], h[c.B>>4], h[c.B&15]}
	return string(b)
}

// iconButton — маленькая кнопка-значок (ещё, удалить, обновить).
func iconButton(res fyne.Resource, tip string, f func()) *widget.Button {
	b := widget.NewButtonWithIcon("", res, f)
	b.Importance = widget.LowImportance
	_ = tip
	return b
}

// showMenu — всплывающее меню под элементом.
func showMenu(w fyne.Window, anchor fyne.CanvasObject, items ...*fyne.MenuItem) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	pos = pos.Add(fyne.NewPos(0, anchor.Size().Height))
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), w.Canvas(), pos)
}

// ---------- Кнопка подключения ----------

// powerButton — круглая кнопка с ореолом: серая — выключено, оранжевая — подключение, зелёная — подключено.
type powerButton struct {
	widget.BaseWidget
	halo, core *canvas.Circle
	img        *canvas.Image
	onTap      func()
	state      string
}

func newPowerButton(onTap func()) *powerButton {
	p := &powerButton{halo: canvas.NewCircle(color.Transparent), core: canvas.NewCircle(color.Transparent),
		img: canvas.NewImageFromResource(icon("power")), onTap: onTap}
	p.img.FillMode = canvas.ImageFillContain
	p.ExtendBaseWidget(p)
	p.SetState("stopped")
	return p
}

func (p *powerButton) SetState(state string) {
	pal := themePal()
	p.state = state
	p.core.StrokeWidth = 0
	switch state {
	case "connected":
		p.halo.FillColor, p.core.FillColor = withAlpha(colOK, 0x30), colOK
		p.img.Resource = coloredIcon("power", "#FFFFFF")
	case "connecting":
		p.halo.FillColor, p.core.FillColor = withAlpha(colWait, 0x30), colWait
		p.img.Resource = coloredIcon("power", "#FFFFFF")
	case "error":
		p.halo.FillColor, p.core.FillColor = withAlpha(colError, 0x24), pal.surface
		p.img.Resource = coloredIcon("power", hexColor(colError))
	default:
		p.halo.FillColor, p.core.FillColor = pal.surfaceVariant, pal.surface
		p.core.StrokeColor, p.core.StrokeWidth = pal.outline, 1
		p.img.Resource = coloredIcon("power", hexColor(pal.accent))
	}
	p.Refresh()
}

func (p *powerButton) CreateRenderer() fyne.WidgetRenderer {
	return &powerRenderer{p: p}
}

func (p *powerButton) Tapped(*fyne.PointEvent) {
	if p.onTap != nil {
		p.onTap()
	}
}

func (p *powerButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

type powerRenderer struct{ p *powerButton }

func (r *powerRenderer) Layout(s fyne.Size) {
	d := min(s.Width, s.Height)
	off := fyne.NewPos((s.Width-d)/2, (s.Height-d)/2)
	r.p.halo.Move(off)
	r.p.halo.Resize(fyne.NewSize(d, d))
	in := d * 0.14
	r.p.core.Move(off.Add(fyne.NewPos(in, in)))
	r.p.core.Resize(fyne.NewSize(d-2*in, d-2*in))
	ic := d * 0.34
	r.p.img.Move(off.Add(fyne.NewPos((d-ic)/2, (d-ic)/2)))
	r.p.img.Resize(fyne.NewSize(ic, ic))
}

func (r *powerRenderer) MinSize() fyne.Size { return fyne.NewSize(104, 104) }
func (r *powerRenderer) Refresh() {
	r.p.halo.Refresh()
	r.p.core.Refresh()
	r.p.img.Refresh()
}
func (r *powerRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.p.halo, r.p.core, r.p.img}
}
func (r *powerRenderer) Destroy() {}
