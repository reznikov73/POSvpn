//go:build !windows

package main

import "errors"

// runServerGUI на не-Windows недоступен: сервер там запускается как демон.
func runServerGUI(app *serverApp) error {
	return errors.New("окно сервера доступно только в Windows")
}
