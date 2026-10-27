// posvpn-server — координатор сети POSvpn: реестр сетей и узлов, выдача адресов,
// карта сети для клиентов и QUIC-хаб.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
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

// serverApp — состояние процесса: его показывает окно и по нему же
// выполняется корректная остановка.
type serverApp struct {
	store       *Store
	hub         *tunnel.Hub
	http        *http.Server
	listen      string
	hubAddr     string
	fingerprint string
	storePath   string
	logPath     string
	started     time.Time
	stopOnce    sync.Once
}

func (a *serverApp) shutdown() {
	a.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := a.http.Shutdown(ctx); err != nil {
			log.Printf("остановка HTTP: %v", err)
		}
		if a.hub != nil {
			_ = a.hub.Close()
		}
	})
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

	app := &serverApp{
		store:     store,
		listen:    *listen,
		hubAddr:   *hubAddr,
		storePath: *storePath,
		logPath:   logPath,
		started:   time.Now(),
	}

	if *hubAddr != "" {
		hub, fingerprint, err := StartHub(store, *hubAddr)
		if err != nil {
			log.Fatalf("хаб не запущен: %v", err)
		}
		app.hub = hub
		app.fingerprint = fingerprint
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           NewServer(store),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	app.http = srv

	useTLS := *certFile != "" && *keyFile != ""

	go func() {
		var err error
		if useTLS {
			log.Printf("POSvpn coordinator слушает https://%s", *listen)
			err = srv.ListenAndServeTLS(*certFile, *keyFile)
		} else {
			log.Printf("POSvpn coordinator слушает http://%s — ВНИМАНИЕ: без TLS, только для отладки", *listen)
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("сервер остановлен: %v", err)
		}
	}()

	if *guiFlag {
		if err := runServerGUI(app); err != nil {
			log.Printf("окно сервера: %v — работаем без окна", err)
			waitForSignal()
		}
	} else {
		waitForSignal()
	}

	log.Println("останавливаемся…")
	app.shutdown()
}

func waitForSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
