//go:build windows

package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"posvpn-engine/clipboard"
	"posvpn-engine/console"
	"posvpn-engine/tray"
	"posvpn-engine/tunnel"
)

//go:embed ui/index.html
var guiHTML string

// guiApp — окно и его связь с туннелем.
type guiApp struct {
	w webview2.WebView

	tray *tray.Tray

	mu       sync.Mutex
	session  *tunnelSession
	starting bool
	lastErr  string
	imported bool
}

type guiState struct {
	Configured       bool      `json:"configured"`
	Server           string    `json:"server"`
	Hub              string    `json:"hub"`
	HubPin           string    `json:"hub_pin"`
	Network          string    `json:"network"`
	SelfName         string    `json:"self_name"`
	SelfIP           string    `json:"self_ip"`
	SelfIndex        uint32    `json:"self_index"`
	NodeID           string    `json:"node_id"`
	ServiceInstalled bool      `json:"service_installed"`
	ServiceRunning   bool      `json:"service_running"`
	Running          bool      `json:"running"`
	Connected        bool      `json:"connected"`
	Starting         bool      `json:"starting"`
	Error            string    `json:"error"`
	HasWintun        bool      `json:"has_wintun"`
	Elevated         bool      `json:"elevated"`
	Service          bool      `json:"service"`
	LogPath          string    `json:"log_path"`
	Peers            []guiPeer `json:"peers"`
}

type guiPeer struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Index  uint32 `json:"index"`
	Role   string `json:"role"`
	Online bool   `json:"online"`
	RTT    int64  `json:"rtt"`
}

