//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Окно есть только в Windows-сборке; здесь — вывод в консоль для проверки разбора.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "использование: table4 протокол.docx")
		os.Exit(2)
	}
	res, err := ParseDocx(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	print := func(title string, entries []Entry) {
		fmt.Printf("%s (%d)\n", title, len(entries))
		for _, e := range entries {
			fmt.Printf("  №%-5s %-14s %s\n", e.Component, e.MgKg, e.Percent)
		}
	}
	print("СВИНЕЦ", res.Lead)
	print("ОБЩИЙ БРОМ", res.Bromine)
	fmt.Printf("Числовых значений в таблице: %d (ячеек мг/кг и %%: %d)\n", res.NumericRows, res.NumericCells)
	fmt.Printf("Строк со значением BL: %d из %d\n", res.BLRows, res.TotalRows)
}
