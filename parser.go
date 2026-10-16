package main

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// Entry — одна строка таблицы №4 с числовым результатом.
type Entry struct {
	Component string // номер компонента/материала (первая колонка), если удалось определить
	Element   string // текст колонки «Определяемая характеристика»
	MgKg      string // значение в мг/кг, как в таблице
	Percent   string // значение в %, как в таблице
	Interp    string // интерпретация результата (BL, OL, X …)
}

// Result — всё, что программа достаёт из таблицы №4.
type Result struct {
	Lead    []Entry // свинец с числовыми значениями
	Bromine []Entry // общий бром с числовыми значениями

	TotalRows    int // всего строк с результатами
	NumericRows  int // строк, где есть числовое значение (по всем элементам)
	NumericCells int // ячеек мг/кг и % с числовыми значениями
	BLRows       int // строк с интерпретацией BL

	// NumericByElement — количество строк с числовыми значениями по каждому
	// элементу, в порядке первого появления в таблице.
	NumericByElement []ElementCount
}

type ElementCount struct {
	Name  string
	Count int
}

// ---------------------------------------------------------------------------
// Минимальное XML-дерево документа.

type node struct {
	space, local string
	attrs        []xml.Attr
	children     []*node
	text         string // содержимое w:t
}

func (n *node) is(local string) bool { return n.space == wNS && n.local == local }

func (n *node) attr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Local == local && (a.Name.Space == wNS || a.Name.Space == "") {
			return a.Value, true
		}
	}
	return "", false
}

func (n *node) child(local string) *node {
	for _, c := range n.children {
		if c.is(local) {
			return c
		}
	}
	return nil
}

// find ищет первый потомок с заданным именем (поиск в глубину).
func (n *node) find(local string) *node {
	for _, c := range n.children {
		if c.is(local) {
			return c
		}
		if f := c.find(local); f != nil {
			return f
		}
	}
	return nil
}

func parseXML(r io.Reader) (*node, error) {
	dec := xml.NewDecoder(r)
	root := &node{}
	stack := []*node{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{space: t.Name.Space, local: t.Name.Local, attrs: t.Attr}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			cur := stack[len(stack)-1]
			if cur.is("t") {
				cur.text += string(t)
			}
		}
	}
	return root, nil
}

// ---------------------------------------------------------------------------
// Извлечение текста.

// symbolChars — символы шрифта Symbol, которые встречаются в протоколах.
var symbolChars = map[int]string{0xB1: "±", 0xB3: "≥", 0xA3: "≤", 0xB0: "°", 0xD7: "×", 0x2D: "−"}

func writeText(n *node, sb *strings.Builder) {
	switch {
	case n.space == wNS:
		switch n.local {
		case "t":
			sb.WriteString(n.text)
			return
		case "tab":
			sb.WriteString(" ")
			return
		case "br", "cr":
			sb.WriteString("\n")
			return
		case "noBreakHyphen":
			sb.WriteString("-")
			return
		case "sym":
			if v, ok := n.attr("char"); ok {
				if code, err := strconv.ParseInt(v, 16, 32); err == nil {
					if code >= 0xF000 {
						code -= 0xF000
					}
					if s, ok := symbolChars[int(code)]; ok {
						sb.WriteString(s)
					} else {
						sb.WriteRune(rune(code))
					}
				}
			}
			return
		case "pPr", "rPr", "tcPr", "trPr", "tblPr", "delText", "instrText", "txbxContent":
			return
		}
	case n.local == "Fallback": // mc:Fallback дублирует содержимое mc:Choice
		return
	}
	for _, c := range n.children {
		writeText(c, sb)
	}
}

func paragraphText(p *node) string {
	var sb strings.Builder
	writeText(p, &sb)
	return sb.String()
}

func cellText(tc *node) string {
	var parts []string
	for _, c := range tc.children {
		if c.is("p") || c.is("tbl") || c.is("sdt") {
			parts = append(parts, paragraphText(c))
		}
	}
	return cleanText(strings.Join(parts, "\n"))
}

// cleanText схлопывает переводы строк и лишние пробелы, сохраняя сами символы.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	return strings.ToLower(cleanText(s))
}

// ---------------------------------------------------------------------------
// Блоки тела документа: абзацы и таблицы (с раскрытием w:sdt и т.п.).

func bodyBlocks(n *node, out []*node) []*node {
	for _, c := range n.children {
		switch {
		case c.is("p"), c.is("tbl"):
			out = append(out, c)
		case c.is("sdt"):
			if sc := c.child("sdtContent"); sc != nil {
				out = bodyBlocks(sc, out)
			}
		case c.is("customXml"), c.is("ins"), c.is("smartTag"):
			out = bodyBlocks(c, out)
		}
	}
	return out
}

var (
	reTable4Caption = regexp.MustCompile(`^таблица\s*(№\s*)?4(\D|$)`)
	reContinuation4 = regexp.MustCompile(`^(продолжение|окончание)\s+табл\S*\s*(№\s*)?4(\D|$)`)
	reAnyCaption    = regexp.MustCompile(`^(таблица|продолжение|окончание)\s`)
)

