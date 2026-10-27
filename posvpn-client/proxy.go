package main

import (
	"fmt"
	"log"
	"net/netip"

	"posvpn-engine/socks5"
)

// defaultProxyPort — порт SOCKS5 на виртуальном адресе узла-шлюза.
const defaultProxyPort = 1080

// startNodeProxy поднимает SOCKS5-прокси, если узел разрешил выход в интернет.
//
// Слушаем все интерфейсы, но принимаем только источники из подсети сети:
// так порт не открывается для локальной сети и физического интерфейса,
// а запросы приходят только через туннель.
func startNodeProxy(opt tunnelOptions) (*socks5.Proxy, error) {
	if !opt.ProxyEnabled {
		return nil, nil
	}

	port := opt.ProxyPort
	if port <= 0 || port > 65535 {
		port = defaultProxyPort
	}
	subnet := opt.Subnet
	if !subnet.IsValid() {
		return nil, fmt.Errorf("не известна подсеть сети")
	}

	proxy, err := socks5.Listen(socks5.Config{
		Addr:     fmt.Sprintf("0.0.0.0:%d", port),
		Username: opt.ProxyUser,
		Password: opt.ProxyPass,
		Allow: func(addr netip.Addr) bool {
			return subnet.Contains(addr)
		},
		Logf: log.Printf,
	})
	if err != nil {
		return nil, err
	}

	log.Printf("прокси включён: %s:%d (только из %s)", opt.SelfIP, port, subnet)
	return proxy, nil
}

// proxyAddress — адрес, который показывают и копируют в настройках.
func proxyAddress(ip string, port int) string {
	if ip == "" {
		return ""
	}
	if port <= 0 || port > 65535 {
		port = defaultProxyPort
	}
	return fmt.Sprintf("%s:%d", ip, port)
}
