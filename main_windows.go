package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const appTitle = "Таблица №4 — свинец и общий бром"

var (
	colorPage       = walk.RGB(244, 246, 250)
	colorPanel      = walk.RGB(232, 236, 243)
	colorHeading    = walk.RGB(36, 52, 82)
	colorMuted      = walk.RGB(110, 118, 132)
	colorTileBg     = walk.RGB(255, 255, 255)
	colorTileBorder = walk.RGB(204, 211, 222)
	colorTileText   = walk.RGB(28, 33, 43)
	colorLitBg      = walk.RGB(255, 221, 87)
	colorLitBorder  = walk.RGB(232, 170, 0)
	colorLitText    = walk.RGB(48, 34, 0)
	colorLastBorder = walk.RGB(196, 98, 0)
)

// tile — плитка со значением. Нажатие копирует text в буфер обмена.
type tile struct {
	w    *walk.CustomWidget
	text string
	lit  bool
}

type app struct {
	mw        *walk.MainWindow
	fileLabel *walk.Label
	scroll    *walk.ScrollView
	content   *walk.Composite
	statNum   *walk.Label
	statBL    *walk.Label
	statInfo  *walk.Label
	status    *walk.Label

	tiles   []*tile
	last    *tile
	lastDir string

	headFont, colFont, tileFont, smallFont *walk.Font
	brushes                                map[walk.Color]*walk.SolidColorBrush
}

func main() {
	a := &app{brushes: map[walk.Color]*walk.SolidColorBrush{}}
	a.headFont, _ = walk.NewFont("Segoe UI", 15, walk.FontBold)
	a.colFont, _ = walk.NewFont("Segoe UI", 10, walk.FontBold)
	a.tileFont, _ = walk.NewFont("Segoe UI", 13, walk.FontBold)
	a.smallFont, _ = walk.NewFont("Segoe UI", 8, 0)

	err := MainWindow{
		AssignTo:    &a.mw,
		Title:       appTitle,
		Font:        Font{Family: "Segoe UI", PointSize: 10},
		Size:        Size{Width: 720, Height: 860},
		MinSize:     Size{Width: 520, Height: 480},
		Layout:      VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 8}, Spacing: 8},
		OnDropFiles: a.onDrop,
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 10},
				Children: []Widget{
					PushButton{Text: "Открыть файл…", MinSize: Size{Width: 130, Height: 34}, OnClicked: a.openDialog},
					Label{AssignTo: &a.fileLabel, Text: "Файл не выбран", EllipsisMode: EllipsisEnd, TextColor: colorMuted},
					HSpacer{},
					PushButton{Text: "Сброс", MinSize: Size{Width: 110, Height: 34}, OnClicked: a.reset,
						ToolTipText: "Погасить все подсвеченные плитки"},
				},
			},
			ScrollView{
				AssignTo:   &a.scroll,
				Background: SolidColorBrush{Color: colorPage},
				Layout:     VBox{MarginsZero: true},
				Children: []Widget{
					Composite{
						AssignTo: &a.content,
						Layout:   VBox{Margins: Margins{Left: 14, Top: 12, Right: 14, Bottom: 14}, Spacing: 8},
					},
					VSpacer{},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: colorPanel},
				Layout:     Grid{Columns: 3, Margins: Margins{Left: 12, Top: 8, Right: 12, Bottom: 8}, Spacing: 4},
				Children: []Widget{
					Label{Text: "СТАТИСТИКА ТАБЛИЦЫ №4", ColumnSpan: 3, Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}, TextColor: colorHeading},
					Label{Text: "Числовых значений в таблице:"},
					Label{AssignTo: &a.statNum, Text: "—", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
					HSpacer{},
					Label{Text: "Строк со значением BL:"},
					Label{AssignTo: &a.statBL, Text: "—", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
					HSpacer{},
					Label{AssignTo: &a.statInfo, ColumnSpan: 3, Text: "", TextColor: colorMuted},
				},
			},
			Label{AssignTo: &a.status, Text: "Нажмите на плитку — значение скопируется в буфер обмена.", TextColor: colorMuted},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, appTitle, "Не удалось создать окно: "+err.Error(), walk.MsgBoxIconError)
		return
	}

	if icon, err := walk.NewIconFromResource("APP"); err == nil {
		a.mw.SetIcon(icon)
	}

	speedUpWheel(a.scroll)
	a.showPlaceholder()
	if len(os.Args) > 1 {
		a.load(os.Args[1])
	}
	a.mw.Run()
}

func (a *app) brush(c walk.Color) *walk.SolidColorBrush {
	if b, ok := a.brushes[c]; ok {
		return b
	}
	b, _ := walk.NewSolidColorBrush(c)
	a.brushes[c] = b
	return b
}

func (a *app) onDrop(files []string) {
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f), ".docx") {
			a.load(f)
			return
		}
	}
	if len(files) > 0 {
		a.load(files[0])
	}
}

