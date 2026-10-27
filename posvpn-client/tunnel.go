package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"posvpn-engine"
	"posvpn-engine/tunnel"
)

// tunnelOptions — всё, что нужно туннелю после входа в сеть.
type tunnelOptions struct {
	Server     string
	HubAddr    string
	Token      string
	NetworkID  string
	Subnet     netip.Prefix
	SelfIndex  uint32
	SelfIP     string
	PrivateKey []byte
	Pin        string
	MTU        int
	Adapter    string
	NetMap     engine.NetMap

	// Прокси на этом узле: участники сети ходят в интернет через него.
	ProxyEnabled bool
	ProxyPort    int
	ProxyUser    string
	ProxyPass    string
}

// peerState — пир и его направленные шифры. Шифры переживают переподключение
// к хабу: если начать счётчики заново с тем же ключом, пир отбросит кадры
// как повторные.
type peerState struct {
	index        uint32
	ip           netip.Addr
	name         string
	role         string
	pub          []byte
	send         *tunnel.Cipher
	recv         *tunnel.Cipher
	netmapOnline atomic.Bool
	lastPong     atomic.Int64 // unix nano
	pingSent     atomic.Int64 // unix nano последнего ping
	rttMs        atomic.Int64
}

// online — пир в сети, если так сказала карта сети или он недавно ответил
// на ping через туннель.
func (p *peerState) online() bool {
	if p.netmapOnline.Load() {
		return true
	}
	last := p.lastPong.Load()
	return last > 0 && time.Since(time.Unix(0, last)) < 10*time.Second
}

func (p *peerState) rtt() int64 {
	if p.rttMs.Load() == 0 && p.lastPong.Load() == 0 {
		return 0
	}
	return p.rttMs.Load()
}

func (p *peerState) setRTT(d time.Duration) {
	if d <= 0 {
		return
	}
	p.rttMs.Store(d.Milliseconds())
	p.lastPong.Store(time.Now().UnixNano())
}

// peerSet — карта участников сети, обновляемая по heartbeat.
type peerSet struct {
	mu    sync.RWMutex
	byIP  map[netip.Addr]*peerState
	byIdx map[uint32]*peerState
}

func newPeerSet() *peerSet {
	return &peerSet{
		byIP:  make(map[netip.Addr]*peerState),
		byIdx: make(map[uint32]*peerState),
	}
}

// rebuild пересобирает карту из netmap. Для пиров с тем же индексом и тем же
// публичным ключом сохраняются прежние шифры, чтобы счётчики не сбрасывались.
func (p *peerSet) rebuild(netmap engine.NetMap, privateKey []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if netmap.Self.Index == 0 {
		return errors.New("сервер не выдал числовой индекс узла: обновите сервер POSvpn")
	}

	byIP := make(map[netip.Addr]*peerState, len(netmap.Peers))
	byIdx := make(map[uint32]*peerState, len(netmap.Peers))

	for _, peer := range netmap.Peers {
		if peer.Index == 0 {
			continue
		}
		ip, err := netip.ParseAddr(peer.IP)
		if err != nil {
			log.Printf("пир %q: адрес %q не разобран: %v", peer.Name, peer.IP, err)
			continue
		}
		pub, err := engine.ParsePublicKey(peer.PublicKey)
		if err != nil {
			log.Printf("пир %q: ключ не разобран: %v", peer.Name, err)
			continue
		}

		if old, ok := p.byIdx[peer.Index]; ok && bytes.Equal(old.pub, pub) {
			old.name = peer.Name
			old.role = peer.Role
			old.netmapOnline.Store(peer.Online)
			byIP[ip] = old
			byIdx[peer.Index] = old
			continue
		}

		send, recv, err := tunnel.PairCiphers(privateKey, pub, netmap.NetworkID, netmap.Self.Index, peer.Index)
		if err != nil {
			log.Printf("пир %q: ключи пары: %v", peer.Name, err)
			continue
		}
		state := &peerState{
			index: peer.Index,
			ip:    ip,
			name:  peer.Name,
			role:  peer.Role,
			pub:   pub,
			send:  send,
			recv:  recv,
		}
		state.netmapOnline.Store(peer.Online)
		byIP[ip] = state
		byIdx[peer.Index] = state
	}

	p.byIP = byIP
	p.byIdx = byIdx
	return nil
}

