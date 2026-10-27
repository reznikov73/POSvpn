//go:build windows

// Package tray — иконка в области уведомлений Windows с меню.
//
// Пакет держит собственное скрытое окно и цикл сообщений, поэтому его можно
// использовать из GUI, не трогая оконную процедуру WebView2. Для «свернуть
// по крестику» есть HookClose: он подменяет оконную процедуру и превращает
// WM_CLOSE в вызов callback'а.
package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmApp            = 0x8000
	wmTrayCallback   = wmApp + 1
	wmCommand        = 0x111
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmLButtonUp      = 0x0202
	wmLButtonDblClk  = 0x0203
	wmRButtonUp      = 0x0205
	hwndMessage      = ^uintptr(2) // (HWND)-3, окно только для сообщений
	nimAdd           = 0x00000000
	nimModify        = 0x00000001
	nimDelete        = 0x00000002
	nifMessage       = 0x00000001
	nifIcon          = 0x00000002
	nifTip           = 0x00000004
	imageIcon        = 1
	lrDefaultSize    = 0x00000040
	lrShared         = 0x00008000
	mfString         = 0x00000000
	mfGrayed         = 0x00000001
	mfSeparator      = 0x00000800
	tpmRightButton   = 0x0002
	tpmReturnCmd     = 0x0100
	swHide           = 0
	swShow           = 5
	gwlpWndProc      = ^uintptr(3) // (DWORD)-4
)

// Item — пункт меню. Separator рисует разделитель, ID тогда не нужен.
type Item struct {
	ID        uint32
	Title     string
	Separator bool
	Disabled  bool
}

// Options — что показать и куда сообщать о действиях.
type Options struct {
	Title    string
	IconID   uint32
	Items    []Item
	OnShow   func()
	OnSelect func(id uint32)
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	SzState          [256]uint16
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd     windows.HWND
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procSetForeground     = user32.NewProc("SetForegroundWindow")
	procShowWindow        = user32.NewProc("ShowWindow")
	procSetWindowLongPtr  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc    = user32.NewProc("CallWindowProcW")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")

	registerOnce sync.Once
	registerErr  error

	traysMu sync.Mutex
	trays   = map[uintptr]*Tray{}

	wndProc = syscall.NewCallback(trayWndProc)
)

// Tray — иконка и её меню. Все методы безопасны для вызова из других горутин.
type Tray struct {
	hwnd    uintptr
	icon    windows.Handle
	opts    Options
	itemsMu sync.Mutex
	items   []Item
	tip     string
	tipLock sync.Mutex

	window    windows.HWND
	closeFunc uintptr
	origProc  uintptr
	exiting   atomic.Bool
}

