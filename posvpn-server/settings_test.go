package main

import (
	"path/filepath"
	"testing"

	"posvpn-engine/tunnel"
)

func TestUISettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-ui.json")
	want := uiSettings{Listen: "0.0.0.0:9000", Hub: "0.0.0.0:9001"}
	if err := saveUISettings(path, want); err != nil {
		t.Fatal(err)
	}
	got := loadUISettings(path)
	if got != want {
		t.Fatalf("получили %+v, ждали %+v", got, want)
	}
}

func TestUpdateSettingsTogglesTLS(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		t.Fatal(err)
	}

	app := &serverApp{store: store, cert: cert, listen: "127.0.0.1:0"}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Stop()

	if err := app.UpdateSettings("127.0.0.1:0", "", true); err != nil {
		t.Fatalf("включение TLS: %v", err)
	}
	if !app.TLS() || !app.Running() {
		t.Fatal("после включения TLS сервер должен работать по https")
	}

	if err := app.UpdateSettings("127.0.0.1:0", "", false); err != nil {
		t.Fatalf("выключение TLS: %v", err)
	}
	if app.TLS() || !app.Running() {
		t.Fatal("после выключения TLS сервер должен работать по http")
	}
}

func TestUpdateEndpointsValidates(t *testing.T) {
	app := &serverApp{listen: "127.0.0.1:0", hubAddr: "127.0.0.1:0"}

	if err := app.UpdateSettings("без порта", "", false); err == nil {
		t.Fatal("адрес без порта должен отвергаться")
	}
	if err := app.UpdateSettings("127.0.0.1:0", "тоже без порта", false); err == nil {
		t.Fatal("адрес хаба без порта должен отвергаться")
	}
	if err := app.UpdateSettings("", "", false); err == nil {
		t.Fatal("пустой адрес контроля должен отвергаться")
	}
}
