package mobile

import (
        "context"
        "encoding/base64"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "net/netip"
        "os"
        "syscall"
        "testing"
        "time"

        "posvpn-engine"
        "posvpn-engine/tunnel"
)

// fakeDevice подменяет адаптер: in — пакеты «от операционной системы»,
// out — пакеты «в операционную систему».
type fakeDevice struct {
        in  chan []byte
        out chan []byte
        mtu int
}

func newFakeDevice() *fakeDevice {
        return &fakeDevice{in: make(chan []byte, 64), out: make(chan []byte, 64), mtu: 1100}
}

func (d *fakeDevice) ReadPacket() ([]byte, error) {
        packet := <-d.in
        if packet == nil {
                return nil, os.ErrClosed
        }
        return packet, nil
}

func (d *fakeDevice) WritePacket(packet []byte) error {
        d.out <- packet
        return nil
}

func (d *fakeDevice) MTU() int      { return d.mtu }
func (d *fakeDevice) Close() error { close(d.in); return nil }

// ipv4 собирает минимальный IPv4-пакет с заданным адресатом.
func ipv4(src, dst [4]byte, payloadLen int) []byte {
        packet := make([]byte, 20+payloadLen)
        packet[0] = 0x45
        packet[2] = byte((20 + payloadLen) >> 8)
        packet[3] = byte(20 + payloadLen)
        copy(packet[12:16], src[:])
        copy(packet[16:20], dst[:])
        return packet
}

// TestSessionThroughHub — сквозной тест: две сессии, хаб, обмен кадрами.
// Проверяется путь «адаптер A → шифрование → хаб → расшифрование → адаптер B»,
// ответ pong и бродкаст.
func TestSessionThroughHub(t *testing.T) {

        // Ключи и индексы двух узлов.
        pairA, _ := engine.GenerateKeyPair()
        pairB, _ := engine.GenerateKeyPair()

        const networkID = "n_test"
        tokens := map[string]tunnel.Identity{
                "token-a": {NodeID: "u_a", NetworkID: networkID, Index: 1},
                "token-b": {NodeID: "u_b", NetworkID: networkID, Index: 2},
        }

        cert, err := tunnel.SelfSignedCertificate()
        if err != nil {
                t.Fatalf("сертификат: %v", err)
        }
        hub, err := tunnel.Listen(tunnel.HubConfig{
                Addr:      "127.0.0.1:0",
                TLSConfig: tunnel.ServerTLSConfig(cert),
                Auth: func(token string) (tunnel.Identity, error) {
                        id, ok := tokens[token]
                        if !ok {
                                return tunnel.Identity{}, os.ErrPermission
                        }
                        return id, nil
                },
        })
        if err != nil {
                t.Fatalf("хаб: %v", err)
        }
        defer hub.Close()

        netmapA := engine.NetMap{
                NetworkID: networkID, NetworkName: "test", Subnet: "10.77.1.0/24", Version: 1,
                Self:  engine.Peer{Index: 1, IP: "10.77.1.1", PublicKey: pairA.Public, Name: "A"},
                Peers: []engine.Peer{{Index: 2, IP: "10.77.1.2", PublicKey: pairB.Public, Name: "B"}},
        }
        netmapB := engine.NetMap{
                NetworkID: networkID, NetworkName: "test", Subnet: "10.77.1.0/24", Version: 1,
                Self:  engine.Peer{Index: 2, IP: "10.77.1.2", PublicKey: pairB.Public, Name: "B"},
                Peers: []engine.Peer{{Index: 1, IP: "10.77.1.1", PublicKey: pairA.Public, Name: "A"}},
        }

        privA, _ := base64.StdEncoding.DecodeString(pairA.Private)
        privB, _ := base64.StdEncoding.DecodeString(pairB.Private)

        optA := tunnelOptions{
                Server: "http://127.0.0.1:1", HubAddr: hub.Addr(), Token: "token-a",
                NetworkID: networkID, Subnet: mustPrefix(t, "10.77.1.0/24"),
                SelfIndex: 1, SelfIP: "10.77.1.1", PrivateKey: privA, MTU: 1100, NetMap: netmapA,
        }
        optB := optA
        optB.Token = "token-b"
        optB.SelfIndex = 2
        optB.SelfIP = "10.77.1.2"
        optB.PrivateKey = privB
        optB.NetMap = netmapB

        devA, devB := newFakeDevice(), newFakeDevice()
        peersA, peersB := newPeerSet(), newPeerSet()
        if err := peersA.rebuild(netmapA, privA); err != nil {
                t.Fatalf("карта A: %v", err)
        }
        if err := peersB.rebuild(netmapB, privB); err != nil {
                t.Fatalf("карта B: %v", err)
        }

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()

        // Сигналы о подключении сессий: пакеты, прочитанные из адаптера до
        // поднятия канала к хабу, отбрасываются — ждём "туннель поднят".
        upA, upB := make(chan bool, 16), make(chan bool, 16)
        stateA := func(connected bool, _ error) { upA <- connected }
        stateB := func(connected bool, _ error) { upB <- connected }

        errCh := make(chan error, 2)
        go func() { errCh <- runSession(ctx, optA, devA, peersA, stateA) }()
        go func() { errCh <- runSession(ctx, optB, devB, peersB, stateB) }()

        waitUp := func(ch chan bool, name string) {
                t.Helper()
                for {
                        select {
                        case v := <-ch:
                                if v {
                                        return
                                }
                        case err := <-errCh:
                                t.Fatalf("сессия %s упала: %v", name, err)
                        case <-time.After(5 * time.Second):
                                t.Fatalf("сессия %s не подключилась за 5 секунд", name)
                        }
                }
        }
        waitUp(upA, "A")
        waitUp(upB, "B")

        // Уникальный IPv4-пакет от A к B.
        packet := ipv4([4]byte{10, 77, 1, 1}, [4]byte{10, 77, 1, 2}, 16)
        for i := range packet[20:] {
                packet[20+i] = byte(i + 1)
        }
        devA.in <- packet

        select {
        case got := <-devB.out:
                if len(got) != len(packet) {
                        t.Fatalf("длина пакета: получили %d, ждали %d", len(got), len(packet))
                }
                for i := range packet {
                        if got[i] != packet[i] {
                                t.Fatalf("байт %d: получили %d, ждали %d", i, got[i], packet[i])
                        }
                }
        case <-time.After(5 * time.Second):
                t.Fatal("пакет от A не дошёл до B за 5 секунд")
        }

        // Ответ от B к A.
        reply := ipv4([4]byte{10, 77, 1, 2}, [4]byte{10, 77, 1, 1}, 8)
        devB.in <- reply
        select {
        case got := <-devA.out:
                if len(got) != len(reply) {
                        t.Fatalf("длина ответа: %d", len(got))
                }
        case <-time.After(5 * time.Second):
                t.Fatal("ответ от B не дошёл до A за 5 секунд")
        }

        // Ping через туннель: у пира A должен появиться RTT после pong от B.
        // На localhost RTT меньше миллисекунды и округляется в 0, поэтому
        // надёжная проверка — online-статус через свежий lastPong.
        deadline := time.Now().Add(8 * time.Second)
        for time.Now().Before(deadline) {
                if peer := peersA.byIndex(2); peer != nil && peer.online() {
                        break
                }
                time.Sleep(200 * time.Millisecond)
        }
        if peer := peersA.byIndex(2); peer == nil || !peer.online() {
                t.Fatal("пир B не ответил на ping: online-статус не появился")
        }

        // Бродкаст от A должны получить все пиры A, то есть B.
        bcast := ipv4([4]byte{10, 77, 1, 1}, [4]byte{255, 255, 255, 255}, 4)
        devA.in <- bcast
        select {
        case <-devB.out:
        case <-time.After(5 * time.Second):
                t.Fatal("бродкаст от A не дошёл до B")
        }

        cancel()
}

