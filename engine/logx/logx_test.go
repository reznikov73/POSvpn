package logx

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingWriter изображает недействительный stderr службы.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("нет консоли") }

// TestSetupDirWritesFileWithoutConsole проверяет, что сбой второго приёмника
// не мешает записи в файл: раньше журнал службы оставался пустым.
func TestSetupDirWritesFileWithoutConsole(t *testing.T) {
	dir := t.TempDir()

	previous := console
	console = failingWriter{}
	t.Cleanup(func() {
		console = previous
		_ = Close()
	})

	path, err := SetupDir(dir, "service")
	if err != nil {
		t.Fatal(err)
	}

	log.Print("проверка записи")
	if err := Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "проверка записи") {
		t.Fatalf("запись не попала в файл: %q", data)
	}
}

// TestSetupDirCreatesDirectory — каталог журналов создаётся сам.
func TestSetupDirCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	t.Cleanup(func() { _ = Close() })

	path, err := SetupDir(dir, "client")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("путь журнала %s вне каталога %s", path, dir)
	}
}