// Start создаёт иконку и запускает цикл сообщений.
func Start(opts Options) (*Tray, error) {
	ready := make(chan error, 1)
	t := &Tray{opts: opts, items: opts.Items, tip: opts.Title}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := t.init(); err != nil {
			ready <- err
			return
		}
		ready <- nil
		t.loop()
	}()

	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Tray) init() error {
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	registerOnce.Do(func() {
		class := windows.StringToUTF16Ptr("PosvpnTrayWindow")
		wc := wndClassEx{
			LpfnWndProc:   wndProc,
			HInstance:     windows.Handle(hInstance),
			LpszClassName: class,
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 {
			registerErr = fmt.Errorf("RegisterClassEx: %w", err)
		}
	})
	if registerErr != nil {
		return registerErr
	}

	className := windows.StringToUTF16Ptr("PosvpnTrayWindow")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0,
		0, 0, 0, 0,
		0, // обычное скрытое окно: Shell_NotifyIcon не любит message-only
		0, hInstance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	t.hwnd = hwnd

	iconID := optsIconID(t.opts.IconID)
	icon, _, _ := procLoadImageW.Call(hInstance, iconID, imageIcon, 0, 0, lrDefaultSize|lrShared)
	if icon == 0 {
		// Запасной вариант — стандартная иконка приложения.
		icon, _, _ = procLoadImageW.Call(hInstance, 32512, imageIcon, 0, 0, lrDefaultSize|lrShared)
	}
	t.icon = windows.Handle(icon)

	traysMu.Lock()
	trays[hwnd] = t
	traysMu.Unlock()

	return t.addIcon()
}

func optsIconID(id uint32) uintptr {
	if id == 0 {
		return 32512
	}
	return uintptr(id)
}

func (t *Tray) addIcon() error {
	data := t.iconData()
	ok, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
	if ok == 0 {
		return fmt.Errorf("Shell_NotifyIcon: %w", err)
	}
	return nil
}

func (t *Tray) iconData() notifyIconData {
	data := notifyIconData{
		HWnd:             windows.HWND(t.hwnd),
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayCallback,
		HIcon:            t.icon,
	}
	data.CbSize = uint32(unsafe.Sizeof(data))

	t.tipLock.Lock()
	tip := t.tip
	t.tipLock.Unlock()
	copy(data.SzTip[:], windows.StringToUTF16(tip))
	return data
}

func (t *Tray) loop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// SetTooltip обновляет подсказку.
func (t *Tray) SetTooltip(text string) {
	t.tipLock.Lock()
	if t.tip == text {
		t.tipLock.Unlock()
		return
	}
	t.tip = text
	t.tipLock.Unlock()

	data := t.iconData()
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

// SetItems заменяет меню. Если пункты не изменились, ничего не делает.
func (t *Tray) SetItems(items []Item) {
	t.itemsMu.Lock()
	defer t.itemsMu.Unlock()
	if sameItems(t.items, items) {
		return
	}
	t.items = items
}

// HookClose перехватывает закрытие окна: вместо выхода вызывается onClose.
func (t *Tray) HookClose(window windows.HWND, onClose func()) error {
	t.window = window

	// Callback должен жить столько же, сколько окно.
	t.closeFunc = syscall.NewCallback(func(hwnd, message, wparam, lparam uintptr) uintptr {
		if message == wmClose {
			if t.exiting.Load() {
				r, _, _ := procCallWindowProc.Call(t.origProc, hwnd, message, wparam, lparam)
				return r
			}
			onClose()
			return 0
		}
		r, _, _ := procCallWindowProc.Call(t.origProc, hwnd, message, wparam, lparam)
		return r
	})

	orig, _, err := procSetWindowLongPtr.Call(uintptr(window), gwlpWndProc, t.closeFunc)
	if orig == 0 {
		return fmt.Errorf("SetWindowLongPtr: %w", err)
	}
	t.origProc = orig
	return nil
}

// AllowClose разрешает настоящее закрытие окна — для команды «Выход».
func (t *Tray) AllowClose() {
	t.exiting.Store(true)
}

// HideWindow убирает окно приложения из панели задач.
func (t *Tray) HideWindow() {
	if t.window != 0 {
		procShowWindow.Call(uintptr(t.window), swHide)
	}
}

// ShowWindow показывает окно и выводит его на передний план.
func (t *Tray) ShowWindow() {
	if t.window != 0 {
		procShowWindow.Call(uintptr(t.window), swShow)
		procSetForeground.Call(uintptr(t.window))
	}
}

// Remove убирает иконку и останавливает цикл сообщений.
func (t *Tray) Remove() {
	data := t.iconData()
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))

	traysMu.Lock()
	delete(trays, t.hwnd)
	traysMu.Unlock()

	procDestroyWindow.Call(t.hwnd)
}

func (t *Tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	t.itemsMu.Lock()
	items := append([]Item(nil), t.items...)
	t.itemsMu.Unlock()

	for _, item := range items {
		if item.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if item.Disabled {
			flags |= mfGrayed
		}
		title := windows.StringToUTF16Ptr(item.Title)
		procAppendMenuW.Call(menu, flags, uintptr(item.ID), uintptr(unsafe.Pointer(title)))
	}

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(t.hwnd)

	selected, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y),
		0, t.hwnd, 0,
	)
	if selected != 0 {
		procPostMessageW.Call(t.hwnd, wmCommand, selected, 0)
	}
}

func trayWndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	traysMu.Lock()
	t := trays[hwnd]
	traysMu.Unlock()

	if t != nil {
		switch message {
		case wmTrayCallback:
			switch lparam & 0xFFFF {
			case wmLButtonUp, wmLButtonDblClk:
				if t.opts.OnShow != nil {
					go t.opts.OnShow()
				}
			case wmRButtonUp:
				t.showMenu()
			}
			return 0
		case wmCommand:
			id := uint32(wparam & 0xFFFF)
			if t.opts.OnSelect != nil {
				go t.opts.OnSelect(id)
			}
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return r
}

func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ErrNotSupported — заглушка для сборочных тегов.
var ErrNotSupported = errors.New("трей доступен только в Windows")
