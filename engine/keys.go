package engine

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyPair — статическая пара ключей узла (X25519).
// Приватный ключ остаётся на клиенте, публичный уходит на сервер и попадает
// в карту сети, чтобы пиры могли установить общий секрет.
type KeyPair struct {
	Private string
	Public  string
}

// GenerateKeyPair выпускает новую пару ключей узла.
func GenerateKeyPair() (KeyPair, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("ключ X25519: %w", err)
	}
	return KeyPair{
		Private: base64.StdEncoding.EncodeToString(key.Bytes()),
		Public:  base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()),
	}, nil
}

// ParsePublicKey проверяет, что строка — корректный публичный ключ X25519.
func ParsePublicKey(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("публичный ключ не base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("публичный ключ длиной %d байт, ждали 32", len(raw))
	}
	return raw, nil
}
