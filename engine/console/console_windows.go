//go:build windows

// Package console настраивает кодовую страницу консоли под UTF-8.
package console

import "golang.org/x/sys/windows"

// UTF8 переключает ввод и вывод консоли на UTF-8. Без этого русский текст,
// который программа пишет в stdout, в Windows PowerShell 5.1 и cmd выглядит
// мусором: консоль по умолчанию использует CP866/CP1251.
func UTF8() {
	_ = windows.SetConsoleOutputCP(65001)
	_ = windows.SetConsoleCP(65001)
}
