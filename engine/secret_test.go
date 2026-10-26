package engine

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	const password = "correct horse battery staple"

	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if encoded == password {
		t.Fatal("хеш совпал с паролем — пароль не захеширован")
	}
	if !VerifyPassword(encoded, password) {
		t.Fatal("верный пароль не прошёл проверку")
	}
	if VerifyPassword(encoded, password+"!") {
		t.Fatal("неверный пароль прошёл проверку")
	}
}

func TestPasswordUniqueSalt(t *testing.T) {
	first, err := HashPassword("same")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword("same")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Fatal("два хеша одного пароля совпали — соль не случайна")
	}
}

func TestVerifyPasswordBrokenHash(t *testing.T) {
	for _, encoded := range []string{"", "pbkdf2-sha256$0$aa$bb", "md5$1$aa$bb", "pbkdf2-sha256$600000$нет-base64$bb"} {
		if VerifyPassword(encoded, "x") {
			t.Fatalf("битый хеш %q прошёл проверку", encoded)
		}
	}
}

func TestToken(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if len(token) < 40 {
		t.Fatalf("токен слишком короткий: %d символов", len(token))
	}

	other, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if token == other {
		t.Fatal("два токена совпали")
	}

	hash := HashToken(token)
	if hash == token {
		t.Fatal("хеш токена совпал с токеном")
	}
	if HashToken(token) != hash {
		t.Fatal("HashToken нестабилен")
	}
	if hash == HashToken(other) {
		t.Fatal("хеши разных токенов совпали")
	}
}

func TestNewID(t *testing.T) {
	id, err := NewID("n_")
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if len(id) != 18 || id[:2] != "n_" {
		t.Fatalf("неожиданный id %q", id)
	}
}
