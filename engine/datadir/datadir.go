// Package datadir выбирает каталог для настроек и журналов.
//
// Если рядом с exe лежит файл portable.txt, программа хранит данные в
// подкаталоге data — так установка в Program Files остаётся самодостаточной.
// Иначе используется профиль пользователя (клиент) или ProgramData (сервер).
package datadir

import (
	"os"
	"path/filepath"
)

// PortableDataDir возвращает <каталог exe>\data, если рядом есть portable.txt
// и каталог доступен на запись. Иначе ok=false.
func PortableDataDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	base := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(base, "portable.txt")); err != nil {
		return "", false
	}

	dir := filepath.Join(base, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	if !Writable(dir) {
		return "", false
	}
	return dir, true
}

// Writable проверяет, можно ли создать файл в каталоге.
func Writable(dir string) bool {
	file, err := os.CreateTemp(dir, "posvpn-write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

// CopyIfMissing копирует файл, если целевого ещё нет. Ошибки чтения источника
// игнорируются: миграция не должна мешать запуску.
func CopyIfMissing(src, dst string) error {
	if src == "" || dst == "" {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
