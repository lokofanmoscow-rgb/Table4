package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// row собирает строку таблицы. Первая ячейка "#" — автонумерация с началом
// вертикального объединения, "^" — продолжение объединённой ячейки.
func row(cells ...string) string {
	var sb strings.Builder
	sb.WriteString("<w:tr>")
	for i, c := range cells {
		switch {
		case i == 0 && c == "#":
			sb.WriteString(`<w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr>` +
				`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr></w:p></w:tc>`)
		case i == 0 && c == "^":
			sb.WriteString(`<w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc>`)
		default:
			sb.WriteString(`<w:tc><w:p><w:r><w:t xml:space="preserve">` + c + `</w:t></w:r></w:p></w:tc>`)
		}
	}
	sb.WriteString("</w:tr>")
	return sb.String()
}

func buildDoc(t *testing.T, body string) string {
	t.Helper()
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body + `</w:body></w:document>`
	path := filepath.Join(t.TempDir(), "test.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(xml))
	zw.Close()
	f.Close()
	return path
}

func para(s string) string { return "<w:p><w:r><w:t>" + s + "</w:t></w:r></w:p>" }

func TestParseTable4(t *testing.T) {
	header := row("№ компонента/ материала", "Определяемая характеристика (Показатель)", "мг/кг", "%", "Интерпретация результата")
	grid := `<w:tblGrid><w:gridCol/><w:gridCol/><w:gridCol/><w:gridCol/><w:gridCol/></w:tblGrid>`
	table3 := "<w:tbl>" + grid + header + row("1", "Свинец (Pb)", "999 ± 1", "0,0999 ± 0,0001", "BL") + "</w:tbl>"
	table4 := "<w:tbl>" + grid + header +
		row("#", "Свинец (Pb)", "Менее 10", "Менее 0,001", "BL") +
		row("^", "Общий бром", "Менее 10", "Менее 0,001", "BL") +
		row("#", "Свинец (Pb)", "127 ± 15", "0,0127 ± 0,0015", "BL") +
		row("^", "Ртуть (Hg)", "Менее 10", "Менее 0,001", "BL") +
		row("^", "Общий бром", "150 ± 21", "0,0150 ± 0,0021", "BL") +
		"</w:tbl>"
	table4cont := "<w:tbl>" + grid + header +
		row("#", "Свинец (Pb)", "57 ± 9", "0,0057 ± 0,0009", "OL") +
		row("^", "Общий бром", `118 </w:t></w:r><w:r><w:sym w:font="Symbol" w:char="F0B1"/></w:r><w:r><w:t xml:space="preserve"> 11`, "0,0118 ± 0,0011", "BL") +
		"</w:tbl>"
	body := para("Результаты испытаний представлены в Таблицах №№ 3, 4.") +
		para("Таблица №3") + table3 +
		para("Таблица №4") + table4 +
		para("") + para("Продолжение таблицы №4") + table4cont +
		para("Таблица №5") + "<w:tbl>" + grid + row("x", "Свинец (Pb)", "1 ± 1", "1 ± 1", "BL") + "</w:tbl>"

	res, err := ParseDocx(buildDoc(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lead) != 2 || res.Lead[0].MgKg != "127 ± 15" || res.Lead[0].Percent != "0,0127 ± 0,0015" ||
		res.Lead[0].Component != "2" || res.Lead[1].Component != "3" {
		t.Errorf("свинец: %+v", res.Lead)
	}
	if len(res.Bromine) != 2 || res.Bromine[0].MgKg != "150 ± 21" || res.Bromine[1].MgKg != "118 ± 11" ||
		res.Bromine[0].Component != "2" {
		t.Errorf("бром: %+v", res.Bromine)
	}
	if res.TotalRows != 7 || res.BLRows != 6 || res.NumericRows != 4 || res.NumericCells != 8 {
		t.Errorf("статистика: всего %d, BL %d, числовых строк %d, ячеек %d",
			res.TotalRows, res.BLRows, res.NumericRows, res.NumericCells)
	}
}

func TestNoTable4(t *testing.T) {
	if _, err := ParseDocx(buildDoc(t, para("Таблица №1")+"<w:tbl>"+row("a", "b")+"</w:tbl>")); err == nil {
		t.Error("ожидалась ошибка «таблица №4 не найдена»")
	}
}

func TestElementNames(t *testing.T) {
	if !isLead("Свинец (Pb)") || isLead("Общий бром") {
		t.Error("isLead")
	}
	if !isBromine("Общий бром") || !isBromine("Бром общий (Br)") || isBromine("Общий хром") ||
		isBromine("Полибромированные бифенилы (ПББ)") {
		t.Error("isBromine")
	}
}

func TestIsNumeric(t *testing.T) {
	for s, want := range map[string]bool{
		"127 ± 15": true, "0,0127 ± 0,0015": true, "1 250": true, "57": true,
		"Менее 10": false, "Менее 0,001": false, "не менее 10": false, "< 10": false, "": false, "BL": false,
	} {
		if got := IsNumeric(s); got != want {
			t.Errorf("IsNumeric(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestSampleFile разбирает реальный протокол: TABLE4_SAMPLE=/путь/к/файлу.docx go test -run Sample -v
func TestSampleFile(t *testing.T) {
	path := os.Getenv("TABLE4_SAMPLE")
	if path == "" {
		t.Skip("TABLE4_SAMPLE не задан")
	}
	res, err := ParseDocx(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Lead {
		t.Logf("СВИНЕЦ  №%-4s %-12s %s", e.Component, e.MgKg, e.Percent)
	}
	for _, e := range res.Bromine {
		t.Logf("БРОМ    №%-4s %-12s %s", e.Component, e.MgKg, e.Percent)
	}
	t.Logf("строк: %d, BL: %d, числовых строк: %d, числовых ячеек: %d, по элементам: %v",
		res.TotalRows, res.BLRows, res.NumericRows, res.NumericCells, res.NumericByElement)
}
