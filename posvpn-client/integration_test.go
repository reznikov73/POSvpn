package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

// fakeDevice — адаптер без драйвера: пакеты приходят из канала и пишутся
// в канал. Позволяет проверить весь путь туннеля в обычном тесте.
type fakeDevice struct {
	in   chan []byte
	out  chan []byte
	once sync.Once
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{in: make(chan []byte, 8), out: make(chan []byte, 8)}
}

func (d *fakeDevice) ReadPacket() ([]byte, error) {
	packet, ok := <-d.in
	if !ok {
		return nil, io.EOF
	}
	return packet, nil
}

func (d *fakeDevice) WritePacket(packet []byte) error {
	copied := append([]byte(nil), packet...)
	d.out <- copied
	return nil
}

func (d *fakeDevice) MTU() int { return tunnel.DefaultMTU }

func (d *fakeDevice) Close() error {
	d.once.Do(func() { close(d.in) })
	return nil
}

func ipv4Packet(src, dst string, payload []byte) []byte {
	packet := make([]byte, 20+len(payload))
	packet[0] = 0x45
	packet[2] = byte((20 + len(payload)) >> 8)
	packet[3] = byte(20 + len(payload))
	packet[8] = 64 // TTL
	packet[9] = 17 // UDP
	copy(packet[12:16], netip.MustParseAddr(src).AsSlice())
	copy(packet[16:20], netip.MustParseAddr(dst).AsSlice())
	copy(packet[20:], payload)
	return packet
}

func TestTunnelEndToEndThroughHub(t *testing.T) {
	// Общий журнал хаба в тесте не нужен.
	oldOutput := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]tunnel.Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
		"tok-b": {NodeID: "u_b", NetworkID: "n1", Index: 2},
	}
	hub, err := tunnel.Listen(tunnel.HubConfig{
		Addr:      "127.0.0.1:0",
		TLSConfig: tunnel.ServerTLSConfig(cert),
		Auth: func(token string) (tunnel.Identity, error) {
			id, ok := tokens[token]
			if !ok {
				return tunnel.Identity{}, io.EOF
			}
			return id, nil
		},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := mustKeyPair(t)
	b := mustKeyPair(t)

	clientA, err := tunnel.Dial(ctx, hub.Addr(), "tok-a", tunnel.ClientTLSConfig(""))
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Close()
	clientB, err := tunnel.Dial(ctx, hub.Addr(), "tok-b", tunnel.ClientTLSConfig(""))
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Close()

	netmapA := engine.NetMap{
		NetworkID: "n1",
		Self:      engine.Peer{NodeID: "u_a", IP: "10.77.1.1", Index: 1, PublicKey: a.public},
		Peers:     []engine.Peer{{NodeID: "u_b", Name: "B", IP: "10.77.1.2", Index: 2, PublicKey: b.public}},
	}
	netmapB := engine.NetMap{
		NetworkID: "n1",
		Self:      engine.Peer{NodeID: "u_b", IP: "10.77.1.2", Index: 2, PublicKey: b.public},
		Peers:     []engine.Peer{{NodeID: "u_a", Name: "A", IP: "10.77.1.1", Index: 1, PublicKey: a.public}},
	}

	peersA, peersB := newPeerSet(), newPeerSet()
	if err := peersA.rebuild(netmapA, a.private); err != nil {
		t.Fatal(err)
	}
	if err := peersB.rebuild(netmapB, b.private); err != nil {
		t.Fatal(err)
	}

	devA, devB := newFakeDevice(), newFakeDevice()
	defer devA.Close()
	defer devB.Close()

	var currentA atomic.Pointer[tunnel.Client]
	currentA.Store(clientA)
	optA := tunnelOptions{Subnet: netip.MustParsePrefix("10.77.1.0/24"), SelfIndex: 1}

	go func() { _ = sendLoop(ctx, devA, peersA, &currentA, optA) }()
	go func() { _ = recvLoop(ctx, clientB, peersB, devB, 2) }()

	packet := ipv4Packet("10.77.1.1", "10.77.1.2", []byte("ping через туннель"))
	devA.in <- packet

	select {
	case got := <-devB.out:
		if !bytes.Equal(got, packet) {
			t.Fatalf("пакет дошёл с изменениями: %x", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("пакет не дошёл до второго узла")
	}

	// Бродкаст уходит всем участникам.
	broadcast := ipv4Packet("10.77.1.1", "10.77.1.255", []byte("всем"))
	devA.in <- broadcast
	select {
	case got := <-devB.out:
		if !bytes.Equal(got, broadcast) {
			t.Fatalf("бродкаст изменён: %x", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("бродкаст не дошёл")
	}

	// Пакет чужому адресату не должен никуда уйти.
	devA.in <- ipv4Packet("10.77.1.1", "10.77.1.77", []byte("никому"))
	select {
	case got := <-devB.out:
		t.Fatalf("чужой пакет дошёл: %x", got)
	case <-time.After(300 * time.Millisecond):
	}
}

type keyPair struct {
	private []byte
	public  string
}

func mustKeyPair(t *testing.T) keyPair {
	t.Helper()
	pair, err := engine.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	private, err := base64.StdEncoding.DecodeString(pair.Private)
	if err != nil {
		t.Fatal(err)
	}
	return keyPair{private: private, public: pair.Public}
}