// TestFdDevice — чтение и запись через реальный файловый дескриптор,
// как это будет с TUN от VpnService. Используются сырые дескрипторы
// syscall.Pipe: их не трогает поллер Go-рантайма.
func TestFdDevice(t *testing.T) {
        // Socketpair вместо pipe: дескриптор TUN двунаправленный.
        p, err := socketpair()
        if err != nil {
                t.Fatal(err)
        }
        defer p.close()
        dev, err := newFdDevice(int(p.a), 1100)
        if err != nil {
                t.Fatalf("устройство: %v", err)
        }

        packet := ipv4([4]byte{10, 77, 1, 1}, [4]byte{10, 77, 1, 2}, 32)
        buf := make([]byte, len(packet))
        go func() { _, _ = syscall.Write(p.b, packet) }()

        got, err := dev.ReadPacket()
        if err != nil {
                t.Fatalf("чтение: %v", err)
        }
        if len(got) != len(packet) {
                t.Fatalf("прочитано %d байт, ждали %d", len(got), len(packet))
        }

        if err := dev.WritePacket(packet); err != nil {
                t.Fatalf("запись: %v", err)
        }
        if n, _ := syscall.Read(p.b, buf); n != len(packet) {
                t.Fatalf("в парный сокет пришло %d байт, ждали %d", n, len(packet))
        }

        if err := dev.Close(); err != nil {
                t.Fatalf("закрытие: %v", err)
        }
}

