package main

import (
	"errors"
	"fmt"
	"log"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

// StartHub поднимает QUIC-хаб и возвращает его отпечаток сертификата.
func StartHub(store *Store, addr string) (*tunnel.Hub, string, error) {
	cert, err := tunnel.SelfSignedCertificate()
	if err != nil {
		return nil, "", fmt.Errorf("сертификат хаба: %w", err)
	}

	hub, err := tunnel.Listen(tunnel.HubConfig{
		Addr:      addr,
		TLSConfig: tunnel.ServerTLSConfig(cert),
		Auth: func(token string) (tunnel.Identity, error) {
			node, ok := store.NodeByTokenHash(engine.HashToken(token))
			if !ok {
				return tunnel.Identity{}, errors.New("неизвестный токен узла")
			}
			if node.Blocked {
				return tunnel.Identity{}, errors.New("узел заблокирован администратором сети")
			}
			if node.Index == 0 {
				return tunnel.Identity{}, errors.New("узлу не выдан числовой индекс: перезапустите сервер")
			}
			return tunnel.Identity{
				NodeID:    node.ID,
				NetworkID: node.NetworkID,
				Index:     node.Index,
			}, nil
		},
	})
	if err != nil {
		return nil, "", err
	}

	fingerprint := tunnel.Fingerprint(cert.Certificate[0])
	log.Printf("хаб слушает %s (QUIC, ALPN %s)", hub.Addr(), tunnel.ALPN)
	log.Printf("отпечаток сертификата хаба: %s", fingerprint)
	return hub, fingerprint, nil
}
