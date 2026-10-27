package main

import (
	"crypto/tls"
	"errors"
	"log"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

// StartHub поднимает QUIC-хаб на готовом сертификате.
func StartHub(store *Store, addr string, cert tls.Certificate) (*tunnel.Hub, error) {
	hub, err := tunnel.Listen(tunnel.HubConfig{
		Addr:      addr,
		TLSConfig: tunnel.ServerTLSConfig(cert),
		Auth: func(token string) (tunnel.Identity, error) {
			node, ok := store.NodeByTokenHash(engine.HashToken(token))
			if !ok {
				return tunnel.Identity{}, errors.New("сеть удалена или вход отозван: войдите в сеть заново")
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
		return nil, err
	}

	log.Printf("хаб слушает %s (QUIC, ALPN %s)", hub.Addr(), tunnel.ALPN)
	return hub, nil
}
