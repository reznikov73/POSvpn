//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"posvpn-engine/console"
	"posvpn-engine/tray"
)

//go:embed ui/index.html
var serverHTML string

type serverGUI struct {
	app  *serverApp
	w    webview2.WebView
	tray *tray.Tray
}

type settingsRequest struct {
	Listen string `json:"listen"`
	Hub    string `json:"hub"`
	TLS    bool   `json:"tls"`
}

type srvState struct {
	Running     bool          `json:"running"`
	Listen      string        `json:"listen"`
	Scheme      string        `json:"scheme"`
	Hub         string        `json:"hub"`
	Fingerprint string        `json:"fingerprint"`
	Uptime      string        `json:"uptime"`
	Store       string        `json:"store"`
	Log         string        `json:"log"`
	Error       string        `json:"error"`
	TLS         bool          `json:"tls"`
	CertPath    string        `json:"cert_path"`
	LocalOnly   bool          `json:"local_only"`
	Networks    []NetworkView `json:"networks"`
	Nodes       int           `json:"nodes"`
	Received    uint64        `json:"received"`
	Forwarded   uint64        `json:"forwarded"`
	Broadcast   uint64        `json:"broadcast"`
	Dropped     uint64        `json:"dropped"`
}

// runServerGUI открывает окно сервера; при закрытии окна сервер останавливается.
func runServerGUI(app *serverApp, minimized bool) error {
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
			IconId: 1,
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

	// Отладочный запуск диалога настроек: POSVPN_SERVER_DIALOG=settings.
	if os.Getenv("POSVPN_SERVER_DIALOG") != "" {
		w.Init("window.addEventListener('load', function(){ setTimeout(function(){ try { document.getElementById('settingsBtn').click(); } catch (e) {} }, 350); });")
		w.SetHtml(serverHTML)
	}

	serverDarkTitleBar(w)

	trayIcon, trayErr := tray.Start(tray.Options{
		Title:  "POSvpn Server",
		IconID: 1,
		Items: []tray.Item{
			{ID: 1, Title: "Открыть"},
			{ID: 2, Title: "Остановить сервер"},
			{ID: 0, Separator: true},
			{ID: 3, Title: "Выход"},
		},
		OnShow: func() {
			if gui.tray != nil {
				gui.tray.ShowWindow()
			}
		},
		OnSelect: gui.trayAction,
	})
	if trayErr != nil {
		log.Printf("трей: %v", trayErr)
	} else {
		gui.tray = trayIcon
		if err := trayIcon.HookClose(windows.HWND(uintptr(w.Window())), func() { trayIcon.HideWindow() }); err != nil {
			log.Printf("трей: перехват закрытия окна: %v", err)
		}
		if minimized {
			trayIcon.HideWindow()
		}
	}

	w.Run()

	app.Stop()
	if gui.tray != nil {
		gui.tray.Remove()
	}
	return nil
}

// trayAction — команды меню иконки.
func (g *serverGUI) trayAction(id uint32) {
	switch id {
	case 1:
		if g.tray != nil {
			g.tray.ShowWindow()
		}
	case 2:
		if g.app.Running() {
			g.app.Stop()
		} else {
			_ = g.app.Start()
		}
	case 3:
		g.app.Stop()
		if g.tray != nil {
			g.tray.AllowClose()
			g.tray.Remove()
		}
		if g.w != nil {
			g.w.Dispatch(func() { g.w.Terminate() })
		}
	}
}

func (g *serverGUI) updateTray(running bool) {
	if g.tray == nil {
		return
	}
	tip := "POSvpn Server · остановлен"
	toggle := "Запустить сервер"
	if running {
		tip = "POSvpn Server · работает"
		toggle = "Остановить сервер"
	}
	g.tray.SetTooltip(tip)
	g.tray.SetItems([]tray.Item{
		{ID: 1, Title: "Открыть"},
		{ID: 2, Title: toggle},
		{ID: 0, Separator: true},
		{ID: 3, Title: "Выход"},
	})
}

// State отдаёт окну состояние сервера.
func (g *serverGUI) State() (string, error) {
	state := srvState{
		Running:     g.app.Running(),
		Listen:      g.app.ListenAddr(),
		Scheme:      "http",
		Hub:         g.app.HubAddr(),
		Fingerprint: g.app.fingerprint,
		Store:       g.app.storePath,
		Log:         g.app.logPath,
		Error:       g.app.StartupError(),
		TLS:         g.app.TLS(),
		CertPath:    g.app.CertificatePath(),
		LocalOnly:   localOnlyListen(g.app.ListenAddr()),
		Networks:    g.app.store.Snapshot(time.Now()),
	}
	if g.app.TLS() {
		state.Scheme = "https"
	}
	if state.Running {
		state.Uptime = time.Since(g.app.StartedAt()).Round(time.Second).String()
	}
	for _, network := range state.Networks {
		state.Nodes += len(network.Nodes)
	}
	if stats, ok := g.app.Stats(); ok {
		state.Received = stats.Received
		state.Forwarded = stats.Forwarded
		state.Broadcast = stats.Broadcast
		state.Dropped = stats.Dropped
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	g.updateTray(state.Running)
	return string(raw), nil
}

// Action выполняет команды окна. Полезная нагрузка — JSON-строка.
func (g *serverGUI) Action(name, payload string) (string, error) {
	var err error
	switch name {
	case "start":
		err = g.app.Start()
	case "stop":
		g.app.Stop()
	case "settings":
		var req settingsRequest
		if parseErr := json.Unmarshal([]byte(payload), &req); parseErr != nil {
			err = fmt.Errorf("настройки: %w", parseErr)
			break
		}
		if applyErr := g.app.UpdateSettings(req.Listen, req.Hub, req.TLS); applyErr != nil {
			err = applyErr
			break
		}
		err = saveUISettings(g.app.settingsFile, uiSettings{
			Listen: g.app.ListenAddr(),
			Hub:    g.app.HubAddr(),
			TLS:    g.app.TLS(),
		})
	case "quit":
		g.app.Stop()
		if g.tray != nil {
			g.tray.AllowClose()
			g.tray.Remove()
		}
		if g.w != nil {
			g.w.Dispatch(func() { g.w.Terminate() })
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
