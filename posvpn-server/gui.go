//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"posvpn-engine/console"
)

//go:embed ui/index.html
var serverHTML string

type serverGUI struct {
	app *serverApp
	w   webview2.WebView
}

type srvState struct {
	Listen      string        `json:"listen"`
	Hub         string        `json:"hub"`
	Fingerprint string        `json:"fingerprint"`
	Uptime      string        `json:"uptime"`
	Store       string        `json:"store"`
	Log         string        `json:"log"`
	Networks    []NetworkView `json:"networks"`
	Nodes       int           `json:"nodes"`
	Received    uint64        `json:"received"`
	Forwarded   uint64        `json:"forwarded"`
	Broadcast   uint64        `json:"broadcast"`
	Dropped     uint64        `json:"dropped"`
}

// runServerGUI открывает окно сервера; при закрытии окна сервер останавливается.
func runServerGUI(app *serverApp) error {
	console.HideWindow()

	dataPath := filepath.Join(os.Getenv("LOCALAPPDATA"), "POSvpn", "webview2-server")
	_ = os.MkdirAll(dataPath, 0o700)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  "POSvpn Server",
			Width:  520,
			Height: 680,
			Center: true,
		},
	})
	if w == nil {
		return errors.New("не удалось открыть окно: не установлен WebView2 Runtime")
	}
	defer w.Destroy()

	gui := &serverGUI{app: app, w: w}
	if err := w.Bind("serverState", gui.State); err != nil {
		return fmt.Errorf("привязка состояния: %w", err)
	}
	if err := w.Bind("serverAction", gui.Action); err != nil {
		return fmt.Errorf("привязка действий: %w", err)
	}

	w.SetHtml(serverHTML)
	serverDarkTitleBar(w)
	w.Run()

	app.shutdown()
	return nil
}

// State отдаёт окну состояние сервера.
func (g *serverGUI) State() (string, error) {
	state := srvState{
		Listen:      g.app.listen,
		Hub:         g.app.hubAddr,
		Fingerprint: g.app.fingerprint,
		Uptime:      time.Since(g.app.started).Round(time.Second).String(),
		Store:       g.app.storePath,
		Log:         g.app.logPath,
		Networks:    g.app.store.Snapshot(time.Now()),
	}
	for _, network := range state.Networks {
		state.Nodes += len(network.Nodes)
	}
	if g.app.hub != nil {
		stats := g.app.hub.Stats()
		state.Received = stats.Received
		state.Forwarded = stats.Forwarded
		state.Broadcast = stats.Broadcast
		state.Dropped = stats.Dropped
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Action выполняет команды окна.
func (g *serverGUI) Action(name string) (string, error) {
	var err error
	switch name {
	case "quit":
		g.app.shutdown()
		if g.w != nil {
			g.w.Terminate()
		}
	case "open_logs":
		err = exec.Command("explorer.exe", "/select,", g.app.logPath).Start()
	default:
		err = fmt.Errorf("неизвестное действие %q", name)
	}

	resp := map[string]any{"ok": err == nil}
	if err != nil {
		resp["error"] = err.Error()
	}
	raw, _ := json.Marshal(resp)
	return string(raw), nil
}

// serverDarkTitleBar красит заголовок окна в тёмный цвет.
func serverDarkTitleBar(w webview2.WebView) {
	if w == nil {
		return
	}
	hwnd := windows.HWND(uintptr(w.Window()))
	if hwnd == 0 {
		return
	}
	dwmapi := windows.NewLazySystemDLL("dwmapi.dll")
	setAttr := dwmapi.NewProc("DwmSetWindowAttribute")
	var enabled int32 = 1
	for _, attr := range []uintptr{20, 19} {
		_, _, _ = setAttr.Call(uintptr(hwnd), attr, uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
	}
}
