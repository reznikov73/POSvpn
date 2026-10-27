package main

import (
	"net/netip"
	"testing"
)

// TestStartNodeProxyDisabled — без разрешения прокси не поднимается.
func TestStartNodeProxyDisabled(t *testing.T) {
	proxy, err := startNodeProxy(tunnelOptions{Subnet: netip.MustParsePrefix("10.77.1.0/24")})
	if err != nil {
		t.Fatalf("выключенный прокси дал ошибку: %v", err)
	}
	if proxy != nil {
		_ = proxy.Close()
		t.Fatal("прокси поднялся без разрешения")
	}
}

// TestStartNodeProxyNeedsSubnet — без подсети сети прокси не знает, кого пускать.
func TestStartNodeProxyNeedsSubnet(t *testing.T) {
	if _, err := startNodeProxy(tunnelOptions{ProxyEnabled: true, ProxyPort: 1080}); err == nil {
		t.Fatal("ожидалась ошибка: подсеть не известна")
	}
}

// TestProxyAddress — адрес для окна и буфера обмена: пустой IP и порт
// превращаются в понятные значения по умолчанию.
func TestProxyAddress(t *testing.T) {
	cases := []struct {
		ip   string
		port int
		want string
	}{
		{"10.77.1.1", 1080, "10.77.1.1:1080"},
		{"10.77.2.3", 0, "10.77.2.3:1080"},
		{"10.77.2.3", 70000, "10.77.2.3:1080"},
		{"10.77.2.3", 9050, "10.77.2.3:9050"},
		{"", 1080, ""},
	}
	for _, c := range cases {
		if got := proxyAddress(c.ip, c.port); got != c.want {
			t.Fatalf("proxyAddress(%q, %d) = %q, ожидалось %q", c.ip, c.port, got, c.want)
		}
	}
}

// TestApplyProxySettings — порт приводится к допустимому, пароль без логина
// не сохраняется, иначе в конфиге остался бы бесполезный секрет.
func TestApplyProxySettings(t *testing.T) {
	cfg := &config{}
	applyProxySettings(cfg, guiRequest{ProxyEnabled: true, ProxyPort: 0})
	if !cfg.ProxyEnabled || cfg.ProxyPort != defaultProxyPort {
		t.Fatalf("порт по умолчанию не подставлен: %+v", cfg)
	}

	applyProxySettings(cfg, guiRequest{ProxyPort: 70000})
	if cfg.ProxyPort != defaultProxyPort {
		t.Fatalf("некорректный порт сохранён: %d", cfg.ProxyPort)
	}

	applyProxySettings(cfg, guiRequest{ProxyPort: 9050, ProxyUser: " u ", ProxyPass: "p"})
	if cfg.ProxyPort != 9050 || cfg.ProxyUser != "u" || cfg.ProxyPass != "p" {
		t.Fatalf("настройки прокси не применились: %+v", cfg)
	}

	applyProxySettings(cfg, guiRequest{ProxyPort: 9050})
	if cfg.ProxyPass != "" {
		t.Fatalf("пароль остался без логина: %q", cfg.ProxyPass)
	}
}