func (p *peerSet) byAddr(addr netip.Addr) *peerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byIP[addr]
}

func (p *peerSet) byIndex(index uint32) *peerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byIdx[index]
}

func (p *peerSet) all() []*peerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*peerState, 0, len(p.byIdx))
	for _, peer := range p.byIdx {
		out = append(out, peer)
	}
	return out
}

// tunnelSession — туннель, которым управляет GUI: старт, стоп, состояние.
type tunnelSession struct {
	opt    tunnelOptions
	peers  *peerSet
	cancel context.CancelFunc

	mu        sync.RWMutex
	running   bool
	connected bool
	lastError string
	startedAt time.Time
}

// startTunnelSession поднимает адаптер и запускает туннель в фоне.
func startTunnelSession(opt tunnelOptions) (*tunnelSession, error) {
	peers := newPeerSet()
	if err := peers.rebuild(opt.NetMap, opt.PrivateKey); err != nil {
		return nil, err
	}

	device, err := openDevice(opt.Adapter, opt.SelfIP, opt.MTU)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &tunnelSession{
		opt:       opt,
		peers:     peers,
		cancel:    cancel,
		running:   true,
		startedAt: time.Now(),
	}
	go func() {
		err := runSession(ctx, opt, device, peers, s.setState)
		_ = device.Close()
		s.finish(err)
	}()
	return s, nil
}

// Stop останавливает туннель и снимает адреса с адаптера.
func (s *tunnelSession) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *tunnelSession) setState(connected bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
	if err != nil {
		s.lastError = err.Error()
	}
	if connected {
		s.lastError = ""
	}
}

func (s *tunnelSession) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.connected = false
	if err != nil && !errors.Is(err, context.Canceled) {
		s.lastError = err.Error()
	}
}

func (s *tunnelSession) status() (running, connected bool, lastError string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running, s.connected, s.lastError
}

// runTunnel — блокирующий вариант для CLI.
func runTunnel(ctx context.Context, opt tunnelOptions) error {
	peers := newPeerSet()
	if err := peers.rebuild(opt.NetMap, opt.PrivateKey); err != nil {
		return err
	}

	device, err := openDevice(opt.Adapter, opt.SelfIP, opt.MTU)
	if err != nil {
		return err
	}
	defer device.Close()

	return runSession(ctx, opt, device, peers, nil)
}

// runSession держит туннель: подключается к хабу, переподключается при обрыве
// и обновляет карту сети по heartbeat.
func runSession(ctx context.Context, opt tunnelOptions, device packetDevice, peers *peerSet, onState func(bool, error)) error {
	var current atomic.Pointer[tunnel.Client]
	errCh := make(chan error, 2)

	// Прокси живёт столько же, сколько сеанс: включается вместе с туннелем,
	// гаснет при отключении, смене настроек и выходе.
	if proxy, err := startNodeProxy(opt); err != nil {
		log.Printf("прокси не запущен: %v", err)
	} else if proxy != nil {
		defer func() { _ = proxy.Close() }()
	}

	go func() { errCh <- sendLoop(ctx, device, peers, &current, opt) }()
	go heartbeatLoop(ctx, opt, peers)

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		select {
		case err := <-errCh:
			return err
		default:
		}

		client, err := tunnel.Dial(ctx, opt.HubAddr, opt.Token, tunnel.ClientTLSConfig(opt.Pin))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("хаб %s недоступен: %v (повтор через %s)", opt.HubAddr, err, backoff)
			if onState != nil {
				onState(false, nil)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			continue
		}

		backoff = time.Second
		current.Store(client)
		log.Printf("туннель поднят: %s → %s", client.LocalAddr(), client.RemoteAddr())
		if onState != nil {
			onState(true, nil)
		}

		connCtx, connCancel := context.WithCancel(ctx)
		go pingLoop(connCtx, client, peers, opt.SelfIndex)

		recvErr := make(chan error, 1)
		go func() { recvErr <- recvLoop(connCtx, client, peers, device, opt.SelfIndex) }()

		select {
		case <-ctx.Done():
			connCancel()
			current.Store(nil)
			_ = client.Close()
			return nil
		case err := <-recvErr:
			connCancel()
			current.Store(nil)
			_ = client.Close()
			log.Printf("канал к хабу потерян: %v", err)
			if onState != nil {
				onState(false, err)
			}
		case err := <-errCh:
			connCancel()
			current.Store(nil)
			_ = client.Close()
			return err
		}
	}
}

