// posvpn-server — координатор сети POSvpn: реестр сетей и узлов, выдача адресов,
// карта сети для клиентов и QUIC-хаб.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"posvpn-engine/console"
	"posvpn-engine/datadir"
	"posvpn-engine/logx"
	"posvpn-engine/tunnel"
)

// serverApp — процесс сервера. Его можно останавливать и снова запускать
// из окна, не закрывая программу.
type serverApp struct {
	store       *Store
	cert        tls.Certificate
	fingerprint string

	listen       string
	hubAddr      string
	storePath    string
	logPath      string
	certFile     string
	keyFile      string
	settingsFile string
	tlsSelf      bool
	certPath     string

	mu           sync.Mutex
	running      bool
	hub          *tunnel.Hub
	http         *http.Server
	startedAt    time.Time
	startupError string
}

// Start поднимает HTTP-сервер и хаб. Повторный вызов при работающем сервере
// ничего не делает.
func (a *serverApp) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil
	}

	handler := NewServer(a.store)
	srv := &http.Server{
		Addr:              a.listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", a.listen)
	if err != nil {
		a.startupError = fmt.Sprintf("порт %s: %v", a.listen, err)
		return fmt.Errorf("%s", a.startupError)
	}

	var hub *tunnel.Hub
	if a.hubAddr != "" {
		hub, err = StartHub(a.store, a.hubAddr, a.cert)
		if err != nil {
			_ = listener.Close()
			a.startupError = err.Error()
			return err
		}
	}

	a.http = srv
	a.hub = hub
	a.running = true
	a.startedAt = time.Now()
	a.startupError = ""

	go func() {
		var serveErr error
		if a.certFile != "" && a.keyFile != "" {
			log.Printf("POSvpn coordinator слушает https://%s", a.listen)
			serveErr = srv.ServeTLS(listener, a.certFile, a.keyFile)
		} else if a.tlsSelf {
			srv.TLSConfig = &tls.Config{
				Certificates: []tls.Certificate{a.cert},
				MinVersion:   tls.VersionTLS12,
			}
			log.Printf("POSvpn coordinator слушает https://%s (сертификат хаба, клиентам нужен -pin)", a.listen)
			serveErr = srv.ServeTLS(listener, "", "")
		} else {
			log.Printf("POSvpn coordinator слушает http://%s — ВНИМАНИЕ: без TLS, только для отладки", a.listen)
			serveErr = srv.Serve(listener)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("сервер остановлен из-за ошибки: %v", serveErr)
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}
	}()

	log.Printf("сервер запущен (control %s, хаб %s)", a.listen, a.hubAddr)
	if localOnlyListen(a.listen) {
		log.Printf("ВНИМАНИЕ: %s слушает только этот компьютер — удалённые клиенты не подключатся", a.listen)
	}
	return nil
}

// Stop останавливает HTTP-сервер и хаб, освобождая порты.
func (a *serverApp) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	srv, hub := a.http, a.hub
	a.running = false
	a.http = nil
	a.hub = nil
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("остановка HTTP: %v", err)
		}
	}
	if hub != nil {
		_ = hub.Close()
	}
	log.Printf("сервер остановлен")
}

func (a *serverApp) Running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

func (a *serverApp) StartedAt() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startedAt
}

func (a *serverApp) ListenAddr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listen
}

func (a *serverApp) HubAddr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hubAddr
}

// UpdateEndpoints применяет новые адреса: останавливает сервер, меняет
// настройки и поднимает снова. Если новый адрес занят, возвращает прежние.
func (a *serverApp) UpdateSettings(listen, hub string, tlsSelf bool) error {
	listen = strings.TrimSpace(listen)
	hub = strings.TrimSpace(hub)
	if listen == "" {
		return errors.New("адрес контроля не может быть пустым")
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("контроль: %w", err)
	}
	if hub != "" {
		if _, _, err := net.SplitHostPort(hub); err != nil {
			return fmt.Errorf("хаб: %w", err)
		}
	}

	a.mu.Lock()
	oldListen, oldHub, oldTLS, wasRunning := a.listen, a.hubAddr, a.tlsSelf, a.running
	a.mu.Unlock()

	a.Stop()
	a.mu.Lock()
	a.listen, a.hubAddr, a.tlsSelf = listen, hub, tlsSelf
	a.mu.Unlock()

	if !wasRunning {
		return nil
	}
	if err := a.Start(); err != nil {
		a.mu.Lock()
		a.listen, a.hubAddr, a.tlsSelf = oldListen, oldHub, oldTLS
		a.mu.Unlock()
		_ = a.Start()
		return fmt.Errorf("не удалось применить: %v (вернули %s и %s)", err, oldListen, oldHub)
	}
	return nil
}

func (a *serverApp) TLS() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tlsSelf
}

func (a *serverApp) CertificatePath() string { return a.certPath }

