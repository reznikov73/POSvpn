package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

// runUp поднимает туннель: адаптер, адрес, маршрут и подключение к хабу.
func runUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	hubFlag := fs.String("hub", "", "адрес QUIC-хаба; по умолчанию хост сервера и порт 8788")
	pinFlag := fs.String("pin", "", "отпечаток сертификата хаба (hex); пусто — не проверять")
	mtu := fs.Int("mtu", tunnel.DefaultMTU, "MTU виртуального адаптера")
	adapter := fs.String("adapter", "POSvpn", "имя виртуального адаптера")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" || cfg.NodeID == "" {
		return errors.New("клиент ещё не входил в сеть: выполните create или join")
	}

	if cfg.Hub == "" {
		cfg.Hub = defaultHubAddr(cfg.Server)
	}
	if *hubFlag != "" {
		cfg.Hub = *hubFlag
	}
	if *pinFlag != "" {
		cfg.HubPin = *pinFlag
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	privateKey, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil || len(privateKey) != 32 {
		return errors.New("в настройках нет корректного приватного ключа: выполните create или join заново")
	}

	netmap, err := fetchNetMap(cfg)
	if err != nil {
		return err
	}
	if netmap.Self.Index == 0 {
		return errors.New("сервер не выдал числовой индекс узла: обновите сервер POSvpn")
	}
	subnet, err := netip.ParsePrefix(netmap.Subnet)
	if err != nil {
		return fmt.Errorf("сервер прислал некорректную подсеть %q: %w", netmap.Subnet, err)
	}

	if cfg.HubPin == "" {
		fmt.Println("Внимание: отпечаток сертификата хаба не задан — сервер не проверяется (флаг -pin).")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Сеть %q, ваш адрес %s (индекс %d)\n", netmap.NetworkName, netmap.Self.IP, netmap.Self.Index)
	fmt.Printf("Хаб: %s, участников: %d\n", cfg.Hub, len(netmap.Peers)+1)
	fmt.Println("Туннель поднимается. Ctrl+C — остановить.")

	session, err := startTunnelSession(tunnelOptions{
		Server:     cfg.Server,
		HubAddr:    cfg.Hub,
		Token:      cfg.Token,
		NetworkID:  netmap.NetworkID,
		Subnet:     subnet,
		SelfIndex:  netmap.Self.Index,
		SelfIP:     netmap.Self.IP,
		PrivateKey: privateKey,
		Pin:        cfg.HubPin,
		MTU:        *mtu,
		Adapter:    *adapter,
		NetMap:     netmap,
	})
	if err != nil {
		return err
	}
	defer session.Stop()

	<-ctx.Done()
	return nil
}

// runDown снимает адреса и маршруты с адаптера, не удаляя драйвер.
func runDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	adapter := fs.String("adapter", "POSvpn", "имя виртуального адаптера")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := flushInterface(*adapter); err != nil {
		return err
	}
	fmt.Println("Адреса и маршруты сняты. Туннель остановлен.")
	return nil
}

// fetchNetMap запрашивает у координатора актуальную карту сети.
func fetchNetMap(cfg *config) (engine.NetMap, error) {
	var netmap engine.NetMap
	if err := call(http.MethodGet, cfg.Server+"/api/v1/netmap", cfg.Token, nil, &netmap); err != nil {
		return engine.NetMap{}, err
	}
	return netmap, nil
}

// defaultHubAddr выводит адрес хаба из адреса координатора: тот же хост,
// порт 8788.
func defaultHubAddr(server string) string {
	parsed, err := url.Parse(server)
	if err != nil || parsed.Hostname() == "" {
		return "127.0.0.1:8788"
	}
	return net.JoinHostPort(parsed.Hostname(), "8788")
}
