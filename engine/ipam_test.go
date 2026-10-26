package engine

import (
	"errors"
	"testing"
)

func TestNetworkSubnet(t *testing.T) {
	got, err := NetworkSubnet(3)
	if err != nil {
		t.Fatalf("NetworkSubnet(3): %v", err)
	}
	if got != "10.77.3.0/24" {
		t.Fatalf("получили %q, ждали 10.77.3.0/24", got)
	}

	if _, err := NetworkSubnet(0); err == nil {
		t.Fatal("для индекса 0 ждали ошибку")
	}
	if _, err := NetworkSubnet(255); err == nil {
		t.Fatal("для индекса 255 ждали ошибку")
	}
}

func TestHostIP(t *testing.T) {
	cases := []struct {
		host int
		want string
	}{
		{1, "10.77.3.1"},
		{2, "10.77.3.2"},
		{254, "10.77.3.254"},
	}
	for _, c := range cases {
		got, err := HostIP("10.77.3.0/24", c.host)
		if err != nil {
			t.Fatalf("HostIP(%d): %v", c.host, err)
		}
		if got != c.want {
			t.Fatalf("HostIP(%d) = %q, ждали %q", c.host, got, c.want)
		}
	}

	if _, err := HostIP("10.77.3.0/24", 255); err == nil {
		t.Fatal("для host=255 ждали ошибку: адрес за пределами подсети")
	}
}

func TestNextFreeIP(t *testing.T) {
	got, err := NextFreeIP("10.77.3.0/24", nil)
	if err != nil {
		t.Fatalf("NextFreeIP: %v", err)
	}
	if got != "10.77.3.1" {
		t.Fatalf("первый свободный = %q, ждали 10.77.3.1", got)
	}

	used := map[string]struct{}{"10.77.3.1": {}, "10.77.3.2": {}}
	got, err = NextFreeIP("10.77.3.0/24", used)
	if err != nil {
		t.Fatalf("NextFreeIP с занятыми: %v", err)
	}
	if got != "10.77.3.3" {
		t.Fatalf("следующий свободный = %q, ждали 10.77.3.3", got)
	}
}

func TestNextFreeIPExhausted(t *testing.T) {
	used := make(map[string]struct{}, 254)
	for i := 1; i <= 254; i++ {
		ip, err := HostIP("10.77.3.0/24", i)
		if err != nil {
			t.Fatalf("HostIP(%d): %v", i, err)
		}
		used[ip] = struct{}{}
	}
	if _, err := NextFreeIP("10.77.3.0/24", used); !errors.Is(err, ErrNoFreeIP) {
		t.Fatalf("ждали ErrNoFreeIP, получили %v", err)
	}
}
