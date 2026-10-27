//go:build windows

package main

// defaultGUIFlag: на Windows сервер по умолчанию открывает окно, как клиент.
// Консольный режим — флаг -console (служба, VPS, отладка).
func defaultGUIFlag() bool { return true }
