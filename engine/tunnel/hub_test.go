package tunnel

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

func testHub(t *testing.T, tokens map[string]Identity) *Hub {
	t.Helper()
	cert, err := SelfSignedCertificate()
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	hub, err := Listen(HubConfig{
		Addr:      "127.0.0.1:0",
		TLSConfig: ServerTLSConfig(cert),
		Auth: func(token string) (Identity, error) {
			id, ok := tokens[token]
			if !ok {
				return Identity{}, errors.New("неизвестный токен узла")
			}
			return id, nil
		},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("хаб: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	return hub
}

func dialHub(t *testing.T, hub *Hub, token string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, hub.Addr(), token, ClientTLSConfig(""))
	if err != nil {
		t.Fatalf("подключение %q: %v", token, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func receive(t *testing.T, client *Client, timeout time.Duration) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return client.Receive(ctx)
}

// pairForTest собирает направленные шифры двух узлов.
func pairForTest(t *testing.T, networkID string, ai, bi uint32) (aSend, bRecv, bSend, aRecv *Cipher) {
	t.Helper()
	curve := ecdh.X25519()
	a, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	aSend, aRecv, err = PairCiphers(a.Bytes(), b.PublicKey().Bytes(), networkID, ai, bi)
	if err != nil {
		t.Fatal(err)
	}
	bSend, bRecv, err = PairCiphers(b.Bytes(), a.PublicKey().Bytes(), networkID, bi, ai)
	if err != nil {
		t.Fatal(err)
	}
	return aSend, bRecv, bSend, aRecv
}

func TestHubRoutesAndRewritesSource(t *testing.T) {
	hub := testHub(t, map[string]Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
		"tok-b": {NodeID: "u_b", NetworkID: "n1", Index: 2},
	})
	a := dialHub(t, hub, "tok-a")
	b := dialHub(t, hub, "tok-b")

	aSend, bRecv, _, _ := pairForTest(t, "n1", 1, 2)

	frame, err := aSend.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("привет через хаб"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(frame); err != nil {
		t.Fatalf("отправка: %v", err)
	}

	raw, err := receive(t, b, 3*time.Second)
	if err != nil {
		t.Fatalf("приём: %v", err)
	}
	hdr, payload, err := bRecv.Decrypt(raw)
	if err != nil {
		t.Fatalf("расшифровка: %v", err)
	}
	if string(payload) != "привет через хаб" {
		t.Fatalf("содержимое %q", payload)
	}
	if hdr.Src != 1 || hdr.Dst != 2 {
		t.Fatalf("адреса src=%d dst=%d", hdr.Src, hdr.Dst)
	}

	stats := hub.Stats()
	if stats.Forwarded != 1 || stats.Received != 1 {
		t.Fatalf("счётчики: %+v", stats)
	}
}

func TestHubBroadcast(t *testing.T) {
	hub := testHub(t, map[string]Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
		"tok-b": {NodeID: "u_b", NetworkID: "n1", Index: 2},
	})
	a := dialHub(t, hub, "tok-a")
	b := dialHub(t, hub, "tok-b")

	aSend, bRecv, _, _ := pairForTest(t, "n1", 1, 2)

	frame, err := aSend.Encrypt(Header{Type: FrameIPv4, Dst: BroadcastIndex, Src: 1}, []byte("всем"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(frame); err != nil {
		t.Fatal(err)
	}

	raw, err := receive(t, b, 3*time.Second)
	if err != nil {
		t.Fatalf("приём broadcast: %v", err)
	}
	hdr, payload, err := bRecv.Decrypt(raw)
	if err != nil {
		t.Fatalf("расшифровка broadcast: %v", err)
	}
	if string(payload) != "всем" || hdr.Dst != BroadcastIndex {
		t.Fatalf("payload=%q dst=%d", payload, hdr.Dst)
	}

	// Отправителю собственный broadcast не возвращается.
	if _, err := receive(t, a, 300*time.Millisecond); err == nil {
		t.Fatal("хаб вернул broadcast отправителю")
	}
}

func TestHubDropsUnknownDestination(t *testing.T) {
	hub := testHub(t, map[string]Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
		"tok-b": {NodeID: "u_b", NetworkID: "n1", Index: 2},
	})
	a := dialHub(t, hub, "tok-a")
	b := dialHub(t, hub, "tok-b")

	aSend, _, _, _ := pairForTest(t, "n1", 1, 2)
	frame, _ := aSend.Encrypt(Header{Type: FrameIPv4, Dst: 99, Src: 1}, []byte("никому"))

	if err := a.Send(frame); err != nil {
		t.Fatal(err)
	}
	if _, err := receive(t, b, 300*time.Millisecond); err == nil {
		t.Fatal("кадр неизвестному адресату дошёл до пира")
	}
	if hub.Stats().Dropped == 0 {
		t.Fatal("хаб не посчитал отброшенный кадр")
	}
}

func TestHubRewritesSpoofedSource(t *testing.T) {
	hub := testHub(t, map[string]Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
		"tok-b": {NodeID: "u_b", NetworkID: "n1", Index: 2},
	})
	a := dialHub(t, hub, "tok-a")
	b := dialHub(t, hub, "tok-b")

	aSend, bRecv, _, _ := pairForTest(t, "n1", 1, 2)

	// A выдаёт себя за узел 99. Хаб обязан поставить настоящий индекс,
	// после чего тег AEAD у получателя не сходится.
	frame, _ := aSend.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 99}, []byte("подделка"))
	if err := a.Send(frame); err != nil {
		t.Fatal(err)
	}

	raw, err := receive(t, b, 3*time.Second)
	if err != nil {
		t.Fatalf("приём: %v", err)
	}
	hdr, err := ParseHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Src != 1 {
		t.Fatalf("хаб не переписал src: %d", hdr.Src)
	}
	if _, _, err := bRecv.Decrypt(raw); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("подмена src должна ломать тег, получили %v", err)
	}
}

func TestHubRejectsBadToken(t *testing.T) {
	hub := testHub(t, map[string]Identity{
		"tok-a": {NodeID: "u_a", NetworkID: "n1", Index: 1},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Dial(ctx, hub.Addr(), "чужой", ClientTLSConfig(""))
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("ждали ErrRejected, получили %v", err)
	}
}

func TestPinnedFingerprint(t *testing.T) {
	cert, err := SelfSignedCertificate()
	if err != nil {
		t.Fatal(err)
	}
	pin := Fingerprint(cert.Certificate[0])

	hub, err := Listen(HubConfig{
		Addr:      "127.0.0.1:0",
		TLSConfig: ServerTLSConfig(cert),
		Auth: func(string) (Identity, error) {
			return Identity{NodeID: "u", NetworkID: "n", Index: 1}, nil
		},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Dial(ctx, hub.Addr(), "tok", ClientTLSConfig(pin)); err != nil {
		t.Fatalf("верный отпечаток: %v", err)
	}

	wrong := "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := Dial(ctx, hub.Addr(), "tok", ClientTLSConfig(wrong)); err == nil {
		t.Fatal("подключение с неверным отпечатком должно падать")
	}
}
