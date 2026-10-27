// Package logx настраивает журнал в файл с ротацией.
//
// Консоль у GUI скрыта, у службы её нет вовсе, поэтому журнал — единственный
// способ понять, что произошло. Файлы лежат в %LocalAppData%\POSvpn\logs
// (на Linux — ~/.cache/POSvpn/logs).
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	// maxSize — после этого размера файл прокручивается.
	maxSize = 10 << 20
	// backups — сколько старых файлов хранить.
	backups = 3
)

// Setup открывает журнал компонента и направляет в него стандартный log.
// Возвращает путь к файлу, чтобы показать его в интерфейсе.
func Setup(component string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		// В службе и в урезанном окружении %LocalAppData% может отсутствовать.
		home, homeErr := os.UserHomeDir()
		if homeErr == nil && home != "" {
			dir = filepath.Join(home, "AppData", "Local")
		} else {
			dir = os.TempDir()
		}
	}
	return SetupDir(filepath.Join(dir, "POSvpn", "logs"), component)
}

// console — второй приёмник журнала. В тестах подменяется, чтобы изобразить
// процесс без консоли.
var console io.Writer = os.Stderr

// current — открытый файл журнала: держим его, чтобы закрыть при выходе
// или перед открытием нового (тесты и повторная настройка).
var (
	currentMu sync.Mutex
	current   *rotatingWriter
)

// Close закрывает текущий файл журнала.
func Close() error {
	currentMu.Lock()
	defer currentMu.Unlock()
	if current == nil {
		return nil
	}
	err := current.close()
	current = nil
	return err
}

// SetupDir открывает журнал в заданном каталоге. Служба пишет в ProgramData.
func SetupDir(dir, component string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("каталог журналов: %w", err)
	}

	path := filepath.Join(dir, component+".log")
	writer, err := newRotatingWriter(path, maxSize, backups)
	if err != nil {
		return "", err
	}

	log.SetFlags(log.LstdFlags)
	// Файл — первым: у службы и у скрытого окна stderr может быть недействителен,
	// и при порядке «stderr, файл» MultiWriter обрывает запись на первой ошибке.
	log.SetOutput(io.MultiWriter(writer, quietWriter{console}))

	currentMu.Lock()
	previous := current
	current = writer
	currentMu.Unlock()
	if previous != nil {
		_ = previous.close()
	}
	return path, nil
}

// quietWriter глушит ошибки второго приёмника журнала: отсутствие консоли
// не должно отнимать записи у файла.
type quietWriter struct{ w io.Writer }

func (q quietWriter) Write(p []byte) (int, error) {
	if q.w == nil {
		return len(p), nil
	}
	_, _ = q.w.Write(p)
	return len(p), nil
}

func (w *rotatingWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// rotatingWriter пишет в файл и прокручивает его по размеру.
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	max     int64
	backups int
	file    *os.File
	size    int64
}

func newRotatingWriter(path string, max int64, backups int) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, max: max, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("журнал %s: %w", w.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("журнал %s: %w", w.path, err)
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.max {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	// path.2 → path.3, path.1 → path.2, path → path.1
	for i := w.backups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", w.path, i)
		next := fmt.Sprintf("%s.%d", w.path, i+1)
		_ = os.Remove(next)
		_ = os.Rename(old, next)
	}
	_ = os.Remove(w.path + ".1")
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return err
	}
	return w.open()
}
