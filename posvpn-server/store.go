package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"posvpn-engine"
)

// state — всё состояние координатора. Для MVP этого достаточно: сети, узлы,
// счётчик подсетей и версия карты. При росте — SQLite с теми же сущностями.
type state struct {
	Networks   []engine.Network `json:"networks"`
	Nodes      []engine.Node    `json:"nodes"`
	NextSubnet int              `json:"next_subnet"`
	Version    int64            `json:"version"`
}

// Store — потокобезопасное хранилище состояния с атомарной записью на диск.
type Store struct {
	mu   sync.Mutex
	path string
	st   state
}

// OpenStore читает состояние из файла. Отсутствующий файл — не ошибка:
// координатор стартует с пустым состоянием.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, st: state{NextSubnet: 1}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение состояния: %w", err)
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		return nil, fmt.Errorf("разбор состояния %s: %w", path, err)
	}
	if s.st.NextSubnet < 1 {
		s.st.NextSubnet = 1
	}
	s.migrateIndexesLocked()
	return s, nil
}

// Empty сообщает, что в состоянии нет ни одной сети. В этом случае токены
// прежних клиентов неизвестны и любой вход получит 401.
func (s *Store) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Networks) == 0
}

// saveLocked пишет состояние во временный файл и переименовывает его,
// чтобы падение процесса не оставило недописанный JSON.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация состояния: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "posvpn-*.tmp")
	if err != nil {
		return fmt.Errorf("временный файл: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("запись состояния: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync состояния: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("закрытие состояния: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("замена состояния: %w", err)
	}
	return nil
}

// migrateIndexesLocked чинит состояние, созданное до появления числовых
// индексов: узлам без индекса выдаёт следующий свободный, а счётчик сети
// поднимает выше всех занятых.
func (s *Store) migrateIndexesLocked() {
	max := make(map[string]uint32)
	for _, n := range s.st.Nodes {
		if n.Index > max[n.NetworkID] {
			max[n.NetworkID] = n.Index
		}
	}
	byID := make(map[string]*engine.Network, len(s.st.Networks))
	for i := range s.st.Networks {
		net := &s.st.Networks[i]
		byID[net.ID] = net
		if net.NextIndex <= max[net.ID] {
			net.NextIndex = max[net.ID] + 1
		}
		if net.NextIndex == 0 {
			net.NextIndex = 1
		}
	}
	for i := range s.st.Nodes {
		node := &s.st.Nodes[i]
		if node.Index != 0 {
			continue
		}
		net, ok := byID[node.NetworkID]
		if !ok {
			continue
		}
		node.Index = net.NextIndex
		net.NextIndex++
	}
}

func (s *Store) bumpVersionLocked() {
	s.st.Version++
}

// CreateNetwork создаёт сеть и её первого узла (администратора).
// Подсеть и адрес узла выдаются здесь же, чтобы между чтением счётчика
// подсетей и записью состояния не было гонки.
func (s *Store) CreateNetwork(networkID, name, passwordHash string, node engine.Node) (engine.Network, engine.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.st.Networks {
		if equalFold(existing.Name, name) {
			return engine.Network{}, engine.Node{}, ErrNetworkExists
		}
	}
	if s.st.NextSubnet > 254 {
		return engine.Network{}, engine.Node{}, ErrSubnetExhausted
	}

	subnet, err := engine.NetworkSubnet(s.st.NextSubnet)
	if err != nil {
		return engine.Network{}, engine.Node{}, err
	}
	ip, err := engine.NextFreeIP(subnet, nil)
	if err != nil {
		return engine.Network{}, engine.Node{}, err
	}

	net := engine.Network{
		ID:           networkID,
		Name:         name,
		Subnet:       subnet,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	node.NetworkID = net.ID
	node.IP = ip
	node.Index = 1
	net.NextIndex = 2

	s.st.Networks = append(s.st.Networks, net)
	s.st.Nodes = append(s.st.Nodes, node)
	s.st.NextSubnet++
	s.bumpVersionLocked()

	if err := s.saveLocked(); err != nil {
		return engine.Network{}, engine.Node{}, err
	}
	return net, node, nil
}

// NetworkByName ищет сеть по имени без учёта регистра.
func (s *Store) NetworkByName(name string) (engine.Network, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, net := range s.st.Networks {
		if equalFold(net.Name, name) {
			return net, true
		}
	}
	return engine.Network{}, false
}

// NetworkByID возвращает сеть по идентификатору.
func (s *Store) NetworkByID(id string) (engine.Network, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, net := range s.st.Networks {
		if net.ID == id {
			return net, true
		}
	}
	return engine.Network{}, false
}

// NodesOf возвращает копию списка узлов сети.
func (s *Store) NodesOf(networkID string) []engine.Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []engine.Node
	for _, node := range s.st.Nodes {
		if node.NetworkID == networkID {
			out = append(out, node)
		}
	}
	return out
}

// AddNode добавляет узел в существующую сеть: проверяет имя, считает лимит
// участников и выдаёт первый свободный адрес подсети.
func (s *Store) AddNode(node engine.Node) (engine.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var net *engine.Network
	for i := range s.st.Networks {
		if s.st.Networks[i].ID == node.NetworkID {
			net = &s.st.Networks[i]
			break
		}
	}
	if net == nil {
		return engine.Node{}, ErrNetworkNotFound
	}

	used := make(map[string]struct{})
	count := 0
	nameIdx := -1
	for i := range s.st.Nodes {
		existing := s.st.Nodes[i]
		if existing.NetworkID != node.NetworkID {
			continue
		}
		count++
		used[existing.IP] = struct{}{}
		if equalFold(existing.Name, node.Name) {
			nameIdx = i
		}
	}

	// Тот же узел возвращается после перезапуска или падения: имя занято им же.
	// Занять имя вышедшего из сети узла тоже можно — иначе после аварийного
	// закрытия клиента войти под своим именем больше нельзя.
	if nameIdx >= 0 {
		old := s.st.Nodes[nameIdx]
		sameKey := old.PublicKey != "" && old.PublicKey == node.PublicKey
		offline := time.Since(old.LastSeen) >= engine.OnlineWindow
		if !sameKey && !offline {
			return engine.Node{}, ErrNodeNameTaken
		}

		node.IP = old.IP
		node.Index = old.Index
		if node.Role == "" {
			node.Role = old.Role
		}
		s.st.Nodes[nameIdx] = node
		s.bumpVersionLocked()
		if err := s.saveLocked(); err != nil {
			return engine.Node{}, err
		}
		return node, nil
	}

	if count >= maxNodesPerNetwork {
		return engine.Node{}, ErrNetworkFull
	}

	ip, err := engine.NextFreeIP(net.Subnet, used)
	if err != nil {
		return engine.Node{}, err
	}
	node.IP = ip
	node.Index = net.NextIndex
	if node.Index == 0 {
		node.Index = 1
	}
	net.NextIndex = node.Index + 1

	s.st.Nodes = append(s.st.Nodes, node)
	s.bumpVersionLocked()

	if err := s.saveLocked(); err != nil {
		return engine.Node{}, err
	}
	return node, nil
}

// NodeByTokenHash ищет узел по хешу токена — единственный способ аутентификации клиента.
func (s *Store) NodeByTokenHash(hash string) (engine.Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, node := range s.st.Nodes {
		if node.TokenHash == hash {
			return node, true
		}
	}
	return engine.Node{}, false
}

// TouchNode обновляет внешний адрес и время последней активности узла.
func (s *Store) TouchNode(nodeID, endpoint string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.st.Nodes {
		if s.st.Nodes[i].ID != nodeID {
			continue
		}
		s.st.Nodes[i].LastSeen = at
		if endpoint != "" && s.st.Nodes[i].Endpoint != endpoint {
			s.st.Nodes[i].Endpoint = endpoint
			s.bumpVersionLocked()
		}
		return s.saveLocked()
	}
	return ErrNodeNotFound
}

// RemoveNode удаляет узел из сети.
func (s *Store) RemoveNode(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, node := range s.st.Nodes {
		if node.ID != nodeID {
			continue
		}
		s.st.Nodes = append(s.st.Nodes[:i], s.st.Nodes[i+1:]...)
		s.bumpVersionLocked()
		return s.saveLocked()
	}
	return ErrNodeNotFound
}

// NetMapFor собирает карту сети для конкретного узла.
func (s *Store) NetMapFor(node engine.Node) (engine.NetMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var net engine.Network
	found := false
	for _, candidate := range s.st.Networks {
		if candidate.ID == node.NetworkID {
			net = candidate
			found = true
			break
		}
	}
	if !found {
		return engine.NetMap{}, ErrNetworkNotFound
	}

	now := time.Now()
	netmap := engine.NetMap{
		NetworkID:   net.ID,
		NetworkName: net.Name,
		Subnet:      net.Subnet,
		Version:     s.st.Version,
		Self: engine.Peer{
			NodeID:    node.ID,
			Name:      node.Name,
			IP:        node.IP,
			Index:     node.Index,
			PublicKey: node.PublicKey,
			Role:      node.Role,
			Endpoint:  node.Endpoint,
			Online:    true,
		},
	}

	for _, peer := range s.st.Nodes {
		if peer.NetworkID != net.ID || peer.ID == node.ID || peer.Blocked {
			continue
		}
		netmap.Peers = append(netmap.Peers, engine.Peer{
			NodeID:    peer.ID,
			Name:      peer.Name,
			IP:        peer.IP,
			Index:     peer.Index,
			PublicKey: peer.PublicKey,
			Role:      peer.Role,
			Endpoint:  peer.Endpoint,
			Online:    now.Sub(peer.LastSeen) < engine.OnlineWindow,
		})
	}
	return netmap, nil
}

// NetworkView — снимок сети для окна сервера.
type NetworkView struct {
	Name   string     `json:"name"`
	Subnet string     `json:"subnet"`
	Closed bool       `json:"closed"`
	Nodes  []NodeView `json:"nodes"`
}

// NodeView — снимок узла для окна сервера.
type NodeView struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Index   uint32 `json:"index"`
	Role    string `json:"role"`
	Online  bool   `json:"online"`
	Blocked bool   `json:"blocked"`
}

