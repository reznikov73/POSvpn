// posvpn-server — координатор сети POSvpn: реестр сетей и узлов, выдача адресов,
// карта сети для клиентов. На этом этапе — только control plane.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"posvpn-engine/console"
)

func main() {
	console.UTF8()
	listen := flag.String("listen", "127.0.0.1:8787", "адрес и порт control-сервера")
	hubAddr := flag.String("hub", "0.0.0.0:8788", "адрес QUIC-хаба; пусто — не запускать")
	storePath := flag.String("store", "server.json", "файл состояния координатора")
	certFile := flag.String("tls-cert", "", "сертификат TLS; пусто — работа без TLS (только для локальной отладки)")
	keyFile := flag.String("tls-key", "", "ключ TLS")
	flag.Parse()

	store, err := OpenStore(*storePath)
	if err != nil {
		log.Fatalf("состояние не загружено: %v", err)
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           NewServer(store),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	useTLS := *certFile != "" && *keyFile != ""

	if *hubAddr != "" {
		hub, err := StartHub(store, *hubAddr)
		if err != nil {
			log.Fatalf("хаб не запущен: %v", err)
		}
		defer hub.Close()
	}

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

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("останавливаемся…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("не удалось корректно остановить сервер: %v", err)
	}
}
