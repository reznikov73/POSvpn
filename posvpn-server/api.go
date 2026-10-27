package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"posvpn-engine"
)

const (
	// maxNodesPerNetwork — осознанное ограничение MVP. У Radmin VPN 150 узлов,
	// но у нас весь трафик идёт через один хаб, поэтому 25 — разумный потолок.
	maxNodesPerNetwork = 25
	minPasswordLen     = 8
	maxNameLen         = 40
)

// Server — HTTP API координатора.
type Server struct {
	store *Store
	now   func() time.Time
}

// NewServer собирает маршруты API.
func NewServer(store *Store) http.Handler {
	s := &Server{store: store, now: time.Now}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/networks", s.handleCreateNetwork)
	mux.HandleFunc("POST /api/v1/networks/join", s.handleJoinNetwork)
	mux.HandleFunc("GET /api/v1/netmap", s.withAuth(s.handleNetMap))
	mux.HandleFunc("POST /api/v1/heartbeat", s.withAuth(s.handleHeartbeat))
	mux.HandleFunc("POST /api/v1/leave", s.withAuth(s.handleLeave))
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	return logRequests(mux)
}

type authedHandler func(w http.ResponseWriter, r *http.Request, node engine.Node)

func (s *Server) withAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			writeError(w, http.StatusUnauthorized, "нужен заголовок Authorization: Bearer <токен>")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "пустой токен")
			return
		}

		node, ok := s.store.NodeByTokenHash(engine.HashToken(token))
		if !ok {
			writeError(w, http.StatusUnauthorized, "неизвестный токен: войдите в сеть заново")
			return
		}
		if node.Blocked {
			writeError(w, http.StatusForbidden, "узел заблокирован администратором сети")
			return
		}
		next(w, r, node)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req engine.CreateNetworkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NodeName = strings.TrimSpace(req.NodeName)
	if err := validateName("имя сети", req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateName("имя компьютера", req.NodeName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "пароль сети короче 8 символов")
		return
	}
	if _, err := engine.ParsePublicKey(req.PublicKey); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	passwordHash, err := engine.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось захешировать пароль")
		return
	}
	networkID, err := engine.NewID("n_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выпустить id сети")
		return
	}
	nodeID, err := engine.NewID("u_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выпустить id узла")
		return
	}
	token, err := engine.NewToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выпустить токен")
		return
	}

	node := engine.Node{
		ID:        nodeID,
		Name:      req.NodeName,
		PublicKey: req.PublicKey,
		Role:      engine.RoleAdmin,
		TokenHash: engine.HashToken(token),
		LastSeen:  s.now(),
	}

	net, node, err := s.store.CreateNetwork(networkID, req.Name, passwordHash, node)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	netmap, err := s.store.NetMapFor(node)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось собрать карту сети")
		return
	}

	log.Printf("создана сеть %q (%s), первый узел %q, адрес %s", net.Name, net.ID, node.Name, node.IP)
	writeJSON(w, http.StatusCreated, engine.AuthResponse{NodeID: node.ID, Token: token, NetMap: netmap})
}

func (s *Server) handleJoinNetwork(w http.ResponseWriter, r *http.Request) {
	var req engine.JoinNetworkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NodeName = strings.TrimSpace(req.NodeName)
	if err := validateName("имя компьютера", req.NodeName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := engine.ParsePublicKey(req.PublicKey); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Одинаковый ответ на «нет такой сети» и «неверный пароль»:
	// иначе API превращается в перебор имён сетей.
	net, ok := s.store.NetworkByName(req.Name)
	if !ok || !engine.VerifyPassword(net.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "неверное имя сети или пароль")
		return
	}
	if net.Closed {
		writeError(w, http.StatusForbidden, "сеть закрыта для новых участников")
		return
	}
	if len(s.store.NodesOf(net.ID)) >= maxNodesPerNetwork {
		writeError(w, http.StatusConflict, "в сети достигнут предел участников")
		return
	}

	nodeID, err := engine.NewID("u_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выпустить id узла")
		return
	}
	token, err := engine.NewToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выпустить токен")
		return
	}

	node := engine.Node{
		ID:        nodeID,
		NetworkID: net.ID,
		Name:      req.NodeName,
		PublicKey: req.PublicKey,
		Role:      engine.RoleUser,
		TokenHash: engine.HashToken(token),
		LastSeen:  s.now(),
	}

	node, err = s.store.AddNode(node)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	netmap, err := s.store.NetMapFor(node)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось собрать карту сети")
		return
	}

	log.Printf("узел %q вошёл в сеть %q, адрес %s", node.Name, net.Name, node.IP)
	writeJSON(w, http.StatusOK, engine.AuthResponse{NodeID: node.ID, Token: token, NetMap: netmap})
}

func (s *Server) handleNetMap(w http.ResponseWriter, r *http.Request, node engine.Node) {
	netmap, err := s.store.NetMapFor(node)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, netmap)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, node engine.Node) {
	var req engine.HeartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.TouchNode(node.ID, strings.TrimSpace(req.Endpoint), s.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, engine.HeartbeatResponse{NetMapVersion: s.store.Version()})
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request, node engine.Node) {
	if node.Role == engine.RoleAdmin && s.store.CountAdmins(node.NetworkID) <= 1 {
		writeError(w, http.StatusConflict,
			"последний администратор не может покинуть сеть: сначала назначьте другого")
		return
	}
	if err := s.store.RemoveNode(node.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	log.Printf("узел %q (%s) покинул сеть", node.Name, node.ID)
	w.WriteHeader(http.StatusNoContent)
}

func validateName(field, value string) error {
	if value == "" {
		return errors.New(field + " не может быть пустым")
	}
	if len([]rune(value)) > maxNameLen {
		return errors.New(field + " длиннее 40 символов")
	}
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return errors.New("тело запроса не является корректным JSON")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("ответ не отправлен: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, engine.APIError{Error: message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNetworkExists), errors.Is(err, ErrNodeNameTaken), errors.Is(err, ErrAddressTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNetworkNotFound), errors.Is(err, ErrNodeNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSubnetExhausted):
		writeError(w, http.StatusInsufficientStorage, err.Error())
	default:
		log.Printf("внутренняя ошибка: %v", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка сервера")
	}
}

// logRequests пишет метод, путь и код ответа — минимальная наблюдаемость без зависимостей.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s → %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