// Snapshot отдаёт сети и узлы для окна сервера.
func (s *Store) Snapshot(now time.Time) []NetworkView {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]NetworkView, 0, len(s.st.Networks))
	for _, net := range s.st.Networks {
		view := NetworkView{Name: net.Name, Subnet: net.Subnet, Closed: net.Closed}
		for _, node := range s.st.Nodes {
			if node.NetworkID != net.ID {
				continue
			}
			view.Nodes = append(view.Nodes, NodeView{
				Name:    node.Name,
				IP:      node.IP,
				Index:   node.Index,
				Role:    node.Role,
				Online:  now.Sub(node.LastSeen) < engine.OnlineWindow,
				Blocked: node.Blocked,
			})
		}
		sort.Slice(view.Nodes, func(i, j int) bool { return view.Nodes[i].Index < view.Nodes[j].Index })
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Version возвращает текущую версию карты сети.
func (s *Store) Version() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.st.Version
}

// CountAdmins считает администраторов сети — нужен для инварианта
// «в сети всегда остаётся хотя бы один администратор».
func (s *Store) CountAdmins(networkID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, node := range s.st.Nodes {
		if node.NetworkID == networkID && node.Role == engine.RoleAdmin {
			count++
		}
	}
	return count
}

// UsedIPs возвращает занятые адреса сети.
func (s *Store) UsedIPs(networkID string) map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	used := make(map[string]struct{})
	for _, node := range s.st.Nodes {
		if node.NetworkID == networkID {
			used[node.IP] = struct{}{}
		}
	}
	return used
}