func (a *app) openDialog() {
	dlg := walk.FileDialog{
		Title:          "Выберите протокол испытаний (.docx)",
		Filter:         "Документы Word (*.docx)|*.docx|Все файлы (*.*)|*.*",
		InitialDirPath: a.lastDir,
	}
	ok, err := dlg.ShowOpen(a.mw)
	if err != nil || !ok {
		return
	}
	a.load(dlg.FilePath)
}

func (a *app) load(path string) {
	a.mw.SetCursor(walk.CursorWait())
	res, err := ParseDocx(path)
	a.mw.SetCursor(nil)
	if err != nil {
		walk.MsgBox(a.mw, appTitle, "Файл: "+filepath.Base(path)+"\n\n"+err.Error(), walk.MsgBoxIconError)
		return
	}
	a.lastDir = filepath.Dir(path)
	a.fileLabel.SetText(path)
	a.fileLabel.SetTextColor(colorHeading)
	a.mw.SetTitle(filepath.Base(path) + " — " + appTitle)
	a.show(res)
}

// clearContent удаляет все виджеты из области с плитками.
func (a *app) clearContent() {
	a.tiles = nil
	a.last = nil
	children := a.content.Children()
	for children.Len() > 0 {
		children.At(0).Dispose()
	}
}

func (a *app) showPlaceholder() {
	a.content.SetSuspended(true)
	defer a.content.SetSuspended(false)
	a.clearContent()
	a.addNote("Откройте протокол испытаний (.docx) кнопкой «Открыть файл…» или перетащите файл в это окно.")
}

// addNote добавляет поясняющий текст с переносом строк. TextLabel, в отличие от
// Label, сообщает раскладке «высоту по ширине», и ScrollView тогда оставляет
// место под вертикальную полосу прокрутки, не пряча под неё правую колонку.
func (a *app) addNote(text string) {
	l, _ := walk.NewTextLabel(a.content)
	l.SetMinMaxSize(walk.Size{Width: 240}, walk.Size{}) // без минимальной ширины текст не переносится
	l.SetText(text)
	l.SetTextColor(colorMuted)
}

func (a *app) show(res *Result) {
	a.content.SetSuspended(true)
	a.clearContent()
	a.addNote("Нажмите на плитку — значение скопируется в буфер обмена, а плитка будет подсвечена до нажатия «Сброс».")
	a.addSection("СВИНЕЦ", res.Lead)
	a.addSpacer(10)
	a.addSection("ОБЩИЙ БРОМ", res.Bromine)
	a.content.SetSuspended(false)

	a.statNum.SetText(fmt.Sprint(res.NumericRows))
	a.statBL.SetText(fmt.Sprintf("%d  (всего строк с результатами: %d)", res.BLRows, res.TotalRows))
	var parts []string
	for _, e := range res.NumericByElement {
		parts = append(parts, fmt.Sprintf("%s — %d", e.Name, e.Count))
	}
	info := ""
	if len(parts) > 0 {
		info = "По элементам: " + strings.Join(parts, ", ") + ".  "
	}
	info += fmt.Sprintf("Ячеек с числами (мг/кг и %%): %d.", res.NumericCells)
	a.statInfo.SetText(info)
	a.status.SetText("Нажмите на плитку — значение скопируется в буфер обмена.")
	a.scroll.Invalidate()
}

func (a *app) addSpacer(height int) {
	c, _ := walk.NewComposite(a.content)
	c.SetMinMaxSize(walk.Size{Width: 1, Height: height}, walk.Size{Width: 0, Height: height})
}

func (a *app) addSection(title string, entries []Entry) {
	head, _ := walk.NewLabel(a.content)
	head.SetFont(a.headFont)
	head.SetTextColor(colorHeading)
	head.SetText(fmt.Sprintf("%s   (%d)", title, len(entries)))

	if len(entries) == 0 {
		a.addNote("Числовых значений в таблице №4 нет.")
		return
	}

	grid, _ := walk.NewComposite(a.content)
	gl := walk.NewGridLayout()
	gl.SetMargins(walk.Margins{})
	gl.SetSpacing(8)
	grid.SetLayout(gl)

	for col, name := range []string{"мг/кг", "%"} {
		l, _ := walk.NewLabel(grid)
		l.SetText(name)
		l.SetFont(a.colFont)
		l.SetTextColor(colorMuted)
		l.SetTextAlignment(walk.AlignCenter)
		gl.SetRange(l, walk.Rectangle{X: col + 1, Y: 0, Width: 1, Height: 1})
	}
	gl.SetColumnStretchFactor(1, 1)
	gl.SetColumnStretchFactor(2, 1)

	for i, e := range entries {
		n, _ := walk.NewLabel(grid)
		if e.Component != "" {
			n.SetText("№ " + e.Component)
			n.SetToolTipText("Номер компонента/материала в таблице №4")
		}
		n.SetTextColor(colorMuted)
		n.SetTextAlignment(walk.AlignFar)
		gl.SetRange(n, walk.Rectangle{X: 0, Y: i + 1, Width: 1, Height: 1})

		gl.SetRange(a.newTile(grid, e.MgKg), walk.Rectangle{X: 1, Y: i + 1, Width: 1, Height: 1})
		gl.SetRange(a.newTile(grid, e.Percent), walk.Rectangle{X: 2, Y: i + 1, Width: 1, Height: 1})
	}
}

