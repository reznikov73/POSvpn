package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"posvpn-engine/datadir"
)

// uiSettings — адреса, которые пользователь задал в окне сервера.
// Флаги командной строки имеют приоритет: если флаг передан явно, значение
// из файла не используется.
type uiSettings struct {
	Listen string `json:"listen,omitempty"`
	Hub    string `json:"hub,omitempty"`
	TLS    bool   `json:"tls,omitempty"`
}

// settingsPath — файл настроек окна рядом с журналами.
func settingsPath() string {
	if dir, ok := datadir.PortableDataDir(); ok {
		return filepath.Join(dir, "server-ui.json")
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil && home != "" {
			dir = filepath.Join(home, "AppData", "Local")
		} else {
			dir = os.TempDir()
		}
	}
	return filepath.Join(dir, "POSvpn", "server-ui.json")
}

func loadUISettings(path string) uiSettings {
	data, err := os.ReadFile(path)
	if err != nil {
		return uiSettings{}
	}
	var settings uiSettings
	if json.Unmarshal(data, &settings) != nil {
		return uiSettings{}
	}
	return settings
}

func saveUISettings(path string, settings uiSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("каталог настроек: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация настроек: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("запись настроек: %w", err)
	}
	return os.Rename(tmp, path)
}
