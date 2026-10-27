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

// HideWindow прячет консольное окно процесса. Нужно GUI-запуску: окно
// приложения не должно стоять рядом с чёрной консолью.
func HideWindow() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getConsole := kernel32.NewProc("GetConsoleWindow")
	user32 := windows.NewLazySystemDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsole.Call()
	if hwnd != 0 {
		const swHide = 0
		_, _, _ = showWindow.Call(hwnd, swHide)
	}
}