type guiRequest struct {
	Server   string `json:"server"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Node     string `json:"node"`
	Hub      string `json:"hub"`
	Pin      string `json:"pin"`
	Command  string `json:"command"`
}

// guiLog пишет строку в тот же журнал, что и JS-ошибки.
func guiLog(format string, args ...any) {
	path := filepath.Join(os.TempDir(), "posvpn-gui.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// showWindow возвращает окно из трея.
func (a *guiApp) showWindow() {
	if a.tray != nil {
		a.tray.ShowWindow()
	}
}

func (a *guiApp) isConnected() bool {
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	if session == nil {
		return false
	}
	_, connected, _ := session.status()
	return connected
}

// trayToggle подключает или отключает туннель — и локальный, и через службу.
func (a *guiApp) trayToggle() {
	if resp, err := serviceCall(pipeRequest{Cmd: "status"}); err == nil && resp.State != nil {
		command := "connect"
		if resp.State.Connected || resp.State.Running {
			command = "disconnect"
		}
		_, _ = serviceCall(pipeRequest{Cmd: command})
		return
	}
	if a.isConnected() {
		a.disconnect()
		return
	}
	_ = a.connect()
}

func (a *guiApp) trayAction(id uint32) {
	switch id {
	case 1:
		a.showWindow()
	case 2:
		a.trayToggle()
	case 3:
		a.showWindow()
		if a.w != nil {
			a.w.Eval("openSettings()")
		}
	case 4:
		a.disconnect()
		if a.tray != nil {
			a.tray.AllowClose()
			a.tray.Remove()
		}
		if a.w != nil {
			a.w.Dispatch(func() { a.w.Terminate() })
		}
	}
}

func (a *guiApp) updateTray(s guiState) {
	if a.tray == nil {
		return
	}
	tip := "POSvpn · не подключено"
	if s.Configured && s.Network != "" {
		tip = "POSvpn · " + s.Network
	}
	if s.Connected {
		tip = "POSvpn · " + s.Network + " · в сети"
	}
	a.tray.SetTooltip(tip)

	connectTitle := "Подключиться"
	if s.Connected || s.Running {
		connectTitle = "Отключиться"
	}
	a.tray.SetItems([]tray.Item{
		{ID: 1, Title: "Открыть"},
		{ID: 2, Title: connectTitle, Disabled: !s.Configured},
		{ID: 3, Title: "Настройки"},
		{ID: 0, Separator: true},
		{ID: 4, Title: "Выход"},
	})
}

// runGUI открывает окно. Возвращает ошибку, если WebView2 недоступен.
// autoConnect сразу поднимает туннель — удобно для автозапуска.
func runGUI(autoConnect, minimized bool) error {
	console.HideWindow()

	dataPath := filepath.Join(os.Getenv("LOCALAPPDATA"), "POSvpn", "webview2")
	_ = os.MkdirAll(dataPath, 0o700)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  "POSvpn",
			Width:  440,
			Height: 700,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return errors.New("не удалось открыть окно: не установлен WebView2 Runtime")
	}
	defer w.Destroy()

	app := &guiApp{w: w}
	if err := w.Bind("posvpnState", app.State); err != nil {
		return fmt.Errorf("привязка состояния: %w", err)
	}
	if err := w.Bind("posvpnAction", app.Action); err != nil {
		return fmt.Errorf("привязка действий: %w", err)
	}
	if err := w.Bind("posvpnLog", app.Log); err != nil {
		return fmt.Errorf("привязка журнала: %w", err)
	}

	w.SetHtml(guiHTML)

	// Отладочный запуск диалога для проверки вёрстки: POSVPN_GUI_DIALOG=create.
	if dialog := os.Getenv("POSVPN_GUI_DIALOG"); dialog != "" {
		call := "setAuthMode('" + dialog + "')"
		if dialog == "settings" {
			call = "openSettings()"
		}
		w.Init("window.addEventListener('load', function(){ setTimeout(function(){ try { " + call + "; } catch (e) {} }, 900); });")
		w.SetHtml(guiHTML)
	}

	app.darkTitleBar()

	trayIcon, trayErr := tray.Start(tray.Options{
		Title:  "POSvpn",
		IconID: 1,
		Items: []tray.Item{
			{ID: 1, Title: "Открыть"},
			{ID: 2, Title: "Подключиться"},
			{ID: 3, Title: "Настройки"},
			{ID: 0, Separator: true},
			{ID: 4, Title: "Выход"},
		},
		OnShow:   func() { app.showWindow() },
		OnSelect: func(id uint32) { app.trayAction(id) },
	})
	if trayErr != nil {
		log.Printf("трей: %v", trayErr)
	} else {
		app.tray = trayIcon
		if err := trayIcon.HookClose(windows.HWND(uintptr(w.Window())), func() { trayIcon.HideWindow() }); err != nil {
			log.Printf("трей: перехват закрытия окна: %v", err)
		}
		if minimized {
			trayIcon.HideWindow()
		}
	}

	if autoConnect {
		go func() { _ = app.connect() }()
	}
	w.Run()

	app.disconnect()
	if app.tray != nil {
		app.tray.Remove()
	}
	return nil
}

// State отдаёт в интерфейс всё, что нужно для отрисовки.
func (a *guiApp) State() (string, error) {
	state := a.collectState()
	if a.tray != nil {
		a.updateTray(state)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *guiApp) collectState() guiState {
	// Если служба запущена, она владеет туннелем и настройками — берём её состояние.
	if resp, err := serviceCall(pipeRequest{Cmd: "status"}); err == nil && resp.State != nil {
		// Локальный туннель больше не нужен: адаптер должен принадлежать службе.
		if a.session != nil {
			a.disconnect()
		}

		// Первый запуск службы: переносим настройки пользователя, чтобы не
		// входить в сеть заново и не получать 409 на занятое имя.
		if !resp.State.Configured && !a.imported {
			a.imported = true
			if cfg, cfgErr := loadConfig(); cfgErr == nil && cfg.Token != "" {
				if rawCfg, marshalErr := json.Marshal(cfg); marshalErr == nil {
					if imported, importErr := serviceCall(pipeRequest{Cmd: "import", Config: rawCfg}); importErr == nil && imported.OK {
						if again, againErr := serviceCall(pipeRequest{Cmd: "status"}); againErr == nil && again.State != nil {
							resp = again
						}
					}
				}
			}
		}

		raw, err := json.Marshal(resp.State)
		if err != nil {
			return guiState{}
		}
		_ = raw
		return *resp.State
	}

	cfg, _ := loadConfig()

	// Служба установлена, но не ответила: не подменяем её локальным режимом,
	// иначе окно возьмёт устаревший токен и получит 401.
	if installed, running := serviceStatusInfo(); installed {
		state := guiState{
			HasWintun:        hasWintun(),
			Elevated:         isElevated(),
			ServiceInstalled: true,
			ServiceRunning:   running,
			Peers:            []guiPeer{},
		}
		if !running {
			state.Error = "служба остановлена — запустите её в настройках"
		}
		return state
	}

	state := guiState{
		HasWintun: hasWintun(),
		Elevated:  isElevated(),
		LogPath:   logPath,
		Peers:     []guiPeer{},
	}
	if cfg != nil {
		state.Configured = cfg.Token != ""
		state.Server = cfg.Server
		state.Hub = cfg.Hub
		state.HubPin = cfg.HubPin
		state.Network = cfg.Network
		state.SelfName = cfg.Node
		state.SelfIP = cfg.IP
		state.SelfIndex = cfg.Index
		state.NodeID = cfg.NodeID
	}
	state.ServiceInstalled, state.ServiceRunning = serviceStatusInfo()
	if state.SelfName == "" {
		state.SelfName = hostname()
	}

	a.mu.Lock()
	session := a.session
	state.Starting = a.starting
	if session == nil && a.lastErr != "" {
		state.Error = a.lastErr
	}
	a.mu.Unlock()

	if session != nil {
		running, connected, lastErr := session.status()
		state.Running = running
		state.Connected = connected
		state.Error = lastErr

		for _, peer := range session.peers.all() {
			state.Peers = append(state.Peers, guiPeer{
				Name:   peer.name,
				IP:     peer.ip.String(),
				Index:  peer.index,
				Role:   peer.role,
				Online: peer.online(),
				RTT:    peer.rtt(),
			})
		}
		sort.Slice(state.Peers, func(i, j int) bool { return state.Peers[i].Index < state.Peers[j].Index })
	}

	return state
}

// Action выполняет команду из интерфейса. Ошибки возвращаются в JSON,
// чтобы окно показало их само, а не молчало.
func (a *guiApp) Action(name, payload string) (string, error) {
	var req guiRequest
	if strings.TrimSpace(payload) != "" {
		_ = json.Unmarshal([]byte(payload), &req)
	}

	// Команды, которые может выполнить служба, отправляем ей.
	if handled, svcErr := a.serviceAction(name, req); handled {
		resp := map[string]any{"ok": svcErr == nil}
		if svcErr != nil {
			resp["error"] = svcErr.Error()
		}
		raw, _ := json.Marshal(resp)
		return string(raw), nil
	}

	var err error
	switch name {
	case "create":
		err = guiAuth(req, true)
	case "join":
		err = guiAuth(req, false)
	case "connect":
		err = a.connect()
	case "disconnect":
		a.disconnect()
	case "service":
		err = a.runServiceCommand(req.Command)
	case "settings":
		_, err = a.applySettings(req)
	case "paste_clipboard":
		text, readErr := clipboard.Read()
		resp := map[string]any{"ok": readErr == nil, "text": text}
		if readErr != nil {
			resp["error"] = readErr.Error()
		}
		raw, _ := json.Marshal(resp)
		return string(raw), nil
	case "quit":
		a.disconnect()
		if a.tray != nil {
			a.tray.AllowClose()
			a.tray.Remove()
		}
		if a.w != nil {
			a.w.Dispatch(func() { a.w.Terminate() })
		}
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

// serviceCall отправляет одну команду службе через named pipe.
func serviceCall(req pipeRequest) (pipeResponse, error) {
	file, err := os.OpenFile(pipePath, os.O_RDWR, 0)
	if err != nil {
		return pipeResponse{}, err
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(req); err != nil {
		return pipeResponse{}, err
	}
	var resp pipeResponse
	if err := json.NewDecoder(file).Decode(&resp); err != nil {
		return pipeResponse{}, err
	}
	return resp, nil
}

// serviceRunning проверяет, отвечает ли служба на localhost-pipe.
func serviceRunning() bool {
	resp, err := serviceCall(pipeRequest{Cmd: "status"})
	return err == nil && resp.OK
}

// serviceAction отправляет команду службе. handled=false означает, что
// службы нет и нужно работать локально.
func (a *guiApp) serviceAction(name string, req guiRequest) (bool, error) {
	switch name {
	case "create", "join", "connect", "disconnect":
	default:
		return false, nil
	}

	resp, err := serviceCall(pipeRequest{
		Cmd:      name,
		Server:   req.Server,
		Name:     req.Name,
		Password: req.Password,
		Node:     req.Node,
	})
	if err != nil {
		// Служба установлена, но не отвечает: локальный режим только запутает —
		// у него свой конфиг и свой токен.
		if installed, _ := serviceStatusInfo(); installed {
			return true, errors.New("служба установлена, но не отвечает: запустите её в настройках")
		}
		return false, nil
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "служба вернула ошибку"
		}
		return true, errors.New(resp.Error)
	}
	return true, nil
}

// runServiceCommand выполняет действие со службой. Если прав мало,
// перезапускает этот же exe с теми же аргументами через UAC.
func (a *guiApp) runServiceCommand(command string) error {
	switch command {
	case "install", "uninstall", "start", "stop":
	default:
		return fmt.Errorf("неизвестная команда службы %q", command)
	}
	if isElevated() {
		return serviceCommand([]string{command})
	}
	return relaunchElevated([]string{"service", command})
}

// applySettings сохраняет адреса и отпечаток. Если служба работает,
// настройки уходят ей; иначе пишутся в локальный конфиг.
func (a *guiApp) applySettings(req guiRequest) (bool, error) {
	if resp, err := serviceCall(pipeRequest{
		Cmd:    "settings",
		Server: req.Server,
		Hub:    req.Hub,
		Pin:    req.Pin,
	}); err == nil {
		if !resp.OK {
			if resp.Error == "" {
				resp.Error = "служба вернула ошибку"
			}
			return true, errors.New(resp.Error)
		}
		return true, nil
	} else if installed, _ := serviceStatusInfo(); installed {
		return true, errors.New("служба установлена, но не отвечает: запустите её в настройках")
	}

	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}
	cfg.Server = strings.TrimRight(strings.TrimSpace(req.Server), "/")
	if cfg.Server == "" {
		cfg.Server = defaultServer
	}
	cfg.Hub = strings.TrimSpace(req.Hub)
	cfg.HubPin = strings.TrimSpace(req.Pin)
	if err := saveConfig(cfg); err != nil {
		return false, err
	}
	return false, nil
}

func guiAuth(req guiRequest, create bool) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("не задано имя сети")
	}
	if len(req.Password) < 8 {
		return errors.New("пароль сети короче 8 символов")
	}
	node := strings.TrimSpace(req.Node)
	if node == "" {
		node = hostname()
	}
	server := strings.TrimSpace(req.Server)

	args := []string{"-server", server, "-network", req.Name, "-password", req.Password, "-node", node}
	if err := runAuth(args, create); err != nil {
		return err
	}
	return nil
}

// connect поднимает туннель в фоне, чтобы окно не замирало на установке
// драйвера и подключении к хабу.
func (a *guiApp) connect() error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return errors.New("туннель уже запущен")
	}
	if a.starting {
		a.mu.Unlock()
		return errors.New("туннель уже подключается")
	}
	a.starting = true
	a.lastErr = ""
	a.mu.Unlock()

	cfg, err := loadConfig()
	if err != nil {
		a.finishStart(err)
		return err
	}
	if cfg.Token == "" {
		a.finishStart(errors.New("сначала создайте сеть или войдите в неё"))
		return errors.New("сначала создайте сеть или войдите в неё")
	}

	privateKey, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil || len(privateKey) != 32 {
		msg := "в настройках нет корректного приватного ключа: войдите в сеть заново"
		a.finishStart(errors.New(msg))
		return errors.New(msg)
	}

	netmap, err := fetchNetMap(cfg)
	if err != nil {
		a.finishStart(err)
		return err
	}
	subnet, err := netip.ParsePrefix(netmap.Subnet)
	if err != nil {
		msg := fmt.Sprintf("сервер прислал некорректную подсеть %q", netmap.Subnet)
		a.finishStart(errors.New(msg))
		return errors.New(msg)
	}

	hub := cfg.Hub
	if hub == "" {
		hub = defaultHubAddr(cfg.Server)
	}

	opt := tunnelOptions{
		Server:     cfg.Server,
		HubAddr:    hub,
		Token:      cfg.Token,
		NetworkID:  netmap.NetworkID,
		Subnet:     subnet,
		SelfIndex:  netmap.Self.Index,
		SelfIP:     netmap.Self.IP,
		PrivateKey: privateKey,
		Pin:        cfg.HubPin,
		MTU:        tunnel.DefaultMTU,
		Adapter:    "POSvpn",
		NetMap:     netmap,
	}

	go func() {
		guiLog("connect: старт")
		session, err := startTunnelSession(opt)
		a.mu.Lock()
		a.starting = false
		if err != nil {
			a.lastErr = err.Error()
			guiLog("connect: ошибка: %v", err)
		} else {
			a.session = session
			a.lastErr = ""
			guiLog("connect: сессия создана")
		}
		a.mu.Unlock()
	}()
	return nil
}

func (a *guiApp) finishStart(err error) {
	a.mu.Lock()
	a.starting = false
	if err != nil {
		a.lastErr = err.Error()
	}
	a.mu.Unlock()
}

func (a *guiApp) disconnect() {
	a.mu.Lock()
	session := a.session
	a.session = nil
	a.mu.Unlock()
	if session != nil {
		session.Stop()
	}
}

// Log пишет строку из интерфейса в журнал рядом с временными файлами.
// Нужен, чтобы видеть ошибки JS, которые иначе не видны в окне.
func (a *guiApp) Log(line string) (string, error) {
	path := filepath.Join(os.TempDir(), "posvpn-gui.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), line)
	return "", nil
}

// hasWintun проверяет, есть ли драйвер рядом с exe или в системе.
func hasWintun() bool {
	_, ok := wintunPath()
	return ok
}

// darkTitleBar красит заголовок окна в тёмный цвет, чтобы он не выбивался
// из интерфейса.
func (a *guiApp) darkTitleBar() {
	if a.w == nil {
		return
	}
	hwnd := windows.HWND(uintptr(a.w.Window()))
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

// hideConsole больше не нужен: используется console.HideWindow.
