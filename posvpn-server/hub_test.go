package main

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

func TestHubAcceptsOnlyKnownTokens(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, node, err := store.CreateNetwork("n_1", "office", "hash", engine.Node{
		ID:        "u_1",
		Name:      "PC-1",
		TokenHash: engine.HashToken("good-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Index == 0 {
		t.Fatal("узлу не выдан индекс")
	}

	// Хаб пишет в общий журнал; в тесте он не нужен.
	oldOutput := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		t.Fatal(err)
	}
	hub, err := StartHub(store, "127.0.0.1:0", cert)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := tunnel.Dial(ctx, hub.Addr(), "good-token", tunnel.ClientTLSConfig(""))
	if err != nil {
		t.Fatalf("верный токен: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := tunnel.Dial(ctx, hub.Addr(), "bad-token", tunnel.ClientTLSConfig("")); !errors.Is(err, tunnel.ErrRejected) {
		t.Fatalf("неверный токен: ждали ErrRejected, получили %v", err)
	}
}
