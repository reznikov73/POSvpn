package main

import "testing"

func TestLocalOnlyListen(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8787":    true,
		"localhost:8787":    true,
		"[::1]:8787":        true,
		"0.0.0.0:8787":      false,
		":8787":             false,
		"192.168.5.81:8787": false,
	}
	for addr, want := range cases {
		if got := localOnlyListen(addr); got != want {
			t.Errorf("%s: получили %v, ждали %v", addr, got, want)
		}
	}
}