// sendLoop читает пакеты из адаптера и отправляет их в текущее соединение.
// Пока соединения нет, пакеты отбрасываются.
func sendLoop(ctx context.Context, device packetDevice, peers *peerSet, current *atomic.Pointer[tunnel.Client], opt tunnelOptions) error {
	for {
		packet, err := device.ReadPacket()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("чтение из адаптера: %w", err)
		}

		client := current.Load()
		if client == nil {
			continue
		}

		dst, frameType, ok := parsePacketDestination(packet)
		if !ok {
			continue
		}

		if isBroadcast(dst, opt.Subnet) {
			for _, peer := range peers.all() {
				sendFrame(client, peer, frameType, packet, opt.SelfIndex)
			}
			continue
		}

		peer := peers.byAddr(dst)
		if peer == nil {
			// Адресат не из нашей сети: молча отбрасываем, иначе один
			// сканирующий порт зальёт журнал.
			continue
		}
		sendFrame(client, peer, frameType, packet, opt.SelfIndex)
	}
}

func sendFrame(client *tunnel.Client, peer *peerState, frameType tunnel.FrameType, packet []byte, selfIndex uint32) {
	frame, err := peer.send.Encrypt(tunnel.Header{
		Type: frameType,
		Dst:  peer.index,
		Src:  selfIndex,
	}, packet)
	if err != nil {
		log.Printf("шифрование для узла %d: %v", peer.index, err)
		return
	}
	if err := client.Send(frame); err != nil {
		log.Printf("отправка узлу %d: %v", peer.index, err)
	}
}

// recvLoop принимает кадры от хаба, расшифровывает и пишет в адаптер.
func recvLoop(ctx context.Context, client *tunnel.Client, peers *peerSet, device packetDevice, selfIndex uint32) error {
	for {
		frame, err := client.Receive(ctx)
		if err != nil {
			return err
		}

		header, err := tunnel.ParseHeader(frame)
		if err != nil {
			continue
		}
		peer := peers.byIndex(header.Src)
		if peer == nil {
			continue
		}

		header, payload, err := peer.recv.Decrypt(frame)
		if err != nil {
			if !errors.Is(err, tunnel.ErrReplay) {
				log.Printf("кадр от узла %d: %v", peer.index, err)
			}
			continue
		}

		switch header.Type {
		case tunnel.FramePing:
			pong, err := peer.send.Encrypt(tunnel.Header{
				Type: tunnel.FramePong,
				Dst:  peer.index,
				Src:  selfIndex,
			}, nil)
			if err == nil {
				_ = client.Send(pong)
			}
		case tunnel.FramePong:
			now := time.Now().UnixNano()
			sent := int64(0)
			if len(payload) >= 8 {
				sent = int64(binary.BigEndian.Uint64(payload[:8]))
			}
			if sent <= 0 {
				// Старые клиенты отвечают пустым pong: берём время отправки ping.
				sent = peer.pingSent.Load()
			}
			if sent > 0 {
				peer.setRTT(time.Duration(now - sent))
			}
		default:
			if err := device.WritePacket(payload); err != nil {
				log.Printf("запись в адаптер: %v", err)
			}
		}
	}
}

