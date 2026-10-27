package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWritable — обычный каталог доступен для записи, отсутствующий — нет.
func TestWritable(t *testing.T) {
	if !Writable(t.TempDir()) {
		t.Fatal("временный каталог должен быть доступен для записи")
	}
	if Writable(filepath.Join(t.TempDir(), "нет-такого")) {
		t.Fatal("несуществующий каталог не может быть доступен для записи")
	}
}

// TestCopyIfMissing копирует только при отсутствии цели: так перенос
// настроек не затирает уже созданные рядом с программой.
func TestCopyIfMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "data", "client.json")
	if err := os.WriteFile(src, []byte(`{"token":"a"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CopyIfMissing(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != `{"token":"a"}` {
		t.Fatalf("копия не создана: %s, %v", data, err)
	}

	if err := os.WriteFile(src, []byte(`{"token":"b"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CopyIfMissing(src, dst); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(dst)
	if string(data) != `{"token":"a"}` {
		t.Fatalf("существующий файл перезаписан: %s", data)
	}
}

// TestCopyIfMissingWithoutSource — отсутствующий источник не ошибка запуска.
func TestCopyIfMissingWithoutSource(t *testing.T) {
	dir := t.TempDir()
	if err := CopyIfMissing(filepath.Join(dir, "нет.json"), filepath.Join(dir, "data", "x.json")); err == nil {
		t.Fatal("ожидалась ошибка чтения источника")
	}
	if err := CopyIfMissing("", filepath.Join(dir, "y.json")); err != nil {
		t.Fatalf("пустой источник должен игнорироваться: %v", err)
	}
}
