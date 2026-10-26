package engine

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	// pbkdf2Iterations — рекомендованное OWASP значение для PBKDF2-HMAC-SHA256 (2023+).
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32
	saltLen          = 16
	tokenLen         = 32
)

// HashPassword считает хеш пароля сети для хранения на сервере.
// Формат: pbkdf2-sha256$<итерации>$<соль base64>$<хеш base64>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("соль: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", fmt.Errorf("pbkdf2: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword проверяет пароль против сохранённого хеша за постоянное время.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken выпускает токен узла. Клиент хранит его, сервер — только хеш.
func NewToken() (string, error) {
	buf := make([]byte, tokenLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("токен: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken считает хеш токена для хранения на сервере.
// Токен длинный и случайный, поэтому достаточно одного SHA-256 без PBKDF2.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewID выпускает короткий идентификатор с префиксом: n_, u_ и т.п.
func NewID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("id: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}
