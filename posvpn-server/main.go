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
	"sync"
	"syscall"
	"time"

	"posvpn-engine/console"
	"posvpn-engine/logx"
	"posvpn-engine/tunnel"
)

// serverApp — процесс сервера. Его можно останавливать и снова запускать
// из окна, не закрывая программу.
type serverApp struct {
	store       *Store
	cert        tls.Certificate
	fingerprint string

	listen    string
	hubAddr   string
	storePath string
	logPath   string
	certFile  string
	keyFile   string

	mu        sync.Mutex
	running   bool
	hub       *tunnel.Hub
	http      *http.Server
	startedAt time.Time
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
		return fmt.Errorf("порт %s: %w", a.listen, err)
	}

	var hub *tunnel.Hub
	if a.hubAddr != "" {
		hub, err = StartHub(a.store, a.hubAddr, a.cert)
		if err != nil {
			_ = listener.Close()
			return err
		}
	}

	a.http = srv
	a.hub = hub
	a.running = true
	a.startedAt = time.Now()

	go func() {
		var serveErr error
		if a.certFile != "" && a.keyFile != "" {
			log.Printf("POSvpn coordinator слушает https://%s", a.listen)
			serveErr = srv.ServeTLS(listener, a.certFile, a.keyFile)
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
	logPath, _ := logx.Setup("server")

	listen := flag.String("listen", "127.0.0.1:8787", "адрес и порт control-сервера")
	hubAddr := flag.String("hub", "0.0.0.0:8788", "адрес QUIC-хаба; пусто — не запускать")
	storePath := flag.String("store", "server.json", "файл состояния координатора")
	certFile := flag.String("tls-cert", "", "сертификат TLS; пусто — работа без TLS (только для локальной отладки)")
	keyFile := flag.String("tls-key", "", "ключ TLS")
	guiFlag := flag.Bool("gui", false, "открыть окно сервера (Windows)")
	flag.Parse()

	store, err := OpenStore(*storePath)
	if err != nil {
		log.Fatalf("состояние не загружено: %v", err)
	}

	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		log.Fatalf("сертификат хаба: %v", err)
	}
	fingerprint := tunnel.Fingerprint(cert.Certificate[0])

	app := &serverApp{
		store:       store,
		cert:        cert,
		fingerprint: fingerprint,
		listen:      *listen,
		hubAddr:     *hubAddr,
		storePath:   *storePath,
		logPath:     logPath,
		certFile:    *certFile,
		keyFile:     *keyFile,
	}

	log.Printf("отпечаток сертификата хаба: %s", fingerprint)
	if err := app.Start(); err != nil {
		log.Fatalf("сервер не запущен: %v", err)
	}

	if *guiFlag {
		if err := runServerGUI(app); err != nil {
			log.Printf("окно сервера: %v — работаем без окна", err)
			waitForSignal()
		}
	} else {
		waitForSignal()
	}

	log.Println("останавливаемся…")
	app.Stop()
}

func waitForSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
