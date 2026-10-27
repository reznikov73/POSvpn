package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateStoreCopiesLegacy проверяет перенос состояния из каталога запуска
// в портативный каталог: без этого установленная копия стартовала с пустым
// состоянием и отвергала токены прежних клиентов.
func TestMigrateStoreCopiesLegacy(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "server.json")
	if err := os.WriteFile(old, []byte(`{"networks":[{"id":"n1","name":"rez"}],"next_subnet":2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	target := filepath.Join(dir, "data", "server.json")
	migrateStore(target)

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("состояние не перенесено: %v", err)
	}
	store, err := OpenStore(target)
	if err != nil {
		t.Fatal(err)
	}
	if store.Empty() {
		t.Fatal("перенесённое состояние пустое")
	}
}

// TestMigrateStoreKeepsExisting — существующий файл не перезаписывается.
func TestMigrateStoreKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.json"), []byte(`{"networks":[{"id":"old"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "data", "server.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"networks":[{"id":"new"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	migrateStore(target)

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"networks":[{"id":"new"}]}` {
		t.Fatalf("существующее состояние перезаписано: %s", data)
	}
}

// TestStoreEmpty — пустое состояние распознаётся, чтобы сервер записал подсказку.
func TestStoreEmpty(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Empty() {
		t.Fatal("новое состояние должно быть пустым")
	}
}
