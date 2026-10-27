package main

import (
	"path/filepath"
	"testing"
	"time"

	"posvpn-engine"
)

// newTestStore открывает пустое состояние во временном каталоге.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestDeleteNetworkRemovesNodes удаляет сеть вместе с узлами, не задевая соседнюю.
func TestDeleteNetworkRemovesNodes(t *testing.T) {
	store := newTestStore(t)

	netA, _, err := store.CreateNetwork("n_a", "dom2", "hash", engine.Node{Name: "rez-Home", Role: "admin", TokenHash: "h_a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateNetwork("n_b", "rez", "hash", engine.Node{Name: "rez-Home", Role: "admin", TokenHash: "h_b"}); err != nil {
		t.Fatal(err)
	}

	removed, err := store.DeleteNetwork(netA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != "dom2" {
		t.Fatalf("удалена сеть %q, ожидалась dom2", removed.Name)
	}
	if _, ok := store.NetworkByID(netA.ID); ok {
		t.Fatal("сеть осталась в состоянии")
	}
	if nodes := store.NodesOf(netA.ID); len(nodes) != 0 {
		t.Fatalf("узлы удалённой сети остались: %d", len(nodes))
	}
	if _, ok := store.NodeByTokenHash("h_a"); ok {
		t.Fatal("токен узла удалённой сети продолжает работать")
	}

	netB, ok := store.NetworkByName("rez")
	if !ok {
		t.Fatal("соседняя сеть пропала")
	}
	if nodes := store.NodesOf(netB.ID); len(nodes) != 1 {
		t.Fatalf("узлы соседней сети: %d, ожидался 1", len(nodes))
	}
	if _, ok := store.NetworkByID("n_нет"); ok {
		t.Fatal("несуществующая сеть найдена")
	}

	// Состояние сохраняется на диск: после перезапуска сеть не воскресает.
	reopened, err := OpenStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.NetworkByID(netA.ID); ok {
		t.Fatal("после перезапуска удалённая сеть вернулась")
	}
}

// TestDeleteNetworkUnknown — повторное удаление даёт понятную ошибку.
func TestDeleteNetworkUnknown(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.DeleteNetwork("n_нет"); err == nil {
		t.Fatal("ожидалась ошибка для несуществующей сети")
	}
}

// TestSnapshotHasNetworkID — окну нужен ID, иначе кнопка удаления не работает.
func TestSnapshotHasNetworkID(t *testing.T) {
	store := newTestStore(t)
	created, _, err := store.CreateNetwork("n_a", "dom2", "hash", engine.Node{Name: "rez-Home", Role: "admin", TokenHash: "h_a"})
	if err != nil {
		t.Fatal(err)
	}
	views := store.Snapshot(time.Now())
	if len(views) != 1 {
		t.Fatalf("сетей в снимке: %d", len(views))
	}
	if views[0].ID != created.ID {
		t.Fatalf("ID сети в снимке %q, ожидался %q", views[0].ID, created.ID)
	}
}
