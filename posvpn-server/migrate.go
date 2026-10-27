package main

import (
	"log"
	"os"
	"path/filepath"

	"posvpn-engine/datadir"
)

// migrateStore переносит состояние из прежних мест в портативный каталог.
//
// Порядок важен: сначала ближайшие к программе файлы, затем ProgramData.
// Копируется только первый найденный — данные не сливаются, чтобы не
// получить дубли сетей с разными идентификаторами.
func migrateStore(target string) {
	if _, err := os.Stat(target); err == nil {
		return
	}
	for _, src := range legacyStorePaths() {
		if src == target {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := datadir.CopyIfMissing(src, target); err != nil {
			log.Printf("состояние не перенесено из %s: %v", src, err)
			continue
		}
		log.Printf("состояние перенесено: %s → %s", src, target)
		return
	}
}

// legacyStorePaths — места, где server.json мог остаться от прежних версий:
// каталог запуска, каталог exe и ProgramData.
func legacyStorePaths() []string {
	paths := []string{"server.json"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(dir, "server.json"))
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		paths = append(paths, filepath.Join(programData, "POSVpn", "server.json"))
	}
	return paths
}
