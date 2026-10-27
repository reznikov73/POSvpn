package main

import (
	"os"
	"path/filepath"
	"testing"

	"posvpn-engine/tunnel"
)

func TestResolveStorePathFromOutDir(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, "server.json")
	if err := os.WriteFile(store, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := resolveStorePathFor("server.json", filepath.Join(outDir, "posvpn-server.exe"))
	if got != store {
		t.Fatalf("получили %q, ждали %q", got, store)
	}
}

func TestServerAppStartStop(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		t.Fatal(err)
	}

	app := &serverApp{
		store:   store,
		cert:    cert,
		listen:  "127.0.0.1:0",
		hubAddr: "127.0.0.1:0",
	}

	if err := app.Start(); err != nil {
		t.Fatalf("запуск: %v", err)
	}
	if !app.Running() {
		t.Fatal("после запуска сервер должен быть активен")
	}

	// Повторный запуск не должен падать и плодить слушателей.
	if err := app.Start(); err != nil {
		t.Fatalf("повторный запуск: %v", err)
	}

	app.Stop()
	if app.Running() {
		t.Fatal("после остановки сервер должен быть выключен")
	}

	// Повторная остановка безопасна.
	app.Stop()

	// И сервер можно поднять снова — кнопка ON/OFF в окне делает именно это.
	if err := app.Start(); err != nil {
		t.Fatalf("запуск после остановки: %v", err)
	}
	if !app.Running() {
		t.Fatal("сервер не поднялся заново")
	}
	app.Stop()
}
