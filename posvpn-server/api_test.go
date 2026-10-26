package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"posvpn-engine"
)

func newTestServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	srv := httptest.NewServer(NewServer(store))
	t.Cleanup(srv.Close)

	return srv, store
}

func mustKey(t *testing.T) string {
	t.Helper()

	pair, err := engine.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return pair.Public
}

func doJSON(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data
}

// TestCreateJoinNetMap проверяет главный сценарий: создать сеть, войти вторым
// узлом, увидеть друг друга в карте сети.
func TestCreateJoinNetMap(t *testing.T) {
	srv, _ := newTestServer(t)

	status, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name:      "office",
		Password:  "secret123",
		NodeName:  "PC-1",
		PublicKey: mustKey(t),
	})
	if status != http.StatusCreated {
		t.Fatalf("создание сети: код %d, тело %s", status, data)
	}

	var created engine.AuthResponse
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if created.Token == "" {
		t.Fatal("не выдан токен")
	}
	if created.NetMap.Subnet != "10.77.1.0/24" {
		t.Fatalf("подсеть %q, ждали 10.77.1.0/24", created.NetMap.Subnet)
	}
	if created.NetMap.Self.IP != "10.77.1.1" {
		t.Fatalf("адрес первого узла %q, ждали 10.77.1.1", created.NetMap.Self.IP)
	}
	if created.NetMap.Self.Role != engine.RoleAdmin {
		t.Fatalf("роль первого узла %q, ждали admin", created.NetMap.Self.Role)
	}
	if len(created.NetMap.Peers) != 0 {
		t.Fatalf("в новой сети %d пиров, ждали 0", len(created.NetMap.Peers))
	}

	status, data = doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks/join", "", engine.JoinNetworkRequest{
		Name:      "office",
		Password:  "secret123",
		NodeName:  "PC-2",
		PublicKey: mustKey(t),
	})
	if status != http.StatusOK {
		t.Fatalf("вход в сеть: код %d, тело %s", status, data)
	}

	var joined engine.AuthResponse
	if err := json.Unmarshal(data, &joined); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if joined.NetMap.Self.IP != "10.77.1.2" {
		t.Fatalf("адрес второго узла %q, ждали 10.77.1.2", joined.NetMap.Self.IP)
	}
	if len(joined.NetMap.Peers) != 1 || joined.NetMap.Peers[0].Name != "PC-1" {
		t.Fatalf("второй узел не увидел первого: %+v", joined.NetMap.Peers)
	}

	status, data = doJSON(t, http.MethodGet, srv.URL+"/api/v1/netmap", created.Token, nil)
	if status != http.StatusOK {
		t.Fatalf("карта сети: код %d, тело %s", status, data)
	}
	var netmap engine.NetMap
	if err := json.Unmarshal(data, &netmap); err != nil {
		t.Fatalf("разбор карты: %v", err)
	}
	if len(netmap.Peers) != 1 || netmap.Peers[0].Name != "PC-2" {
		t.Fatalf("первый узел не увидел второго: %+v", netmap.Peers)
	}
}

func TestJoinRejectsWrongPassword(t *testing.T) {
	srv, _ := newTestServer(t)

	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name: "home", Password: "secret123", NodeName: "PC-1", PublicKey: mustKey(t),
	})
	if status != http.StatusCreated {
		t.Fatalf("создание сети: код %d", status)
	}

	status, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks/join", "", engine.JoinNetworkRequest{
		Name: "home", Password: "wrong-password", NodeName: "PC-2", PublicKey: mustKey(t),
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("неверный пароль: код %d, тело %s", status, data)
	}

	// Несуществующая сеть должна давать тот же ответ, иначе API работает
	// как перебор имён сетей.
	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks/join", "", engine.JoinNetworkRequest{
		Name: "no-such-network", Password: "secret123", NodeName: "PC-3", PublicKey: mustKey(t),
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("несуществующая сеть: код %d, ждали 401", status)
	}
}

func TestDuplicateNamesAndTokens(t *testing.T) {
	srv, _ := newTestServer(t)

	status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name: "dup", Password: "secret123", NodeName: "PC-1", PublicKey: mustKey(t),
	})
	if status != http.StatusCreated {
		t.Fatalf("создание сети: код %d", status)
	}

	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name: "DUP", Password: "secret123", NodeName: "PC-9", PublicKey: mustKey(t),
	})
	if status != http.StatusConflict {
		t.Fatalf("дубль имени сети: код %d, ждали 409", status)
	}

	status, _ = doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks/join", "", engine.JoinNetworkRequest{
		Name: "dup", Password: "secret123", NodeName: "PC-1", PublicKey: mustKey(t),
	})
	if status != http.StatusConflict {
		t.Fatalf("дубль имени узла: код %d, ждали 409", status)
	}

	status, _ = doJSON(t, http.MethodGet, srv.URL+"/api/v1/netmap", "not-a-token", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("чужой токен: код %d, ждали 401", status)
	}
}

func TestHeartbeatAndLeave(t *testing.T) {
	srv, _ := newTestServer(t)

	_, data := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name: "hb", Password: "secret123", NodeName: "PC-1", PublicKey: mustKey(t),
	})
	var admin engine.AuthResponse
	if err := json.Unmarshal(data, &admin); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/heartbeat", admin.Token,
		engine.HeartbeatRequest{Endpoint: "203.0.113.5:51820"})
	if status != http.StatusOK {
		t.Fatalf("heartbeat: код %d, тело %s", status, body)
	}
	var hb engine.HeartbeatResponse
	if err := json.Unmarshal(body, &hb); err != nil {
		t.Fatalf("разбор heartbeat: %v", err)
	}
	if hb.NetMapVersion == 0 {
		t.Fatal("версия карты не выросла после heartbeat с новым адресом")
	}

	// Последний администратор не может покинуть сеть.
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/leave", admin.Token, nil)
	if status != http.StatusConflict {
		t.Fatalf("выход последнего админа: код %d, тело %s", status, body)
	}

	// Второй узел выйти может.
	_, data = doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks/join", "", engine.JoinNetworkRequest{
		Name: "hb", Password: "secret123", NodeName: "PC-2", PublicKey: mustKey(t),
	})
	var user engine.AuthResponse
	if err := json.Unmarshal(data, &user); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	status, body = doJSON(t, http.MethodPost, srv.URL+"/api/v1/leave", user.Token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("выход обычного узла: код %d, тело %s", status, body)
	}
}

// TestStatePersists проверяет, что состояние переживает перезапуск процесса.
func TestStatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	srv := httptest.NewServer(NewServer(store))
	defer srv.Close()

	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/networks", "", engine.CreateNetworkRequest{
		Name: "persist", Password: "secret123", NodeName: "PC-1", PublicKey: mustKey(t),
	})
	if status != http.StatusCreated {
		t.Fatalf("создание сети: код %d, тело %s", status, body)
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatalf("повторное открытие: %v", err)
	}
	net, ok := reopened.NetworkByName("persist")
	if !ok {
		t.Fatal("сеть не сохранилась на диск")
	}
	if net.Subnet != "10.77.1.0/24" {
		t.Fatalf("подсеть после перезапуска %q", net.Subnet)
	}

	// Пароль должен проверяться и после перезапуска.
	if !engine.VerifyPassword(net.PasswordHash, "secret123") {
		t.Fatal("пароль не проверяется после перезапуска")
	}
}
