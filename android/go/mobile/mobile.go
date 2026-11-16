// Package mobile — фасад движка POSvpn для Android-приложения.
//
// Пакет собирается через gomobile bind в AAR и вызывается из Kotlin/Java.
// Типы экспортируемых функций ограничены возможностями gomobile: строки,
// числа, error и интерфейсы. Сложные структуры передаются как JSON-строки.
//
// Логика туннеля перенесена из posvpn-client (tunnel.go) с сохранением
// формата кадров, ключей пар и поведения при переподключении.
package mobile

import (
        "context"
        "sync"
)

// Events — коллбэки из Go в Java. Реализация на стороне приложения.
// Вызовы могут приходить из фоновых горутин: Java-сторона обязана сама
// переключиться на главный поток.
type Events interface {
        // OnStateChanged вызывается при смене состояния туннеля.
        // JSON: {"state":"up|wait|down|error","error":"..."}
        OnStateChanged(stateJson string)
        // OnNetmapUpdated вызывается, когда карта сети изменилась.
        // JSON: {"network":"...","self_ip":"...","peers":[...]}
        OnNetmapUpdated(netmapJson string)
}

// manager — единое состояние библиотеки: настройки, сессия, события.
type manager struct {
        mu      sync.Mutex
        cfg     *config
        session *tunnelSession
        events  Events
}

var mgr = &manager{}

// SetEvents подключает коллбэки UI. Можно вызвать один раз при старте
// приложения; nil отключает уведомления.
func SetEvents(e Events) {
        mgr.mu.Lock()
        defer mgr.mu.Unlock()
        mgr.events = e
}

func (m *manager) emitState(s string, errMsg string) {
        m.mu.Lock()
        e := m.events
        m.mu.Unlock()
        if e == nil {
                return
        }
        e.OnStateChanged(mustJSON(stateEvent{State: s, Error: errMsg}))
}

func (m *manager) emitNetmap() {
        m.mu.Lock()
        e := m.events
        m.mu.Unlock()
        if e == nil {
                return
        }
        e.OnNetmapUpdated(Status())
}

// SetDataDir задаёт каталог, где хранится client.json. Android передаёт
// getFilesDir(). Вызывать до остальных функций.
func SetDataDir(dir string) {
        setDataDir(dir)
}

// HasConfig сообщает, входил ли этот узел в сеть (create/join выполнены).
func HasConfig() bool {
        cfg, err := loadConfig()
        return err == nil && cfg != nil && cfg.Token != "" && cfg.NodeID != ""
}

// ConfigInfo возвращает краткую сводку настроек как JSON:
// {"server":"...","hub":"...","hub_pin":"...","network":"...","node":"...",
//  "ip":"...","index":N,"joined":true}
// Сервису VPN отсюда нужен ip — для Builder.addAddress.
func ConfigInfo() string {
        cfg, err := loadConfig()
        if err != nil || cfg == nil {
                return `{"joined":false}`
        }
        return mustJSON(configInfo{
                Server:   cfg.Server,
                Hub:      hubOrDefault(cfg),
                HubPin:   cfg.HubPin,
                Network:  cfg.Network,
                Node:     cfg.Node,
                IP:       cfg.IP,
                Index:    cfg.Index,
                Joined:   cfg.Token != "" && cfg.NodeID != "",
        })
}

// SetServer сохраняет адрес координатора, хаба и отпечаток сертификата
// без входа в сеть. Пустой hub означает «вывести из адреса сервера».
func SetServer(server, hub, hubPin string) error {
        cfg, err := loadConfig()
        if err != nil {
                return err
        }
        cfg.Server = normalizeServer(server)
        if hub != "" {
                cfg.Hub = hub
        }
        if hubPin != "" {
                cfg.HubPin = hubPin
        }
        return saveConfig(cfg)
}

// Create создаёт новую сеть: этот узел становится её администратором.
// Блокирующий вызов: Java обязан звать из фонового потока.
func Create(server, hub, hubPin, network, password, nodeName string) error {
        return auth(server, hub, hubPin, network, password, nodeName, true)
}

// Join входит в существующую сеть по имени и паролю.
// Блокирующий вызов: Java обязан звать из фонового потока.
func Join(server, hub, hubPin, network, password, nodeName string) error {
        return auth(server, hub, hubPin, network, password, nodeName, false)
}

// Leave выходит из сети: сообщает серверу и стирает сетевые настройки.
// Ключ узла сохраняется — повторный вход вернёт прежнюю идентичность.
func Leave() error {
        Stop()
        return leaveNetwork()
}

// Start поднимает туннель на файловом дескрипторе, полученном от
// VpnService.establish(). Неблокирующий: запускает сессию в фоне и
// возвращается. Ошибки приходят через Events.OnStateChanged и Status().
func Start(fd, mtu int) error {
        cfg, err := loadConfig()
        if err != nil {
                return err
        }
        if cfg == nil || cfg.Token == "" || cfg.NodeID == "" {
                return errNotJoined
        }

        mgr.mu.Lock()
        defer mgr.mu.Unlock()
        if mgr.session != nil {
                if _, _, last := mgr.session.status(); last == "" {
                        return nil // уже запущен
                }
                mgr.session.Stop()
                mgr.session = nil
        }

        netmap, err := fetchNetMap(cfg)
        if err != nil {
                return err
        }
        opt, err := optionsFromConfig(cfg, netmap, mtu)
        if err != nil {
                return err
        }

        sess, err := startTunnelSession(opt, fd, mgr)
        if err != nil {
                return err
        }
        mgr.session = sess
        return nil
}

// Stop останавливает туннель, если он запущен.
func Stop() {
        mgr.mu.Lock()
        sess := mgr.session
        mgr.session = nil
        mgr.mu.Unlock()
        if sess != nil {
                sess.Stop()
        }
        mgr.emitState("down", "")
}

// Status возвращает полный статус JSON:
// {"running":b,"connected":b,"state":"...","error":"...","network":"...",
//  "self_ip":"...","self_index":N,"peers":[{...}]}
func Status() string {
        mgr.mu.Lock()
        sess := mgr.session
        cfg, cfgErr := loadConfig()
        mgr.mu.Unlock()

        if cfgErr != nil || cfg == nil {
                return mustJSON(statusSnapshot{State: "down", Error: "нет настроек"})
        }

        snap := statusSnapshot{
                Network:   cfg.Network,
                Node:      cfg.Node,
                SelfIP:    cfg.IP,
                SelfIndex: cfg.Index,
                Started:   false,
                Connected: false,
                State:     "down",
        }

        if sess != nil {
                running, connected, lastErr := sess.status()
                snap.Running = running
                snap.Connected = connected
                snap.Started = running
                snap.State = "wait"
                if connected {
                        snap.State = "up"
                } else if lastErr != "" {
                        snap.State = "error"
                        snap.Error = lastErr
                }
                snap.Peers = sess.peersSnapshot()
        }
        return mustJSON(snap)
}

// StopOnExit служебная: гарантирует остановку при выгрузке библиотеки.
// В документацию не входит.
func StopOnExit() { Stop() }

var _ = context.Background // контекст используется в session.go
