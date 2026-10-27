package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// useTestConfig направляет настройки клиента во временный файл.
func useTestConfig(t *testing.T) {
	t.Helper()
	previous := configOverride
	configOverride = filepath.Join(t.TempDir(), "client.json")
	t.Cleanup(func() { configOverride = previous })
}

// leaveServer поднимает заглушку координатора с заданным ответом на /leave.
func leaveServer(t *testing.T, status int, message string, gotToken *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/leave" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if gotToken != nil {
			*gotToken = r.Header.Get("Authorization")
		}
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestLeaveNetworkClearsConfig — успешный выход стирает сеть, адрес и токен,
// но оставляет ключ и имя узла для повторного входа.
func TestLeaveNetworkClearsConfig(t *testing.T) {
	useTestConfig(t)

	var token string
	server := leaveServer(t, http.StatusNoContent, "", &token)
	cfg := config{
		Server: server.URL, Token: "tok-1", Network: "rez", IP: "10.77.1.1",
		Index: 1, NodeID: "u_1", Node: "rez-Home", PrivateKey: "ключ",
	}
	if err := saveConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	if err := leaveNetwork(); err != nil {
		t.Fatalf("выход не выполнен: %v", err)
	}
	if token != "Bearer tok-1" {
		t.Fatalf("токен не отправлен: %q", token)
	}

	after, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if after.Token != "" || after.Network != "" || after.IP != "" || after.Index != 0 || after.NodeID != "" {
		t.Fatalf("настройки не очищены: %+v", after)
	}
	if after.Node != "rez-Home" || after.PrivateKey != "ключ" {
		t.Fatalf("имя или ключ узла потеряны: %+v", after)
	}
}

// TestLeaveNetworkKeepsConfigOnConflict — последний администратор получает 409,
// настройки при этом не трогаются.
func TestLeaveNetworkKeepsConfigOnConflict(t *testing.T) {
	useTestConfig(t)

	server := leaveServer(t, http.StatusConflict, "последний администратор не может покинуть сеть", nil)
	cfg := config{Server: server.URL, Token: "tok-2", Network: "rez", IP: "10.77.1.1"}
	if err := saveConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	if err := leaveNetwork(); err == nil {
		t.Fatal("ожидалась ошибка 409")
	}
	after, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if after.Token != "tok-2" || after.Network != "rez" {
		t.Fatalf("настройки очищены при отказе: %+v", after)
	}
}

// TestLeaveNetworkClearsDeletedNetwork — сеть уже удалена сервером: локальные
// настройки всё равно чистятся, иначе клиент зациклится на 401.
func TestLeaveNetworkClearsDeletedNetwork(t *testing.T) {
	useTestConfig(t)

	server := leaveServer(t, http.StatusUnauthorized, "сеть удалена или вход отозван: войдите в сеть заново", nil)
	cfg := config{Server: server.URL, Token: "tok-3", Network: "dom2", IP: "10.77.2.1"}
	if err := saveConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	if err := leaveNetwork(); err != nil {
		t.Fatalf("выход из удалённой сети: %v", err)
	}
	after, _ := loadConfig()
	if after.Token != "" || after.Network != "" {
		t.Fatalf("настройки удалённой сети остались: %+v", after)
	}
}

// TestLeaveNetworkWithoutNetwork — без сети выходить не из чего.
func TestLeaveNetworkWithoutNetwork(t *testing.T) {
	useTestConfig(t)
	if err := leaveNetwork(); err == nil {
		t.Fatal("ожидалась ошибка «сеть не выбрана»")
	}
}
