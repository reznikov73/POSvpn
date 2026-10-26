package tunnel

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"testing"
)

func TestPairCiphersAgree(t *testing.T) {
	curve := ecdh.X25519()
	a, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	aSend, aRecv, err := PairCiphers(a.Bytes(), b.PublicKey().Bytes(), "n_test", 1, 2)
	if err != nil {
		t.Fatalf("ключи A: %v", err)
	}
	bSend, bRecv, err := PairCiphers(b.Bytes(), a.PublicKey().Bytes(), "n_test", 2, 1)
	if err != nil {
		t.Fatalf("ключи B: %v", err)
	}

	// A → B
	frame, err := aSend.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("привет"))
	if err != nil {
		t.Fatalf("шифрование: %v", err)
	}
	hdr, payload, err := bRecv.Decrypt(frame)
	if err != nil {
		t.Fatalf("расшифровка B: %v", err)
	}
	if string(payload) != "привет" || hdr.Src != 1 || hdr.Dst != 2 {
		t.Fatalf("получили %q, src=%d dst=%d", payload, hdr.Src, hdr.Dst)
	}

	// B → A своим ключом
	frame, err = bSend.Encrypt(Header{Type: FramePing, Dst: 1, Src: 2}, nil)
	if err != nil {
		t.Fatalf("шифрование B: %v", err)
	}
	if _, _, err := aRecv.Decrypt(frame); err != nil {
		t.Fatalf("расшифровка A: %v", err)
	}
}

func TestPairCiphersRejectWrongDirection(t *testing.T) {
	curve := ecdh.X25519()
	a, _ := curve.GenerateKey(rand.Reader)
	b, _ := curve.GenerateKey(rand.Reader)

	aSend, aRecv, err := PairCiphers(a.Bytes(), b.PublicKey().Bytes(), "n_test", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	bSend, _, err := PairCiphers(b.Bytes(), a.PublicKey().Bytes(), "n_test", 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Направленные ключи разные: то, что B шифрует своим ключом, не читается
	// ключом приёма B (он же ключ отправки A).
	frame, err := bSend.Encrypt(Header{Type: FrameIPv4, Dst: 1, Src: 2}, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := aSend.Decrypt(frame); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("ждали ErrDecrypt, получили %v", err)
	}

	// А ключом приёма A — читается.
	frame, err = bSend.Encrypt(Header{Type: FrameIPv4, Dst: 1, Src: 2}, []byte("y"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := aRecv.Decrypt(frame); err != nil {
		t.Fatalf("ключ приёма A должен читать кадры B: %v", err)
	}
}

func TestWrongNetworkKeyDiffers(t *testing.T) {
	curve := ecdh.X25519()
	a, _ := curve.GenerateKey(rand.Reader)
	b, _ := curve.GenerateKey(rand.Reader)

	aSend, _, err := PairCiphers(a.Bytes(), b.PublicKey().Bytes(), "network_one", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, bRecv, err := PairCiphers(b.Bytes(), a.PublicKey().Bytes(), "network_two", 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	frame, _ := aSend.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("x"))
	if _, _, err := bRecv.Decrypt(frame); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("ключи разных сетей не должны совпадать, получили %v", err)
	}
}

func TestReplayRejected(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	send, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	recv, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	frame, err := send.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("один"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := recv.Decrypt(frame); err != nil {
		t.Fatalf("первый раз: %v", err)
	}
	if _, _, err := recv.Decrypt(frame); !errors.Is(err, ErrReplay) {
		t.Fatalf("повтор: ждали ErrReplay, получили %v", err)
	}
}

func TestReplayWindowOutOfOrder(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	send, _ := NewCipher(key)
	recv, _ := NewCipher(key)

	frames := make([][]byte, 0, 100)
	for i := 0; i < 100; i++ {
		f, err := send.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
	}

	// Принимаем 100-й, затем более ранние внутри окна — можно.
	if _, _, err := recv.Decrypt(frames[99]); err != nil {
		t.Fatalf("100-й кадр: %v", err)
	}
	if _, _, err := recv.Decrypt(frames[70]); err != nil {
		t.Fatalf("кадр 70 внутри окна: %v", err)
	}
	// А 1-й — уже за пределами окна в 64 кадра.
	if _, _, err := recv.Decrypt(frames[0]); !errors.Is(err, ErrReplay) {
		t.Fatalf("кадр 1: ждали ErrReplay, получили %v", err)
	}
}

func TestTamperedFrameRejected(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 32)
	send, _ := NewCipher(key)
	recv, _ := NewCipher(key)

	frame, _ := send.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("данные"))
	frame[len(frame)-1] ^= 0xFF
	if _, _, err := recv.Decrypt(frame); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("подмена тега: ждали ErrDecrypt, получили %v", err)
	}

	// Подмена заголовка тоже ломает тег: он входит в AAD.
	frame, _ = send.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("данные"))
	SetDst(frame, 3)
	if _, _, err := recv.Decrypt(frame); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("подмена dst: ждали ErrDecrypt, получили %v", err)
	}
}

func TestRestartNewEpochAccepted(t *testing.T) {
	key := bytes.Repeat([]byte{5}, 32)
	recv, _ := NewCipher(key)

	first, _ := NewCipher(key) // «прошлый запуск»
	f1, _ := first.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("до перезапуска"))
	if _, _, err := recv.Decrypt(f1); err != nil {
		t.Fatal(err)
	}

	second, _ := NewCipher(key) // «после перезапуска», счётчик снова 1
	f2, _ := second.Encrypt(Header{Type: FrameIPv4, Dst: 2, Src: 1}, []byte("после перезапуска"))
	if _, payload, err := recv.Decrypt(f2); err != nil || string(payload) != "после перезапуска" {
		t.Fatalf("новый epoch должен приниматься: err=%v payload=%q", err, payload)
	}
}

func TestFrameTooLarge(t *testing.T) {
	send, _ := NewCipher(bytes.Repeat([]byte{1}, 32))
	if _, err := send.Encrypt(Header{Type: FrameIPv4, Dst: 1, Src: 2}, make([]byte, MaxPayloadSize+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ждали ErrFrameTooLarge, получили %v", err)
	}
}
