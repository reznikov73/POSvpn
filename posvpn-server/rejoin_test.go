package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"posvpn-engine"
)

func TestRejoinReclaimsName(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Прежний узел давно офлайн — его имя и адрес можно занять заново.
	net, first, err := store.CreateNetwork("n_1", "office", "hash", engine.Node{
		ID: "u_1", Name: "PC", PublicKey: "key-old", TokenHash: "t1",
		LastSeen: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	rejoined, err := store.AddNode(engine.Node{
		ID: "u_2", NetworkID: net.ID, Name: "PC", PublicKey: "key-new", TokenHash: "t2",
		LastSeen: time.Now(),
	})
	if err != nil {
		t.Fatalf("офлайн-узел должен отдавать имя: %v", err)
	}
	if rejoined.IP != first.IP || rejoined.Index != first.Index {
		t.Fatalf("адрес и индекс должны сохраниться: было %s/%d, стало %s/%d",
			first.IP, first.Index, rejoined.IP, rejoined.Index)
	}

	// Теперь узел онлайн: чужой ключ с тем же именем не пускаем.
	_, err = store.AddNode(engine.Node{
		ID: "u_3", NetworkID: net.ID, Name: "PC", PublicKey: "key-other", TokenHash: "t3",
		LastSeen: time.Now(),
	})
	if !errors.Is(err, ErrNodeNameTaken) {
		t.Fatalf("ждали ErrNodeNameTaken, получили %v", err)
	}

	// Тот же ключ — это тот же компьютер, пускаем даже онлайн.
	again, err := store.AddNode(engine.Node{
		ID: "u_4", NetworkID: net.ID, Name: "PC", PublicKey: "key-new", TokenHash: "t4",
		LastSeen: time.Now(),
	})
	if err != nil {
		t.Fatalf("тот же ключ должен возвращаться: %v", err)
	}
	if again.IP != first.IP {
		t.Fatalf("адрес изменился: %s", again.IP)
	}
}
