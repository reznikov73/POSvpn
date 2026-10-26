package main

import (
	"os"
	"path/filepath"
	"testing"

	"posvpn-engine"
)

func TestNodeIndexesAssignedAndNotReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}

	net, first, err := store.CreateNetwork("n_1", "office", "hash", engine.Node{
		ID: "u_1", Name: "PC-1", TokenHash: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Index != 1 {
		t.Fatalf("первый узел получил индекс %d, ждали 1", first.Index)
	}

	second, err := store.AddNode(engine.Node{
		ID: "u_2", NetworkID: net.ID, Name: "PC-2", TokenHash: "t2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Index != 2 {
		t.Fatalf("второй узел получил индекс %d, ждали 2", second.Index)
	}

	if err := store.RemoveNode(first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := store.AddNode(engine.Node{
		ID: "u_3", NetworkID: net.ID, Name: "PC-3", TokenHash: "t3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if third.Index != 3 {
		t.Fatalf("индекс переиспользован: получили %d, ждали 3", third.Index)
	}

	// Индексы должны пережить перезапуск сервера.
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored engine.Node
	for _, node := range reopened.NodesOf(net.ID) {
		if node.ID == third.ID {
			stored = node
		}
	}
	if stored.Index != 3 {
		t.Fatalf("после перезапуска индекс %d, ждали 3", stored.Index)
	}

	netmap, err := reopened.NetMapFor(stored)
	if err != nil {
		t.Fatal(err)
	}
	if netmap.Self.Index != 3 {
		t.Fatalf("в карте сети индекс %d", netmap.Self.Index)
	}
	if len(netmap.Peers) != 1 || netmap.Peers[0].Index != 2 {
		t.Fatalf("пиры в карте: %+v", netmap.Peers)
	}
}

func TestOpenStoreMigratesStateWithoutIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	old := `{
  "networks": [{"id": "n_1", "name": "office", "subnet": "10.77.1.0/24"}],
  "nodes": [
    {"id": "u_1", "network_id": "n_1", "name": "PC-1", "ip": "10.77.1.1"},
    {"id": "u_2", "network_id": "n_1", "name": "PC-2", "ip": "10.77.1.2"}
  ],
  "next_subnet": 2,
  "version": 1
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes := store.NodesOf("n_1")
	if len(nodes) != 2 {
		t.Fatalf("узлов %d", len(nodes))
	}
	for i, node := range nodes {
		if node.Index != uint32(i+1) {
			t.Fatalf("узел %s получил индекс %d, ждали %d", node.ID, node.Index, i+1)
		}
	}

	next, err := store.AddNode(engine.Node{
		ID: "u_3", NetworkID: "n_1", Name: "PC-3", TokenHash: "t3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Index != 3 {
		t.Fatalf("новый узел получил индекс %d, ждали 3", next.Index)
	}
}