// pingLoop раз в 3 секунды проверяет каждого пира через туннель.
// Результат — задержка для индикатора в интерфейсе.
func pingLoop(ctx context.Context, client *tunnel.Client, peers *peerSet, selfIndex uint32) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		var payload [8]byte
		binary.BigEndian.PutUint64(payload[:], uint64(time.Now().UnixNano()))
		for _, peer := range peers.all() {
			peer.pingSent.Store(time.Now().UnixNano())
			frame, err := peer.send.Encrypt(tunnel.Header{
				Type: tunnel.FramePing,
				Dst:  peer.index,
				Src:  selfIndex,
			}, payload[:])
			if err != nil {
				continue
			}
			_ = client.Send(frame)
		}
	}
}

// heartbeatLoop раз в 30 секунд сообщает серверу, что узел жив, и обновляет
// карту сети, когда меняется её версия.
func heartbeatLoop(ctx context.Context, opt tunnelOptions, peers *peerSet) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	version := opt.NetMap.Version
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var beat engine.HeartbeatResponse
			if err := call("POST", opt.Server+"/api/v1/heartbeat", opt.Token, engine.HeartbeatRequest{}, &beat); err != nil {
				log.Printf("heartbeat: %v", err)
				continue
			}
			if beat.NetMapVersion == version {
				continue
			}

			var netmap engine.NetMap
			if err := call("GET", opt.Server+"/api/v1/netmap", opt.Token, nil, &netmap); err != nil {
				log.Printf("обновление карты сети: %v", err)
				continue
			}
			if err := peers.rebuild(netmap, opt.PrivateKey); err != nil {
				log.Printf("карта сети: %v", err)
				continue
			}
			version = netmap.Version
			log.Printf("карта сети обновлена: версия %d, участников %d", netmap.Version, len(netmap.Peers))
		}
	}
}

// parsePacketDestination достаёт адрес назначения из IP-пакета.
func parsePacketDestination(packet []byte) (netip.Addr, tunnel.FrameType, bool) {
	if len(packet) < 1 {
		return netip.Addr{}, 0, false
	}
	switch packet[0] >> 4 {
	case 4:
		if len(packet) < 20 {
			return netip.Addr{}, 0, false
		}
		return netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]}), tunnel.FrameIPv4, true
	case 6:
		if len(packet) < 40 {
			return netip.Addr{}, 0, false
		}
		var raw [16]byte
		copy(raw[:], packet[24:40])
		return netip.AddrFrom16(raw), tunnel.FrameIPv6, true
	default:
		return netip.Addr{}, 0, false
	}
}

// isBroadcast отвечает, надо ли разослать пакет всем участникам:
// ограниченный бродкаст, широковещательный адрес подсети или multicast.
func isBroadcast(dst netip.Addr, subnet netip.Prefix) bool {
	if dst.IsMulticast() {
		return true
	}
	if !dst.Is4() {
		return false
	}
	if dst == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return true
	}
	if subnet.IsValid() && subnet.Contains(dst) {
		return dst == broadcastOf(subnet)
	}
	return false
}

// broadcastOf считает широковещательный адрес подсети.
func broadcastOf(prefix netip.Prefix) netip.Addr {
	addr := prefix.Masked().Addr()
	hostBits := addr.BitLen() - prefix.Bits()
	if addr.Is4() {
		raw := addr.As4()
		for i := 0; i < hostBits; i++ {
			raw[3-i/8] |= 1 << (i % 8)
		}
		return netip.AddrFrom4(raw)
	}
	raw := addr.As16()
	for i := 0; i < hostBits; i++ {
		raw[15-i/8] |= 1 << (i % 8)
	}
	return netip.AddrFrom16(raw)
}
