package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// Identity — то, что хаб знает об узле после аутентификации.
type Identity struct {
	NodeID    string
	NetworkID string
	Index     uint32
}

// AuthFunc проверяет токен узла и возвращает его идентичность.
// Ошибка означает отказ в подключении.
type AuthFunc func(token string) (Identity, error)

// HubConfig — настройки QUIC-хаба.
type HubConfig struct {
	// Addr — адрес прослушивания, например ":8788" или "0.0.0.0:8788".
	Addr string
	// TLSConfig — обязателен; удобнее собрать через ServerTLSConfig.
	TLSConfig *tls.Config
	// Auth — проверка токена; обязательна.
	Auth AuthFunc
	// Logger — по умолчанию log.Default().
	Logger *log.Logger
}

// Stats — счётчики хаба для журнала и тестов.
type Stats struct {
	Received   uint64
	Forwarded  uint64
	Broadcast  uint64
	Dropped    uint64
	SendErrors uint64
}

// Hub — QUIC-релей: принимает кадры и пересылает их по индексу узла,
// не читая содержимое.
type Hub struct {
	cfg HubConfig
	ln  *quic.Listener

	mu   sync.RWMutex
	nets map[string]map[uint32]*hubSession

	received   atomic.Uint64
	forwarded  atomic.Uint64
	broadcast  atomic.Uint64
	dropped    atomic.Uint64
	sendErrors atomic.Uint64
}

type hubSession struct {
	id     Identity
	conn   *quic.Conn
	sendMu sync.Mutex
	hub    *Hub
}

// Listen поднимает хаб и начинает принимать подключения.
func Listen(cfg HubConfig) (*Hub, error) {
	if cfg.Auth == nil {
		return nil, errors.New("хаб: не задана проверка токена")
	}
	if cfg.TLSConfig == nil {
		return nil, errors.New("хаб: не задан TLS")
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}

	ln, err := quic.ListenAddr(cfg.Addr, cfg.TLSConfig, &quic.Config{
		EnableDatagrams:      true,
		MaxIdleTimeout:       90 * time.Second,
		KeepAlivePeriod:      25 * time.Second,
		HandshakeIdleTimeout: 10 * time.Second,
		MaxIncomingStreams:   4,
	})
	if err != nil {
		return nil, fmt.Errorf("хаб: слушатель %s: %w", cfg.Addr, err)
	}

	h := &Hub{cfg: cfg, ln: ln, nets: make(map[string]map[uint32]*hubSession)}
	go h.acceptLoop()
	return h, nil
}

// Addr возвращает фактический адрес прослушивания.
func (h *Hub) Addr() string { return h.ln.Addr().String() }

// Stats возвращает снимок счётчиков.
func (h *Hub) Stats() Stats {
	return Stats{
		Received:   h.received.Load(),
		Forwarded:  h.forwarded.Load(),
		Broadcast:  h.broadcast.Load(),
		Dropped:    h.dropped.Load(),
		SendErrors: h.sendErrors.Load(),
	}
}

// Close останавливает слушатель и закрывает все сессии.
func (h *Hub) Close() error {
	err := h.ln.Close()

	h.mu.Lock()
	var sessions []*hubSession
	for _, peers := range h.nets {
		for _, s := range peers {
			sessions = append(sessions, s)
		}
	}
	h.nets = make(map[string]map[uint32]*hubSession)
	h.mu.Unlock()

	for _, s := range sessions {
		_ = s.conn.CloseWithError(0, "хаб остановлен")
	}
	return err
}

func (h *Hub) acceptLoop() {
	for {
		conn, err := h.ln.Accept(context.Background())
		if err != nil {
			return
		}
		go h.serve(conn)
	}
}

func (h *Hub) serve(conn *quic.Conn) {
	remote := conn.RemoteAddr().String()
	logger := h.cfg.Logger
	defer conn.CloseWithError(0, "")

	ctx, cancel := context.WithTimeout(conn.Context(), 15*time.Second)
	defer cancel()

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		logger.Printf("хаб: %s не открыл поток рукопожатия: %v", remote, err)
		return
	}
	_ = stream.SetDeadline(time.Now().Add(10 * time.Second))

	var hello helloRequest
	if err := json.NewDecoder(io.LimitReader(stream, 4096)).Decode(&hello); err != nil {
		h.reject(conn, stream, "некорректное рукопожатие")
		logger.Printf("хаб: %s прислал некорректное рукопожатие: %v", remote, err)
		return
	}
	if hello.Version != handshakeVersion {
		h.reject(conn, stream, "версия рукопожатия не поддерживается")
		logger.Printf("хаб: %s просит версию рукопожатия %d", remote, hello.Version)
		return
	}
	if hello.Token == "" {
		h.reject(conn, stream, "пустой токен")
		return
	}

	ident, err := h.cfg.Auth(hello.Token)
	if err != nil {
		h.reject(conn, stream, err.Error())
		logger.Printf("хаб: %s отклонён: %v", remote, err)
		return
	}
	if ident.NetworkID == "" || ident.Index == 0 {
		h.reject(conn, stream, "узел не привязан к сети")
		logger.Printf("хаб: узел %s без сети или индекса", ident.NodeID)
		return
	}

	sess := &hubSession{id: ident, conn: conn, hub: h}
	h.register(sess)
	defer h.unregister(sess)

	if err := writeHello(stream, helloResponse{OK: true, NodeID: ident.NodeID}); err != nil {
		logger.Printf("хаб: %s не получил подтверждение: %v", remote, err)
		return
	}
	_ = stream.Close()

	logger.Printf("хаб: узел %s из сети %s (индекс %d) подключён с %s",
		ident.NodeID, ident.NetworkID, ident.Index, remote)

	for {
		frame, err := conn.ReceiveDatagram(conn.Context())
		if err != nil {
			return
		}
		h.route(sess, frame)
	}
}

