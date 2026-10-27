//go:build !windows

package main

// defaultGUIFlag: на Linux и других системах окна нет, сервер работает демоном.
func defaultGUIFlag() bool { return false }
