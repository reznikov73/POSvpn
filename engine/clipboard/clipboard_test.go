//go:build windows

package clipboard

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	// Не оставляем мусор в буфере пользователя: возвращаем прежнее содержимое.
	previous, _ := Read()
	defer func() {
		if previous != "" {
			_ = Write(previous)
			return
		}
		_ = Write("")
	}()

	text := "posvpn-clipboard-" + strings.Repeat("x", 80)
	if err := Write(text); err != nil {
		t.Skipf("буфер обмена недоступен: %v", err)
	}
	got, err := Read()
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got != text {
		t.Fatalf("получили %q", got)
	}
}