func writeHello(w io.Writer, resp helloResponse) error {
	return json.NewEncoder(w).Encode(resp)
}

// reject сообщает клиенту причину отказа и даёт ему прочитать ответ,
// прежде чем закрыть соединение. Без этой паузы клиент видит только
// «соединение закрыто» и не знает, что именно не так.
func (h *Hub) reject(conn *quic.Conn, stream *quic.Stream, reason string) {
	_ = writeHello(stream, helloResponse{Error: reason})
	_ = stream.Close()
	select {
	case <-conn.Context().Done():
	case <-time.After(2 * time.Second):
	}
}

func (h *Hub) register(sess *hubSession) {
	h.mu.Lock()
	peers := h.nets[sess.id.NetworkID]
	if peers == nil {
		peers = make(map[uint32]*hubSession)
		h.nets[sess.id.NetworkID] = peers
	}
	old := peers[sess.id.Index]
	peers[sess.id.Index] = sess
	h.mu.Unlock()

	if old != nil && old != sess {
		// Тот же узел переподключился: старую сессию закрываем, иначе кадры
		// будут уходить в мёртвое соединение.
		_ = old.conn.CloseWithError(1, "узел переподключился")
	}
}

func (h *Hub) unregister(sess *hubSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	peers := h.nets[sess.id.NetworkID]
	if peers == nil {
		return
	}
	if peers[sess.id.Index] != sess {
		// Уже заменён новой сессией — ничего не удаляем.
		return
	}
	delete(peers, sess.id.Index)
	if len(peers) == 0 {
		delete(h.nets, sess.id.NetworkID)
	}
}

func (h *Hub) lookup(networkID string, index uint32) *hubSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.nets[networkID][index]
}

func (h *Hub) snapshot(networkID string) []*hubSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	peers := h.nets[networkID]
	out := make([]*hubSession, 0, len(peers))
	for _, s := range peers {
		out = append(out, s)
	}
	return out
}

// route пересылает кадр адресату в пределах сети отправителя.
func (h *Hub) route(from *hubSession, frame []byte) {
	h.received.Add(1)

	if len(frame) > MaxFrameSize {
		h.dropped.Add(1)
		return
	}
	hdr, err := ParseHeader(frame)
	if err != nil {
		h.dropped.Add(1)
		return
	}

	// Полю src не доверяем: ставим индекс того, кто реально прислал кадр.
	// Клиент тоже пишет свой индекс, поэтому для честного клиента это no-op,
	// а для подделки — ломает тег AEAD у получателя.
	if hdr.Src != from.id.Index {
		SetSrc(frame, from.id.Index)
		hdr.Src = from.id.Index
	}

	if hdr.Dst == BroadcastIndex {
		for _, peer := range h.snapshot(from.id.NetworkID) {
			if peer == from {
				continue
			}
			peer.send(frame)
		}
		h.broadcast.Add(1)
		return
	}

	target := h.lookup(from.id.NetworkID, hdr.Dst)
	if target == nil {
		h.dropped.Add(1)
		return
	}
	target.send(frame)
	h.forwarded.Add(1)
}

// DropNetwork закрывает сеансы всех узлов сети. Нужно при удалении сети:
// иначе клиенты продолжали бы слать кадры в никуда и считали себя в сети.
func (h *Hub) DropNetwork(networkID string) int {
	hs := h.snapshot(networkID)
	for _, sess := range hs {
		h.unregister(sess)
		_ = sess.conn.CloseWithError(0x10, "сеть удалена")
	}
	return len(hs)
}

func (s *hubSession) send(frame []byte) {
	s.sendMu.Lock()
	err := s.conn.SendDatagram(frame)
	s.sendMu.Unlock()
	if err != nil {
		s.hub.sendErrors.Add(1)
	}
}
