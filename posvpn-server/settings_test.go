package main

import (
	"path/filepath"
	"testing"
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

func TestUpdateEndpointsValidates(t *testing.T) {
	app := &serverApp{listen: "127.0.0.1:0", hubAddr: "127.0.0.1:0"}

	if err := app.UpdateEndpoints("без порта", ""); err == nil {
		t.Fatal("адрес без порта должен отвергаться")
	}
	if err := app.UpdateEndpoints("127.0.0.1:0", "тоже без порта"); err == nil {
		t.Fatal("адрес хаба без порта должен отвергаться")
	}
	if err := app.UpdateEndpoints("", ""); err == nil {
		t.Fatal("пустой адрес контроля должен отвергаться")
	}
}