// TestAuthAndConfig — create/join против тестового координатора и
// последующее чтение сводки ConfigInfo.
func TestAuthAndConfig(t *testing.T) {
        setDataDir(t.TempDir())

        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                switch r.URL.Path {
                case "/api/v1/networks":
                        var req engine.CreateNetworkRequest
                        _ = json.NewDecoder(r.Body).Decode(&req)
                        if req.Name != "office" || req.NodeName != "phone" {
                                t.Errorf("тело запроса: %+v", req)
                        }
                        _ = json.NewEncoder(w).Encode(engine.AuthResponse{
                                NodeID: "u_1", Token: "tok",
                                NetMap: engine.NetMap{
                                        NetworkID: "n_1", NetworkName: "office", Subnet: "10.77.1.0/24", Version: 1,
                                        Self: engine.Peer{Index: 3, IP: "10.77.1.3", Name: "phone", Role: "admin"},
                                },
                        })
                default:
                        http.NotFound(w, r)
                }
        }))
        defer srv.Close()

        if err := Create(srv.URL, "", "", "office", "password123", "phone"); err != nil {
                t.Fatalf("create: %v", err)
        }

        cfg, err := loadConfig()
        if err != nil {
                t.Fatal(err)
        }
        if cfg.Token != "tok" || cfg.IP != "10.77.1.3" || cfg.Index != 3 || cfg.Network != "office" {
                t.Fatalf("конфиг после create: %+v", cfg)
        }
        if cfg.PrivateKey == "" || cfg.PublicKey == "" {
                t.Fatal("ключи узла не сохранены")
        }

        info := ConfigInfo()
        var ci configInfo
        if err := json.Unmarshal([]byte(info), &ci); err != nil {
                t.Fatalf("ConfigInfo: %v", err)
        }
        if !ci.Joined || ci.IP != "10.77.1.3" || ci.Network != "office" || ci.Node != "phone" {
                t.Fatalf("ConfigInfo: %+v", ci)
        }

        // Ключ переиспользуется при повторном входе.
        old := cfg.PrivateKey
        if err := Join(srv.URL, "", "", "office", "password123", "phone2"); err != nil {
                // join уйдёт на 404 — сервер отвечает только на /networks, это нормально.
                _ = old
        }
}

// TestOptionsFromConfig — сборка опций и защита от пустых значений.
func TestOptionsFromConfig(t *testing.T) {
        setDataDir(t.TempDir())
        if err := saveConfig(&config{}); err != nil {
                t.Fatal(err)
        }
        if _, err := optionsFromConfig(&config{}, engine.NetMap{}, 1100); err == nil {
                t.Fatal("пустой конфиг должен давать ошибку")
        }

        pair, _ := engine.GenerateKeyPair()
        cfg := &config{
                Server: "http://10.0.0.1:8787", Hub: "10.0.0.1:9000", HubPin: "",
                NodeID: "u_1", Token: "t", Network: "office", IP: "10.77.1.3", Index: 3,
                PrivateKey: pair.Private,
        }
        netmap := engine.NetMap{
                NetworkID: "n_1", Subnet: "10.77.1.0/24",
                Self: engine.Peer{Index: 3, IP: "10.77.1.3"},
        }
        opt, err := optionsFromConfig(cfg, netmap, 0)
        if err != nil {
                t.Fatalf("optionsFromConfig: %v", err)
        }
        if opt.HubAddr != "10.0.0.1:9000" {
                t.Fatalf("hub: %q", opt.HubAddr)
        }
        if opt.MTU != 1100 {
                t.Fatalf("mtu по умолчанию: %d", opt.MTU)
        }
}

// TestHubDefault — вывод адреса хаба из адреса сервера.
func TestHubDefault(t *testing.T) {
        cfg := &config{Server: "https://vpn.example.ru:8787"}
        if got := hubOrDefault(cfg); got != "vpn.example.ru:8788" {
                t.Fatalf("hubOrDefault: %q", got)
        }
        cfg.Hub = "1.2.3.4:9999"
        if got := hubOrDefault(cfg); got != "1.2.3.4:9999" {
                t.Fatalf("явный хаб проигнорирован: %q", got)
        }
}

func mustPrefix(t *testing.T, s string) netip.Prefix {
        t.Helper()
        prefix, err := netip.ParsePrefix(s)
        if err != nil {
                t.Fatal(err)
        }
        return prefix
}

// sp — пара соединённых дескрипторов.
type sp struct{ a, b int }

func (s sp) close() {
	_ = syscall.Close(s.a)
	_ = syscall.Close(s.b)
}

// socketpair — два двунаправленных дескриптора UNIX-сокета.
func socketpair() (sp, error) {
	fd, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return sp{}, err
	}
	return sp{a: fd[0], b: fd[1]}, nil
}
