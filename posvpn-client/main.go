// posvpn-client — клиент POSvpn. На этом этапе это CLI-каркас: создание сети,
// вход в неё и просмотр карты. Виртуальный адаптер и туннель — следующий этап
// (см. docs/05-план.md).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"posvpn-engine"
)

const defaultServer = "http://127.0.0.1:8787"

// config — локальное состояние клиента. Приватный ключ узла никогда не уходит
// на сервер: туда отправляется только публичная половина.
type config struct {
	Server     string `json:"server"`
	NodeID     string `json:"node_id"`
	Token      string `json:"token"`
	Network    string `json:"network"`
	IP         string `json:"ip"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "create":
		err = runAuth(os.Args[2:], true)
	case "join":
		err = runAuth(os.Args[2:], false)
	case "status", "peers":
		err = runStatus(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("неизвестная команда %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `POSvpn — виртуальная локальная сеть

  posvpn-client create -network <имя> -password <пароль> [-node <имя ПК>] [-server <url>]
  posvpn-client join   -network <имя> -password <пароль> [-node <имя ПК>] [-server <url>]
  posvpn-client status
`)
}

func runAuth(args []string, create bool) error {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	server := fs.String("server", "", "адрес координатора (по умолчанию "+defaultServer+")")
	network := fs.String("network", "", "имя сети")
	password := fs.String("password", "", "пароль сети")
	nodeName := fs.String("node", hostname(), "имя этого компьютера в сети")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*network) == "" {
		return errors.New("не задано имя сети: -network")
	}
	if len(*password) < 8 {
		return errors.New("пароль сети короче 8 символов")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if *server != "" {
		cfg.Server = strings.TrimRight(*server, "/")
	}
	if cfg.Server == "" {
		cfg.Server = defaultServer
	}

	// Ключ узла переиспользуем: он и есть долговременная идентичность клиента.
	if cfg.PrivateKey == "" {
		pair, err := engine.GenerateKeyPair()
		if err != nil {
			return err
		}
		cfg.PrivateKey, cfg.PublicKey = pair.Private, pair.Public
	}

	var (
		endpoint string
		payload  any
	)
	if create {
		endpoint = "/api/v1/networks"
		payload = engine.CreateNetworkRequest{
			Name: *network, Password: *password, NodeName: *nodeName, PublicKey: cfg.PublicKey,
		}
	} else {
		endpoint = "/api/v1/networks/join"
		payload = engine.JoinNetworkRequest{
			Name: *network, Password: *password, NodeName: *nodeName, PublicKey: cfg.PublicKey,
		}
	}

	var resp engine.AuthResponse
	if err := call(http.MethodPost, cfg.Server+endpoint, "", payload, &resp); err != nil {
		return err
	}

	cfg.Token = resp.Token
	cfg.NodeID = resp.NodeID
	cfg.Network = resp.NetMap.NetworkName
	cfg.IP = resp.NetMap.Self.IP
	if err := saveConfig(cfg); err != nil {
		return err
	}

	action := "вошли в сеть"
	if create {
		action = "создали сеть"
	}
	fmt.Printf("Готово: %s %q, ваш адрес %s\n\n", action, resp.NetMap.NetworkName, cfg.IP)
	printNetMap(resp.NetMap)
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return errors.New("клиент ещё не входил в сеть: выполните create или join")
	}

	var netmap engine.NetMap
	if err := call(http.MethodGet, cfg.Server+"/api/v1/netmap", cfg.Token, nil, &netmap); err != nil {
		return err
	}

	fmt.Printf("Сеть %q, подсеть %s, версия карты %d\n", netmap.NetworkName, netmap.Subnet, netmap.Version)
	printNetMap(netmap)
	return nil
}

func printNetMap(netmap engine.NetMap) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ИМЯ\tАДРЕС\tРОЛЬ\tСОСТОЯНИЕ\tКАНАЛ")
	fmt.Fprintf(w, "%s (вы)\t%s\t%s\tонлайн\t—\n", netmap.Self.Name, netmap.Self.IP, netmap.Self.Role)
	for _, peer := range netmap.Peers {
		state := "офлайн"
		if peer.Online {
			state = "онлайн"
		}
		channel := "—"
		if peer.Endpoint != "" {
			channel = peer.Endpoint
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", peer.Name, peer.IP, peer.Role, state, channel)
	}
	w.Flush()
}

func call(method, url, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("сериализация запроса: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return fmt.Errorf("запрос: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("координатор недоступен (%s): %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("чтение ответа: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr engine.APIError
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("%s (код %d)", apiErr.Error, resp.StatusCode)
		}
		return fmt.Errorf("координатор вернул код %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("разбор ответа: %w", err)
		}
	}
	return nil
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить каталог настроек: %w", err)
	}
	return filepath.Join(dir, "POSvpn", "client.json"), nil
}

func loadConfig() (*config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение настроек: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разбор настроек %s: %w", path, err)
	}
	return &cfg, nil
}

func saveConfig(cfg *config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("каталог настроек: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация настроек: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("запись настроек: %w", err)
	}
	return nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "posvpn-pc"
	}
	return name
}
