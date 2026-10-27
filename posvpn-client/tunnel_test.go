package main

import (
	"net/netip"
	"testing"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

func TestParsePacketDestinationIPv4(t *testing.T) {
	packet := make([]byte, 20)
	packet[0] = 0x45 // IPv4, заголовок 5 слов
	copy(packet[16:20], []byte{10, 77, 1, 5})

	addr, frameType, ok := parsePacketDestination(packet)
	if !ok || frameType != tunnel.FrameIPv4 {
		t.Fatalf("ok=%v type=%v", ok, frameType)
	}
	if addr != netip.MustParseAddr("10.77.1.5") {
		t.Fatalf("адрес %s", addr)
	}
}

func TestParsePacketDestinationIPv6(t *testing.T) {
	packet := make([]byte, 40)
	packet[0] = 0x60
	dst := netip.MustParseAddr("fd00::2").As16()
	copy(packet[24:40], dst[:])

	addr, frameType, ok := parsePacketDestination(packet)
	if !ok || frameType != tunnel.FrameIPv6 {
		t.Fatalf("ok=%v type=%v", ok, frameType)
	}
	if addr != netip.MustParseAddr("fd00::2") {
		t.Fatalf("адрес %s", addr)
	}
}

func TestParsePacketDestinationRejectsShort(t *testing.T) {
	if _, _, ok := parsePacketDestination([]byte{0x45, 0, 0}); ok {
		t.Fatal("короткий IPv4-пакет принят")
	}
	if _, _, ok := parsePacketDestination([]byte{0x60}); ok {
		t.Fatal("короткий IPv6-пакет принят")
	}
	if _, _, ok := parsePacketDestination([]byte{0x00}); ok {
		t.Fatal("пакет без версии IP принят")
	}
}

func TestIsBroadcast(t *testing.T) {
	subnet := netip.MustParsePrefix("10.77.3.0/24")
	cases := []struct {
		addr string
		want bool
	}{
		{"255.255.255.255", true},
		{"10.77.3.255", true},
		{"224.0.0.251", true},
		{"10.77.3.42", false},
		{"10.77.4.255", false},
		{"8.8.8.8", false},
	}
	for _, tc := range cases {
		got := isBroadcast(netip.MustParseAddr(tc.addr), subnet)
		if got != tc.want {
			t.Errorf("%s: получили %v, ждали %v", tc.addr, got, tc.want)
		}
	}
}

func TestBroadcastOf(t *testing.T) {
	cases := map[string]string{
		"10.77.1.0/24":   "10.77.1.255",
		"10.77.0.0/16":   "10.77.255.255",
		"192.168.8.0/22": "192.168.11.255",
	}
	for prefix, want := range cases {
		got := broadcastOf(netip.MustParsePrefix(prefix))
		if got.String() != want {
			t.Errorf("%s: получили %s, ждали %s", prefix, got, want)
		}
	}
}

func TestPeerSetRebuildKeepsCiphers(t *testing.T) {
	a := mustKeyPair(t)
	b := mustKeyPair(t)
	netmap := engine.NetMap{
		NetworkID: "n1",
		Self:      engine.Peer{NodeID: "u_a", IP: "10.77.1.1", Index: 1, PublicKey: a.public},
		Peers: []engine.Peer{
			{NodeID: "u_b", Name: "B", IP: "10.77.1.2", Index: 2, PublicKey: b.public},
		},
	}

	peers := newPeerSet()
	if err := peers.rebuild(netmap, a.private); err != nil {
		t.Fatalf("первая сборка: %v", err)
	}
	first := peers.byIndex(2)
	if first == nil {
		t.Fatal("пир не найден")
	}
	if peers.byAddr(netip.MustParseAddr("10.77.1.2")) != first {
		t.Fatal("пир не найден по адресу")
	}

	// Та же карта — шифры должны сохраниться, иначе счётчики обнулятся.
	if err := peers.rebuild(netmap, a.private); err != nil {
		t.Fatal(err)
	}
	if peers.byIndex(2) != first {
		t.Fatal("шифры пира пересозданы без смены ключа")
	}

	// Новый ключ пира — шифры обязаны обновиться.
	other := mustKeyPair(t)
	netmap.Peers[0].PublicKey = other.public
	if err := peers.rebuild(netmap, a.private); err != nil {
		t.Fatal(err)
	}
	if peers.byIndex(2) == first {
		t.Fatal("шифры не обновлены после смены ключа пира")
	}
}

func TestPeerSetRequiresSelfIndex(t *testing.T) {
	a := mustKeyPair(t)
	peers := newPeerSet()
	err := peers.rebuild(engine.NetMap{Self: engine.Peer{IP: "10.77.1.1", Index: 0}}, a.private)
	if err == nil {
		t.Fatal("карта без индекса узла должна отвергаться")
	}
}

// TestTunnelOptionsFromConfig — опции туннеля собираются в одном месте, поэтому
// настройки прокси попадают в них при любом запуске: окно, служба и CLI.
// Раньше служба собирала опции сама и теряла эти поля, из-за чего на
// узле-шлюзе прокси не поднимался.
func TestTunnelOptionsFromConfig(t *testing.T) {
	cfg := &config{
		Server:       "https://hub.example:8443",
		Token:        "token",
		ProxyEnabled: true,
		ProxyPort:    9050,
		ProxyUser:    "user",
		ProxyPass:    "pass",
	}
	netmap := engine.NetMap{
		NetworkID: "net-1",
		Self:      engine.Peer{Index: 3, IP: "10.77.1.1"},
	}
	subnet := netip.MustParsePrefix("10.77.1.0/24")
	privateKey := make([]byte, 32)

	opt := tunnelOptionsFromConfig(cfg, netmap, subnet, privateKey, tunnel.DefaultMTU, "POSvpn")

	if opt.HubAddr != "hub.example:8788" {
		t.Fatalf("адрес хаба %q, ожидался вывод из адреса сервера", opt.HubAddr)
	}
	if opt.Server != cfg.Server || opt.Token != cfg.Token || opt.NetworkID != netmap.NetworkID {
		t.Fatalf("опции потеряли данные входа: %+v", opt)
	}
	if opt.Subnet != subnet || opt.SelfIndex != 3 || opt.SelfIP != "10.77.1.1" {
		t.Fatalf("опции потеряли адресацию узла: %+v", opt)
	}
	if opt.MTU != tunnel.DefaultMTU || opt.Adapter != "POSvpn" {
		t.Fatalf("опции потеряли параметры адаптера: %+v", opt)
	}
	if !opt.ProxyEnabled || opt.ProxyPort != 9050 || opt.ProxyUser != "user" || opt.ProxyPass != "pass" {
		t.Fatalf("опции потеряли настройки прокси: %+v", opt)
	}

	cfg.Hub = "10.1.1.1:8788"
	if opt := tunnelOptionsFromConfig(cfg, netmap, subnet, privateKey, tunnel.DefaultMTU, "POSvpn"); opt.HubAddr != cfg.Hub {
		t.Fatalf("заданный адрес хаба перезаписан: %q", opt.HubAddr)
	}
}
