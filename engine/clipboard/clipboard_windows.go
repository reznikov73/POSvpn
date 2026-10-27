//go:build windows

// Package clipboard читает и пишет текстовый буфер обмена Windows.
package clipboard

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	maxChars      = 1 << 16
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGetClipboardData = user32.NewProc("GetClipboardData")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
)

// Write кладёт строку в буфер обмена.
func Write(text string) error {
	if ok, _, err := procOpenClipboard.Call(0); ok == 0 {
		return err
	}
	defer procCloseClipboard.Call()

	if ok, _, err := procEmptyClipboard.Call(); ok == 0 {
		return err
	}

	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	handle, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(utf16)*2))
	if handle == 0 {
		return err
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		_, _, _ = procGlobalFree.Call(handle)
		return err
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16)), utf16)
	_, _, _ = procGlobalUnlock.Call(handle)

	if result, _, err := procSetClipboardData.Call(cfUnicodeText, handle); result == 0 {
		_, _, _ = procGlobalFree.Call(handle)
		return err
	}
	return nil
}

// Read возвращает текст из буфера обмена.
func Read() (string, error) {
	if ok, _, err := procOpenClipboard.Call(0); ok == 0 {
		return "", err
	}
	defer procCloseClipboard.Call()

	handle, _, err := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return "", errors.New("в буфере обмена нет текста")
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", err
	}
	defer procGlobalUnlock.Call(handle)

	var chars []uint16
	for i := 0; i < maxChars; i++ {
		value := *(*uint16)(unsafe.Pointer(ptr + uintptr(i*2)))
		if value == 0 {
			break
		}
		chars = append(chars, value)
	}
	if len(chars) == 0 {
		return "", errors.New("буфер обмена пуст")
	}
	return windows.UTF16ToString(chars), nil
}
