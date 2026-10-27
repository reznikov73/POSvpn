//go:build !windows

// Package console настраивает кодовую страницу консоли под UTF-8.
package console

// UTF8 на не-Windows ничего не делает: там UTF-8 и так по умолчанию.
func UTF8() {}

// HideWindow на не-Windows ничего не делает.
func HideWindow() {}
