package mobile

import (
        "bytes"
        "encoding/base64"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "net/netip"
        "net/url"
        "os"
        "path/filepath"
        "strings"
        "time"

        "posvpn-engine"
        "posvpn-engine/tunnel"
)

// errNotJoined — узел не входил в сеть.
var errNotJoined = errors.New("нет входа в сеть: создайте сеть или войдите в настройках")

// defaultServer — значение по умолчанию, совпадает с posvpn-client.
const defaultServer = "http://127.0.0.1:8787"

// dataDir — каталог настроек; задаётся из Android (getFilesDir).
var dataDir string

func setDataDir(dir string) {
        if dir != "" {
                dataDir = dir
        }
}

func configPath() string {
        if dataDir == "" {
                return "client.json"
        }
        return filepath.Join(dataDir, "client.json")
}

// config — локальное состояние клиента, как в posvpn-client/main.go.
// Приватный ключ не покидает устройство.
type config struct {
        Server     string `json:"server"`
        Hub        string `json:"hub,omitempty"`
        HubPin     string `json:"hub_pin,omitempty"`
        NodeID     string `json:"node_id"`
        Node       string `json:"node,omitempty"`
        Token      string `json:"token"`
        Network    string `json:"network"`
        IP         string `json:"ip"`
        Index      uint32 `json:"index,omitempty"`
        PublicKey  string `json:"public_key"`
        PrivateKey string `json:"private_key"`
        // Gateway — IP участника сети, через который этот узел выходит в
        // интернет (пусто = трафик вне подсети отбрасывается).
        Gateway string `json:"gateway,omitempty"`
}

// configInfo — сводка для Java (ConfigInfo).
type configInfo struct {
        Server  string `json:"server"`
        Hub     string `json:"hub"`
        HubPin  string `json:"hub_pin,omitempty"`
        Network string `json:"network"`
        Node    string `json:"node"`
        IP      string `json:"ip"`
        Index   uint32 `json:"index"`
        Gateway string `json:"gateway,omitempty"`
        Joined  bool   `json:"joined"`
}

func loadConfig() (*config, error) {
        data, err := os.ReadFile(configPath())
        if errors.Is(err, os.ErrNotExist) {
                return &config{}, nil
        }
        if err != nil {
                return nil, fmt.Errorf("чтение настроек: %w", err)
        }
        var cfg config
        if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, fmt.Errorf("разбор настроек: %w", err)
        }
        return &cfg, nil
}

func saveConfig(cfg *config) error {
        if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
                return fmt.Errorf("каталог настроек: %w", err)
        }
        data, err := json.MarshalIndent(cfg, "", "  ")
        if err != nil {
                return fmt.Errorf("сериализация настроек: %w", err)
        }
        if err := os.WriteFile(configPath(), data, 0o600); err != nil {
                return fmt.Errorf("запись настроек: %w", err)
        }
        return nil
}

// normalizeServer подрезает хвостовой слэш и подставляет умолчание.
func normalizeServer(server string) string {
        server = strings.TrimRight(strings.TrimSpace(server), "/")
        if server == "" {
                return defaultServer
        }
        return server
}

// hubOrDefault выводит адрес хаба из адреса координатора: тот же хост, порт 8788.
func hubOrDefault(cfg *config) string {
        if cfg.Hub != "" {
                return cfg.Hub
        }
        parsed, err := url.Parse(cfg.Server)
        if err != nil || parsed.Hostname() == "" {
                return "127.0.0.1:8788"
        }
        return parsed.Hostname() + ":8788"
}

// setGateway запоминает шлюз по умолчанию: IP участника сети, через который
// этот узел выходит в интернет. Пустая строка отключает выход.
func setGateway(gateway string) error {
        gateway = strings.TrimSpace(gateway)
        if gateway != "" {
                addr, err := netip.ParseAddr(gateway)
                if err != nil || !addr.Is4() {
                        return fmt.Errorf("шлюз %q: ожидался IPv4-адрес вида 10.77.0.1", gateway)
                }
                gateway = addr.String()
        }
        cfg, err := loadConfig()
        if err != nil {
                return err
        }
        cfg.Gateway = gateway
        return saveConfig(cfg)
}

// auth — создание сети или вход в неё (порт runAuth из posvpn-client).
func auth(server, hub, hubPin, network, password, nodeName string, create bool) error {
        if strings.TrimSpace(network) == "" {
                return errors.New("не задано имя сети")
        }
        if len(password) < 8 {
                return errors.New("пароль сети короче 8 символов")
        }
        nodeName = strings.TrimSpace(nodeName)
        if nodeName == "" {
                nodeName = defaultNodeName()
        }

        cfg, err := loadConfig()
        if err != nil {
                return err
        }
        if server != "" {
                cfg.Server = normalizeServer(server)
        }
        if cfg.Server == "" {
                cfg.Server = defaultServer
        }
        if hub != "" {
                cfg.Hub = hub
        }
        if hubPin != "" {
                cfg.HubPin = strings.TrimSpace(hubPin)
        }

        // Ключ узла переиспользуем: это долговременная идентичность устройства.
        if cfg.PrivateKey == "" {
                pair, err := engine.GenerateKeyPair()
                if err != nil {
                        return err
                }
                cfg.PrivateKey, cfg.PublicKey = pair.Private, pair.Public
        }

        var payload any
        endpoint := "/api/v1/networks/join"
        if create {
                endpoint = "/api/v1/networks"
                payload = engine.CreateNetworkRequest{
                        Name: network, Password: password, NodeName: nodeName, PublicKey: cfg.PublicKey,
                }
        } else {
                payload = engine.JoinNetworkRequest{
                        Name: network, Password: password, NodeName: nodeName, PublicKey: cfg.PublicKey,
                }
        }

        var resp engine.AuthResponse
        if err := call(http.MethodPost, cfg.Server+endpoint, "", payload, &resp, cfg); err != nil {
                return err
        }

        cfg.Token = resp.Token
        cfg.NodeID = resp.NodeID
        cfg.Network = resp.NetMap.NetworkName
        cfg.IP = resp.NetMap.Self.IP
        cfg.Index = resp.NetMap.Self.Index
        cfg.Node = nodeName
        return saveConfig(cfg)
}