// findTable4 возвращает таблицу №4 (и её продолжения, если таблица разорвана).
func findTable4(body *node) []*node {
	blocks := bodyBlocks(body, nil)

	for i, b := range blocks {
		if !b.is("p") || !reTable4Caption.MatchString(normalize(paragraphText(b))) {
			continue
		}
		var tables []*node
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].is("tbl") {
				tables = append(tables, blocks[j])
				// продолжение: пустые абзацы или подпись «Продолжение таблицы №4»
				k := j + 1
				for k < len(blocks) && blocks[k].is("p") {
					t := normalize(paragraphText(blocks[k]))
					if t != "" && !reContinuation4.MatchString(t) {
						break
					}
					k++
				}
				if k < len(blocks) && blocks[k].is("tbl") && sameGrid(blocks[j], blocks[k]) {
					j = k - 1
					continue
				}
				break
			}
			if t := normalize(paragraphText(blocks[j])); reAnyCaption.MatchString(t) && !reTable4Caption.MatchString(t) {
				break // дошли до подписи другой таблицы
			}
		}
		if len(tables) > 0 {
			return tables
		}
	}

	// Подписи нет — ищем таблицу по содержимому.
	for _, b := range blocks {
		if !b.is("tbl") {
			continue
		}
		var sb strings.Builder
		writeText(b, &sb)
		t := normalize(sb.String())
		if strings.Contains(t, "мг/кг") && (strings.Contains(t, "свинец") || strings.Contains(t, "бром")) {
			return []*node{b}
		}
	}
	return nil
}

func gridCols(tbl *node) int {
	if g := tbl.child("tblGrid"); g != nil {
		n := 0
		for _, c := range g.children {
			if c.is("gridCol") {
				n++
			}
		}
		return n
	}
	return 0
}

func sameGrid(a, b *node) bool { return gridCols(a) == gridCols(b) }

// ---------------------------------------------------------------------------
// Разбор строк таблицы.

type cell struct {
	col       int // номер колонки сетки (с учётом gridSpan)
	tc        *node
	text      string
	vContinue bool // продолжение вертикально объединённой ячейки
	numbered  bool // ячейка содержит абзац с автонумерацией
}

func tableRows(tbl *node) [][]cell {
	var rows [][]cell
	var collect func(n *node)
	collect = func(n *node) {
		for _, c := range n.children {
			switch {
			case c.is("tr"):
				rows = append(rows, rowCells(c))
			case c.is("sdt"):
				if sc := c.child("sdtContent"); sc != nil {
					collect(sc)
				}
			case c.is("customXml"), c.is("ins"):
				collect(c)
			}
		}
	}
	collect(tbl)
	return rows
}

func rowCells(tr *node) []cell {
	var cells []cell
	col := 0
	if trPr := tr.child("trPr"); trPr != nil {
		if gb := trPr.child("gridBefore"); gb != nil {
			if v, ok := gb.attr("val"); ok {
				if n, err := strconv.Atoi(v); err == nil {
					col += n
				}
			}
		}
	}
	var add func(n *node)
	add = func(n *node) {
		for _, c := range n.children {
			switch {
			case c.is("tc"):
				ce := cell{col: col, tc: c, text: cellText(c)}
				span := 1
				if pr := c.child("tcPr"); pr != nil {
					if gs := pr.child("gridSpan"); gs != nil {
						if v, ok := gs.attr("val"); ok {
							if n, err := strconv.Atoi(v); err == nil && n > 0 {
								span = n
							}
						}
					}
					if vm := pr.child("vMerge"); vm != nil {
						if v, ok := vm.attr("val"); !ok || v == "continue" {
							ce.vContinue = true
						}
					}
				}
				for _, p := range c.children {
					if p.is("p") {
						if pPr := p.child("pPr"); pPr != nil && pPr.child("numPr") != nil {
							ce.numbered = true
						}
					}
				}
				cells = append(cells, ce)
				col += span
			case c.is("sdt"):
				if sc := c.child("sdtContent"); sc != nil {
					add(sc)
				}
			case c.is("customXml"):
				add(c)
			}
		}
	}
	add(tr)
	return cells
}

func cellAt(cells []cell, col int) *cell {
	for i := range cells {
		if cells[i].col == col {
			return &cells[i]
		}
	}
	return nil
}

func textAt(cells []cell, col int) string {
	if c := cellAt(cells, col); c != nil {
		return c.text
	}
	return ""
}

func isHeaderRow(cells []cell) bool {
	for _, c := range cells {
		t := normalize(c.text)
		if strings.Contains(t, "мг/кг") || strings.Contains(t, "определяемая характеристика") ||
			strings.Contains(t, "интерпретация") || strings.Contains(t, "результат испытаний") {
			return true
		}
	}
	return false
}

// IsNumeric — значение считается числовым, если в нём есть цифры и нет букв
// и знаков сравнения: «127 ± 15» и «0,0127 ± 0,0015» — да, «Менее 10» — нет.
func IsNumeric(s string) bool {
	hasDigit := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsLetter(r):
			return false
		case strings.ContainsRune("<>≤≥", r):
			return false
		}
	}
	return hasDigit
}