func (a *app) newTile(parent walk.Container, text string) walk.Widget {
	t := &tile{text: text}
	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(c *walk.Canvas, _ walk.Rectangle) error {
		return a.paintTile(t, c)
	})
	if err != nil {
		return nil
	}
	t.w = cw
	cw.SetClearsBackground(false)
	cw.SetInvalidatesOnResize(true)
	cw.SetPaintMode(walk.PaintBuffered)
	cw.SetMinMaxSize(walk.Size{Width: 150, Height: 50}, walk.Size{Width: 0, Height: 50})
	cw.SetCursor(walk.CursorHand())
	cw.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.click(t)
		}
	})
	a.tiles = append(a.tiles, t)
	return cw
}

func (a *app) paintTile(t *tile, c *walk.Canvas) error {
	b := t.w.ClientBoundsPixels()
	scale := func(v int) int { return v * c.DPI() / 96 }

	c.FillRectanglePixels(a.brush(colorPage), b)

	bg, border, fg, bw := colorTileBg, colorTileBorder, colorTileText, scale(1)
	if t.lit {
		bg, border, fg = colorLitBg, colorLitBorder, colorLitText
	}
	if t == a.last {
		border, bw = colorLastBorder, scale(3)
	}

	radius := scale(12)
	c.FillRoundedRectanglePixels(a.brush(border), b, walk.Size{Width: radius, Height: radius})
	inner := walk.Rectangle{X: b.X + bw, Y: b.Y + bw, Width: b.Width - 2*bw, Height: b.Height - 2*bw}
	c.FillRoundedRectanglePixels(a.brush(bg), inner, walk.Size{Width: radius - bw, Height: radius - bw})

	c.DrawTextPixels(t.text, a.tileFont, fg, inner,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix)

	if t == a.last {
		mark := walk.Rectangle{X: inner.X, Y: inner.Y + scale(1), Width: inner.Width - scale(8), Height: scale(15)}
		c.DrawTextPixels("в буфере", a.smallFont, colorLastBorder, mark, walk.TextRight|walk.TextTop|walk.TextSingleLine)
	}
	return nil
}

func (a *app) click(t *tile) {
	if err := walk.Clipboard().SetText(t.text); err != nil {
		walk.MsgBox(a.mw, appTitle, "Не удалось скопировать в буфер обмена: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	prev := a.last
	t.lit = true
	a.last = t
	if prev != nil && prev != t {
		prev.w.Invalidate()
	}
	t.w.Invalidate()
	a.status.SetText("Скопировано в буфер обмена:  " + t.text)
}

func (a *app) reset() {
	for _, t := range a.tiles {
		if t.lit || t == a.last {
			t.lit = false
			t.w.Invalidate()
		}
	}
	a.last = nil
	if len(a.tiles) > 0 {
		a.status.SetText("Подсветка сброшена.")
	}
}

const wheelDelta = 120 // WHEEL_DELTA: один щелчок колеса

var (
	scrollOrigProc uintptr
	wheelAcc       int
)

// speedUpWheel: walk.ScrollView прокручивает на 20 px за щелчок колеса — для
// длинного списка плиток это слишком медленно. Подменяем оконную процедуру и
// прокручиваем на 3 строки за щелчок (как принято в Windows), с поддержкой
// тачпадов, присылающих дробные значения.
func speedUpWheel(sv *walk.ScrollView) {
	const step = wheelDelta / 3
	proc := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if msg != win.WM_MOUSEWHEEL {
			return win.CallWindowProc(scrollOrigProc, hwnd, msg, wParam, lParam)
		}
		wheelAcc += int(int16(win.HIWORD(uint32(wParam))))
		for wheelAcc >= step || wheelAcc <= -step {
			dir := 1
			if wheelAcc < 0 {
				dir = -1
			}
			wheelAcc -= dir * step
			wp := uintptr(uint32(uint16(int16(dir*wheelDelta)))<<16) | wParam&0xFFFF
			win.CallWindowProc(scrollOrigProc, hwnd, msg, wp, lParam)
		}
		return 0
	})
	scrollOrigProc = win.SetWindowLongPtr(sv.Handle(), win.GWLP_WNDPROC, proc)
}