// leaveNetwork — выход из сети (порт leaveNetwork из posvpn-client).
func leaveNetwork() error {
        cfg, err := loadConfig()
        if err != nil {
                return err
        }
        if cfg.Token == "" {
                return errors.New("сеть не выбрана: выходить не из чего")
        }
        if err := call(http.MethodPost, cfg.Server+"/api/v1/leave", cfg.Token, nil, nil, cfg); err != nil {
                // Сеть уже удалена или вход отозван — цель достигнута, чистим настройки.
                if !strings.Contains(err.Error(), "сеть удалена или вход отозван") {
                        return err
                }
        }
        cfg.Token = ""
        cfg.Network = ""
        cfg.IP = ""
        cfg.Index = 0
        cfg.NodeID = ""
        return saveConfig(cfg)
}

// fetchNetMap запрашивает актуальную карту сети.
func fetchNetMap(cfg *config) (engine.NetMap, error) {
        var netmap engine.NetMap
        if err := call(http.MethodGet, cfg.Server+"/api/v1/netmap", cfg.Token, nil, &netmap, cfg); err != nil {
                return engine.NetMap{}, err
        }
        return netmap, nil
}

// call выполняет JSON-запрос к координатору (порт call из posvpn-client).
func call(method, rawURL, token string, body, out any, cfg *config) error {
        var reader io.Reader
        if body != nil {
                raw, err := json.Marshal(body)
                if err != nil {
                        return fmt.Errorf("сериализация запроса: %w", err)
                }
                reader = bytes.NewReader(raw)
        }
        req, err := http.NewRequest(method, rawURL, reader)
        if err != nil {
                return fmt.Errorf("запрос: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if token != "" {
                req.Header.Set("Authorization", "Bearer "+token)
        }

        client := apiClient(rawURL, cfg)
        resp, err := client.Do(req)
        if err != nil {
                return fmt.Errorf("координатор недоступен (%s): %w", rawURL, err)
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

// apiClient — HTTP-клиент; для https с отпечатком проверяем именно заданный
// сертификат (самоподписанный координатор).
func apiClient(rawURL string, cfg *config) *http.Client {
        client := &http.Client{Timeout: 15 * time.Second}
        if strings.HasPrefix(rawURL, "https://") && cfg != nil && cfg.HubPin != "" {
                client.Transport = &http.Transport{TLSClientConfig: tunnel.PinTLSConfig(cfg.HubPin)}
        }
        return client
}

// optionsFromConfig собирает опции туннеля (порт tunnelOptionsFromConfig).
func optionsFromConfig(cfg *config, netmap engine.NetMap, mtu int) (tunnelOptions, error) {
        if netmap.Self.Index == 0 {
                return tunnelOptions{}, errors.New("сервер не выдал числовой индекс узла: обновите сервер POSvpn")
        }
        subnet, err := netip.ParsePrefix(netmap.Subnet)
        if err != nil {
                return tunnelOptions{}, fmt.Errorf("сервер прислал некорректную подсеть %q: %w", netmap.Subnet, err)
        }
        privateKey, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
        if err != nil || len(privateKey) != 32 {
                return tunnelOptions{}, errors.New("в настройках нет корректного приватного ключа: войдите в сеть заново")
        }
        if mtu <= 0 {
                mtu = tunnel.DefaultMTU
        }
        var gateway netip.Addr
        if cfg.Gateway != "" {
                gateway, err = netip.ParseAddr(cfg.Gateway)
                if err != nil || !gateway.Is4() {
                        return tunnelOptions{}, fmt.Errorf("шлюз %q: ожидался IPv4-адрес", cfg.Gateway)
                }
        }
        return tunnelOptions{
                Server:     cfg.Server,
                HubAddr:    hubOrDefault(cfg),
                Token:      cfg.Token,
                NetworkID:  netmap.NetworkID,
                Subnet:     subnet,
                SelfIndex:  netmap.Self.Index,
                SelfIP:     netmap.Self.IP,
                PrivateKey: privateKey,
                Pin:        cfg.HubPin,
                MTU:        mtu,
                NetMap:     netmap,
                GatewayIP:  gateway,
        }, nil
}

// defaultNodeName — имя устройства по умолчанию.
func defaultNodeName() string {
        name, err := os.Hostname()
        if err != nil || name == "" {
                return "android"
        }
        return name
}
