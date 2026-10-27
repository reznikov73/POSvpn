//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"posvpn-engine/datadir"
	"posvpn-engine/logx"
	"posvpn-engine/tunnel"
)

const (
	serviceName = "POSvpn"
	pipePath    = `\\.\pipe\posvpn`
)

// serviceApp — состояние службы: настройки, туннель и ответы окну.
type serviceApp struct {
	mu       sync.Mutex
	session  *tunnelSession
	starting bool
	lastErr  string
	// want — туннель должен быть поднят. true после подключения (в том числе
	// автоподключения), false после явного отключения пользователем.
	want bool
}

type pipeRequest struct {
	Cmd      string          `json:"cmd"`
	Server   string          `json:"server,omitempty"`
	Hub      string          `json:"hub,omitempty"`
	Pin      string          `json:"pin,omitempty"`
	Name     string          `json:"name,omitempty"`
	Password string          `json:"password,omitempty"`
	Node     string          `json:"node,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

type pipeResponse struct {
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	State   *guiState `json:"state,omitempty"`
	Service bool      `json:"service"`
}

func programDataDir() string {
	dir := os.Getenv("ProgramData")
	if dir == "" {
		dir = `C:\ProgramData`
	}
	return filepath.Join(dir, "POSVpn")
}

// setupServicePaths переключает настройки и журнал службы на ProgramData.
func setupServicePaths() {
	if dir, ok := datadir.PortableDataDir(); ok {
		configOverride = filepath.Join(dir, "client.json")
		if path, err := logx.SetupDir(filepath.Join(dir, "logs"), "service"); err == nil {
			logPath = path
		}
		// Из прежнего места — только если рядом с программой настроек ещё нет.
		_ = datadir.CopyIfMissing(filepath.Join(programDataDir(), "client.json"), configOverride)
		return
	}

	dir := programDataDir()
	_ = os.MkdirAll(dir, 0o755)
	configOverride = filepath.Join(dir, "client.json")
	if path, err := logx.SetupDir(filepath.Join(dir, "logs"), "service"); err == nil {
		logPath = path
	}
}

func (a *serviceApp) snapshot() guiState {
	cfg, _ := loadConfig()
	state := guiState{
		HasWintun: hasWintun(),
		Elevated:  true,
		Service:   true,
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
	state.ServiceInstalled = true
	state.ServiceRunning = true
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

func (a *serviceApp) connect() error {
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
	a.want = true
	a.mu.Unlock()

	go func() {
		session, err := connectFromConfig()
		a.mu.Lock()
		defer a.mu.Unlock()
		a.starting = false
		if err != nil {
			a.lastErr = err.Error()
			log.Printf("служба: туннель не поднят: %v", err)
			return
		}
		a.session = session
		a.lastErr = ""
		log.Printf("служба: туннель поднят")
	}()
	return nil
}

func (a *serviceApp) disconnect() {
	a.mu.Lock()
	session := a.session
	a.session = nil
	a.want = false
	a.lastErr = ""
	a.mu.Unlock()
	if session != nil {
		session.Stop()
	}
}

// applySettings сохраняет адреса и отпечаток, затем перезапускает туннель,
// если он был поднят.
func (a *serviceApp) applySettings(req pipeRequest) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Server = strings.TrimRight(strings.TrimSpace(req.Server), "/")
	if cfg.Server == "" {
		cfg.Server = defaultServer
	}
	cfg.Hub = strings.TrimSpace(req.Hub)
	cfg.HubPin = strings.TrimSpace(req.Pin)
	if err := saveConfig(cfg); err != nil {
		return err
	}

	a.mu.Lock()
	running := a.session != nil
	a.mu.Unlock()
	if running {
		a.disconnect()
		return a.connect()
	}
	return nil
}

// importConfig перенимает настройки локального клиента при первом запуске
// службы: тот же узел, тот же токен, без повторного входа в сеть.
func (a *serviceApp) importConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("пустой конфиг")
	}
	if existing, err := loadConfig(); err == nil && existing.Token != "" {
		return errors.New("у службы уже есть настройки")
	}

	var parsed config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("конфиг не разобран: %w", err)
	}
	if parsed.Token == "" || parsed.PrivateKey == "" {
		return errors.New("в конфиге нет токена или ключа")
	}

	dir := filepath.Dir(configOverride)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(configOverride, raw, 0o600); err != nil {
		return fmt.Errorf("запись конфига службы: %w", err)
	}
	log.Printf("служба: настройки перенесены из клиента (%s)", parsed.Network)

	// Даём локальному туннелю отпустить адаптер, затем подключаемся сами.
	go func() {
		time.Sleep(800 * time.Millisecond)
		if err := a.connect(); err != nil {
			log.Printf("служба: после переноса настроек: %v", err)
		}
	}()
	return nil
}

// startWatchdog держит туннель поднятым: если координатор был недоступен
// при загрузке или сеанс оборвался, служба повторяет попытки с нарастающей
// паузой. Без этого после старта раньше сервера туннель не поднимался вовсе.
func (a *serviceApp) startWatchdog(ctx context.Context) {
	go func() {
		delay := 5 * time.Second
		const maxDelay = time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			a.mu.Lock()
			want := a.want
			session := a.session
			starting := a.starting
			a.mu.Unlock()
			if !want || starting {
				delay = 5 * time.Second
				continue
			}

			if session != nil {
				running, _, lastErr := session.status()
				if running {
					delay = 5 * time.Second
					continue
				}
				log.Printf("служба: сеанс завершён (%s), переподключаемся", lastErr)
				session.Stop()
				a.mu.Lock()
				if a.session == session {
					a.session = nil
				}
				a.mu.Unlock()
			}

			before := a.snapshot().Error
			if err := a.connect(); err != nil {
				log.Printf("служба: повтор подключения: %v", err)
				delay = maxDelay
				continue
			}
			// Подключение идёт в фоне: даём ему завершиться до следующей проверки.
			select {
			case <-ctx.Done():
				return
			case <-time.After(4 * time.Second):
			}
			if a.snapshot().Error != "" && a.snapshot().Error != before {
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			} else {
				delay = 5 * time.Second
			}
		}
	}()
}

// serviceStatusInfo сообщает, установлена ли служба и работает ли она.
// Обычный пользователь имеет право только на запрос состояния.
func serviceStatusInfo() (installed, running bool) {
	manager, err := mgr.Connect()
	if err != nil {
		return false, false
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		return false, false
	}
	defer service.Close()

	status, err := service.Query()
	if err != nil {
		return true, false
	}
	return true, status.State == svc.Running
}

func (a *serviceApp) action(req pipeRequest) error {
	switch req.Cmd {
	case "connect":
		return a.connect()
	case "disconnect":
		a.disconnect()
		return nil
	case "create":
		return runAuth([]string{
			"-server", req.Server, "-network", req.Name,
			"-password", req.Password, "-node", req.Node,
		}, true)
	case "join":
		return runAuth([]string{
			"-server", req.Server, "-network", req.Name,
			"-password", req.Password, "-node", req.Node,
		}, false)
	case "settings":
		return a.applySettings(req)
	case "import":
		return a.importConfig(req.Config)
	default:
		return fmt.Errorf("неизвестная команда %q", req.Cmd)
	}
}

// servePipe принимает подключения окна и отвечает JSON-строками.
func servePipe(ctx context.Context, app *serviceApp) error {
	sd, err := windows.SecurityDescriptorFromString("D:(A;;GA;;;AU)")
	if err != nil {
		return fmt.Errorf("права на pipe: %w", err)
	}
	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}

	for ctx.Err() == nil {
		handle, err := windows.CreateNamedPipe(
			windows.StringToUTF16Ptr(pipePath),
			windows.PIPE_ACCESS_DUPLEX,
			windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
			windows.PIPE_UNLIMITED_INSTANCES,
			64*1024, 64*1024, 0, sa,
		)
		if err != nil {
			return fmt.Errorf("создание pipe: %w", err)
		}

		err = windows.ConnectNamedPipe(handle, nil)
		if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			_ = windows.CloseHandle(handle)
			continue
		}

		file := os.NewFile(uintptr(handle), "posvpn-pipe")
		go handlePipeConn(file, app)
	}
	return nil
}

func handlePipeConn(file *os.File, app *serviceApp) {
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(file))
	encoder := json.NewEncoder(file)

	for {
		var req pipeRequest
		if err := decoder.Decode(&req); err != nil {
			return
		}

		var actionErr error
		switch req.Cmd {
		case "status":
			// только отдать состояние
		default:
			actionErr = app.action(req)
		}

		state := app.snapshot()
		resp := pipeResponse{OK: actionErr == nil, Service: true, State: &state}
		if actionErr != nil {
			resp.Error = actionErr.Error()
		}
		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

// posvpnService — точка входа диспетчера служб Windows.
type posvpnService struct{}

func (posvpnService) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &serviceApp{}

	go func() {
		if cfg, err := loadConfig(); err == nil && cfg.Token != "" {
			if err := app.connect(); err != nil {
				log.Printf("служба: автоподключение не удалось: %v", err)
			}
		}
	}()
	// Служба стартует вместе с Windows, раньше координатора: без сторожа
	// одна неудачная попытка оставляла туннель опущенным до перезапуска.
	app.startWatchdog(ctx)
	go func() {
		if err := servePipe(ctx, app); err != nil {
			log.Printf("служба: pipe остановлен: %v", err)
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for request := range requests {
		if request.Cmd == svc.Stop || request.Cmd == svc.Shutdown {
			changes <- svc.Status{State: svc.StopPending}
			cancel()
			app.disconnect()
			return false, 0
		}
	}
	return false, 0
}

func serviceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("укажите install, uninstall, start, stop или run")
	}
	setupServicePaths()

	switch args[0] {
	case "install":
		return serviceInstall()
	case "uninstall":
		return serviceUninstall()
	case "start":
		return serviceControl("start")
	case "stop":
		return serviceControl("stop")
	case "run":
		log.Printf("служба POSvpn запущена вручную")
		return svc.Run(serviceName, &posvpnService{})
	case "console":
		return runServiceConsole()
	case "status":
		return serviceStatus()
	default:
		return fmt.Errorf("неизвестная команда службы %q", args[0])
	}
}

func serviceInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("подключение к диспетчеру служб (нужны права администратора): %w", err)
	}
	defer manager.Disconnect()

	if existing, err := manager.OpenService(serviceName); err == nil {
		defer existing.Close()
		_ = existing.Start()
		fmt.Println("Служба POSvpn уже установлена.")
		return nil
	}

	service, err := manager.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "POSvpn",
		Description: "Виртуальная локальная сеть POSvpn: адаптер, туннель и маршруты",
		StartType:   mgr.StartAutomatic,
	}, "service", "run")
	if err != nil {
		return fmt.Errorf("создание службы: %w", err)
	}
	defer service.Close()

	if err := service.Start(); err != nil {
		return fmt.Errorf("служба создана, но не запустилась: %w", err)
	}
	fmt.Println("Служба POSvpn установлена и запущена.")
	return nil
}

func serviceUninstall() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("подключение к диспетчеру служб (нужны права администратора): %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		return errors.New("служба POSvpn не установлена")
	}
	defer service.Close()

	_, _ = service.Control(svc.Stop)
	for i := 0; i < 10; i++ {
		status, err := service.Query()
		if err != nil || status.State == svc.Stopped {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("удаление службы: %w", err)
	}
	fmt.Println("Служба POSvpn удалена.")
	return nil
}

func runServiceConsole() error {
	log.Printf("служба POSvpn: консольный режим, pipe %s", pipePath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := &serviceApp{}
	if cfg, err := loadConfig(); err == nil && cfg.Token != "" {
		if err := app.connect(); err != nil {
			log.Printf("автоподключение: %v", err)
		}
	}
	// Повторяем подключение, пока координатор не ответит.
	app.startWatchdog(ctx)
	go func() {
		if err := servePipe(ctx, app); err != nil {
			log.Printf("pipe: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	app.disconnect()
	return nil
}

// serviceStatus печатает состояние службы — удобно проверять без окна.
func serviceStatus() error {
	resp, err := serviceCall(pipeRequest{Cmd: "status"})
	if err != nil {
		return fmt.Errorf("служба не отвечает: %w", err)
	}
	raw, err := json.MarshalIndent(resp.State, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func serviceControl(command string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("подключение к диспетчеру служб: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		return errors.New("служба POSvpn не установлена")
	}
	defer service.Close()

	switch command {
	case "start":
		if err := service.Start(); err != nil {
			return fmt.Errorf("запуск службы: %w", err)
		}
		fmt.Println("Служба POSvpn запущена.")
	case "stop":
		if _, err := service.Control(svc.Stop); err != nil {
			return fmt.Errorf("остановка службы: %w", err)
		}
		fmt.Println("Служба POSvpn остановлена.")
	}
	return nil
}

// connectFromConfig собирает туннель по сохранённым настройкам.
func connectFromConfig() (*tunnelSession, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, errors.New("сначала создайте сеть или войдите в неё")
	}

	privateKey, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil || len(privateKey) != 32 {
		return nil, errors.New("в настройках нет корректного приватного ключа: войдите в сеть заново")
	}

	netmap, err := fetchNetMap(cfg)
	if err != nil {
		return nil, err
	}
	subnet, err := netip.ParsePrefix(netmap.Subnet)
	if err != nil {
		return nil, fmt.Errorf("сервер прислал некорректную подсеть %q", netmap.Subnet)
	}

	hub := cfg.Hub
	if hub == "" {
		hub = defaultHubAddr(cfg.Server)
	}

	return startTunnelSession(tunnelOptions{
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
	})
}