func (a *serverApp) StartupError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupError
}

func (a *serverApp) Stats() (tunnel.Stats, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hub == nil {
		return tunnel.Stats{}, false
	}
	return a.hub.Stats(), true
}

func main() {
	console.UTF8()

	portableDir, portable := datadir.PortableDataDir()
	logPath := ""
	if portable {
		logPath, _ = logx.SetupDir(filepath.Join(portableDir, "logs"), "server")
	} else {
		logPath, _ = logx.Setup("server")
	}

	settingsFile := settingsPath()
	saved := loadUISettings(settingsFile)
	listenDefault := "0.0.0.0:8787"
	if saved.Listen != "" {
		listenDefault = saved.Listen
	}
	hubDefault := "0.0.0.0:8788"
	if saved.Hub != "" {
		hubDefault = saved.Hub
	}

	listen := flag.String("listen", listenDefault, "адрес и порт control-сервера")
	hubAddr := flag.String("hub", hubDefault, "адрес QUIC-хаба; пусто — не запускать")
	storeDefault := "server.json"
	if portable {
		storeDefault = filepath.Join(portableDir, "server.json")
	}
	storePath := flag.String("store", storeDefault, "файл состояния координатора")
	certFile := flag.String("tls-cert", "", "сертификат TLS; пусто — работа без TLS (только для локальной отладки)")
	keyFile := flag.String("tls-key", "", "ключ TLS")
	guiFlag := flag.Bool("gui", defaultGUIFlag(), "открыть окно сервера (Windows)")
	consoleFlag := flag.Bool("console", false, "работать без окна: служба, VPS, отладка")
	minimized := flag.Bool("minimized", false, "запустить свёрнутым в трей")
	tlsSelf := flag.Bool("tls", saved.TLS, "HTTPS для control-API на сертификате хаба (клиентам нужен -pin)")
	flag.Parse()

	resolvedStore := resolveStorePath(*storePath)
	if portable && *storePath == storeDefault {
		// Первый запуск рядом с программой: подхватываем состояние из текущего каталога.
		_ = datadir.CopyIfMissing("server.json", resolvedStore)
	}
	store, err := OpenStore(resolvedStore)
	if err != nil {
		log.Fatalf("состояние не загружено: %v", err)
	}

	certDir := certificateDir(resolvedStore)
	if portable {
		certDir = portableDir
	}
	cert, err := tunnel.LoadOrCreateCertificate(certDir)
	if err != nil {
		log.Fatalf("сертификат хаба: %v", err)
	}
	fingerprint := tunnel.Fingerprint(cert.Certificate[0])
	certPath := filepath.Join(certDir, "hub-cert.pem")

	app := &serverApp{
		store:        store,
		cert:         cert,
		fingerprint:  fingerprint,
		listen:       *listen,
		hubAddr:      *hubAddr,
		storePath:    resolvedStore,
		logPath:      logPath,
		certFile:     *certFile,
		keyFile:      *keyFile,
		settingsFile: settingsFile,
		tlsSelf:      *tlsSelf,
		certPath:     certPath,
	}

	log.Printf("отпечаток сертификата хаба: %s", fingerprint)
	if err := app.Start(); err != nil {
		if *guiFlag && !*consoleFlag {
			log.Printf("сервер не запущен: %v", err)
		} else {
			log.Fatalf("сервер не запущен: %v", err)
		}
	}

	if *guiFlag && !*consoleFlag {
		if err := runServerGUI(app, *minimized); err != nil {
			log.Printf("окно сервера: %v — работаем без окна", err)
			waitForSignal()
		}
	} else {
		waitForSignal()
	}

	log.Println("останавливаемся…")
	app.Stop()
}

// localOnlyListen отвечает, слушает ли сервер только петлевой адрес.
func localOnlyListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// certificateDir выбирает каталог для сертификата хаба: рядом с состоянием,
// а если он недоступен на запись (Program Files) — в ProgramData.
func certificateDir(storePath string) string {
	dir := filepath.Dir(storePath)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		test := filepath.Join(dir, ".posvpn-write-test")
		if file, err := os.Create(test); err == nil {
			file.Close()
			_ = os.Remove(test)
			return dir
		}
	}

	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	dir = filepath.Join(base, "POSvpn")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func waitForSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

// resolveStorePath ищет файл состояния. Если относительный путь не найден в
// текущем каталоге (частый случай при двойном клике по out\posvpn-server.exe),
// берётся файл в корне проекта — на уровень выше папки с exe.
func resolveStorePath(path string) string {
	exe, err := os.Executable()
	if err != nil {
		return resolveStorePathFor(path, "")
	}
	return resolveStorePathFor(path, exe)
}

func resolveStorePathFor(path, exe string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if exe == "" {
		return path
	}
	candidate := filepath.Join(filepath.Dir(filepath.Dir(exe)), path)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return path
}
