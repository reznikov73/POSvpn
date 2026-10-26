// Package engine содержит общие для клиента и сервера типы протокола POSvpn:
// модель сети, карту сети (netmap), адресацию и работу с секретами.
package engine

import "time"

// Роли участника сети.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// DefaultSubnetPrefix — первые два октета внутренней адресации POSvpn.
// Radmin VPN использует 26.0.0.0/8; мы берём 10.77.0.0/16, чтобы не пересечься
// с типовыми домашними и корпоративными подсетями.
const DefaultSubnetPrefix = "10.77"

// OnlineWindow — сколько узел считается онлайн после последнего heartbeat.
const OnlineWindow = 90 * time.Second

// Network — виртуальная сеть, аналог сети Radmin VPN.
// Сеть идентифицируется именем, доступ — по паролю.
type Network struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Subnet       string    `json:"subnet"`
	PasswordHash string    `json:"password_hash"`
	Closed       bool      `json:"closed"`
	CreatedAt    time.Time `json:"created_at"`
	// NextIndex — следующий числовой индекс узла. Индексы не переиспользуются,
	// пока существует сеть: по ним хаб адресует кадры.
	NextIndex uint32 `json:"next_index"`
}

// Node — узел (компьютер), состоящий в сети. Хранится только на сервере.
type Node struct {
	ID        string `json:"id"`
	NetworkID string `json:"network_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	IP        string `json:"ip"`
	// Index — числовой адрес узла внутри сети для заголовка кадра.
	// 0 зарезервирован под бродкаст, поэтому у реальных узлов индекс ≥ 1.
	Index     uint32    `json:"index"`
	Role      string    `json:"role"`
	TokenHash string    `json:"token_hash"`
	Endpoint  string    `json:"endpoint,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	Blocked   bool      `json:"blocked"`
}

// Peer — описание узла в карте сети, которую получает клиент.
type Peer struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Index     uint32 `json:"index"`
	PublicKey string `json:"public_key"`
	Role      string `json:"role"`
	Endpoint  string `json:"endpoint,omitempty"`
	Online    bool   `json:"online"`
}

// NetMap — снимок сети для конкретного узла. Клиент не договаривается с пирами
// напрямую, а исполняет выданную сервером карту (подход Headscale).
type NetMap struct {
	NetworkID   string `json:"network_id"`
	NetworkName string `json:"network_name"`
	Subnet      string `json:"subnet"`
	Version     int64  `json:"version"`
	Self        Peer   `json:"self"`
	Peers       []Peer `json:"peers"`
}

// CreateNetworkRequest — создание сети и первого узла в ней.
type CreateNetworkRequest struct {
	Name      string `json:"name"`
	Password  string `json:"password"`
	NodeName  string `json:"node_name"`
	PublicKey string `json:"public_key"`
}

// JoinNetworkRequest — вход в существующую сеть по имени и паролю.
type JoinNetworkRequest struct {
	Name      string `json:"name"`
	Password  string `json:"password"`
	NodeName  string `json:"node_name"`
	PublicKey string `json:"public_key"`
}

// AuthResponse — результат создания сети или входа в неё.
type AuthResponse struct {
	NodeID string `json:"node_id"`
	Token  string `json:"token"`
	NetMap NetMap `json:"netmap"`
}

// HeartbeatRequest — клиент сообщает свой внешний адрес и продлевает онлайн-статус.
type HeartbeatRequest struct {
	Endpoint string `json:"endpoint"`
}

// HeartbeatResponse — версия карты, чтобы клиент понял, что пора перезапросить netmap.
type HeartbeatResponse struct {
	NetMapVersion int64 `json:"netmap_version"`
}

// APIError — единый формат ошибки API.
type APIError struct {
	Error string `json:"error"`
}