func isLead(name string) bool {
	n := normalize(name)
	return strings.Contains(n, "свин") || strings.Contains(n, "(pb)") || n == "pb"
}

// isBromine — общий бром; полибромированные бифенилы/эфиры (ПББ, ПБДЭ) — не он.
func isBromine(name string) bool {
	n := normalize(name)
	if strings.Contains(n, "полибром") || strings.Contains(n, "бромирован") {
		return false
	}
	return strings.Contains(n, "бром") || strings.Contains(n, "(br)") || n == "br"
}

// elementTitle — короткое имя элемента для статистики.
func elementTitle(name string) string {
	switch {
	case isLead(name):
		return "свинец"
	case isBromine(name):
		return "общий бром"
	}
	n := cleanText(name)
	if i := strings.Index(n, "("); i > 0 {
		n = strings.TrimSpace(n[:i])
	}
	return strings.ToLower(n)
}

type columns struct{ comp, name, mg, pct, interp int }

func detectColumns(rows [][]cell) columns {
	c := columns{comp: 0, name: 1, mg: 2, pct: 3, interp: 4}
	for i, cells := range rows {
		if i > 6 {
			break
		}
		for _, ce := range cells {
			t := normalize(ce.text)
			switch {
			case t == "мг/кг" || strings.HasPrefix(t, "мг/кг"):
				c.mg = ce.col
			case t == "%" || t == "% масс." || t == "масс. %":
				c.pct = ce.col
			case strings.Contains(t, "интерпретация"):
				c.interp = ce.col
			case strings.Contains(t, "определяемая характеристика") || strings.Contains(t, "показатель"):
				c.name = ce.col
			case strings.Contains(t, "компонент") || strings.Contains(t, "материал"):
				c.comp = ce.col
			}
		}
	}
	return c
}

// ParseDocx открывает .docx и разбирает таблицу №4.
func ParseDocx(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл: %v", err)
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		if strings.HasSuffix(strings.ToLower(path), ".doc") {
			return nil, errors.New("файл в старом формате .doc — откройте его в Word и сохраните как .docx")
		}
		return nil, errors.New("это не документ Word .docx (файл другого формата или повреждён)")
	}

	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return nil, errors.New("в файле нет word/document.xml — это не документ Word .docx")
	}
	rc, err := doc.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return ParseDocumentXML(rc)
}

// ParseDocumentXML разбирает содержимое word/document.xml.
func ParseDocumentXML(r io.Reader) (*Result, error) {
	root, err := parseXML(r)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения XML документа: %v", err)
	}
	body := root.find("body")
	if body == nil {
		return nil, errors.New("в документе не найдено тело (w:body)")
	}
	tables := findTable4(body)
	if len(tables) == 0 {
		return nil, errors.New("в документе не найдена таблица №4")
	}

	var rows [][]cell
	for _, t := range tables {
		rows = append(rows, tableRows(t)...)
	}
	cols := detectColumns(rows)

	res := &Result{}
	counts := map[string]int{}
	var order []string
	component := ""
	counter := 0

	for _, cells := range rows {
		if isHeaderRow(cells) {
			continue
		}
		if c := cellAt(cells, cols.comp); c != nil && !c.vContinue {
			switch {
			case c.text != "":
				component = strings.TrimSuffix(c.text, ".")
			case c.numbered:
				counter++
				component = strconv.Itoa(counter)
			default:
				component = ""
			}
		}

		name := textAt(cells, cols.name)
		mg := textAt(cells, cols.mg)
		pct := textAt(cells, cols.pct)
		interp := textAt(cells, cols.interp)
		if name == "" && mg == "" && pct == "" && interp == "" {
			continue
		}
		res.TotalRows++
		if strings.EqualFold(strings.TrimSpace(interp), "BL") {
			res.BLRows++
		}

		mgNum, pctNum := IsNumeric(mg), IsNumeric(pct)
		if mgNum {
			res.NumericCells++
		}
		if pctNum {
			res.NumericCells++
		}
		if !mgNum && !pctNum {
			continue
		}
		res.NumericRows++
		title := elementTitle(name)
		if _, ok := counts[title]; !ok {
			order = append(order, title)
		}
		counts[title]++

		e := Entry{Component: component, Element: name, MgKg: mg, Percent: pct, Interp: interp}
		switch {
		case isLead(name):
			res.Lead = append(res.Lead, e)
		case isBromine(name):
			res.Bromine = append(res.Bromine, e)
		}
	}
	// свинец и общий бром — первыми, остальные элементы — в порядке появления
	order = append([]string{"свинец", "общий бром"}, order...)
	seen := map[string]bool{}
	for _, name := range order {
		if seen[name] || counts[name] == 0 {
			continue
		}
		seen[name] = true
		res.NumericByElement = append(res.NumericByElement, ElementCount{Name: name, Count: counts[name]})
	}
	return res, nil
}
