package tunnel

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/chacha20poly1305"
)

// maxEpochs — сколько последних сессий пира помнить для защиты от повтора.
// Перезапуск узла даёт новый epoch; старые вытесняются.
const maxEpochs = 4

// PairCiphers выводит два направленных ключа для пары узлов.
//
//	shared = X25519(мой приватный, публичный ключ пира)
//	k1, k2 = HKDF-SHA256(shared, salt = network_id, info = "posvpn v1 pair key N")
//
// Направление выбирается по индексам: узел с меньшим индексом шифрует ключом
// k1 и расшифровывает k2, узел с большим — наоборот. Два ключа нужны потому,
// что счётчики у направлений независимые: с одним ключом стороны выдали бы
// одинаковые nonce.
func PairCiphers(privateKey, peerPublicKey []byte, networkID string, selfIndex, peerIndex uint32) (send, recv *Cipher, err error) {
	if selfIndex == 0 {
		return nil, nil, fmt.Errorf("индекс узла не может быть 0: он зарезервирован под бродкаст")
	}
	if selfIndex == peerIndex {
		return nil, nil, fmt.Errorf("индексы узлов совпадают: %d", selfIndex)
	}

	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("приватный ключ узла: %w", err)
	}
	pub, err := curve.NewPublicKey(peerPublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("публичный ключ пира: %w", err)
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("общий секрет: %w", err)
	}

	k1, err := hkdf.Key(sha256.New, shared, []byte(networkID), "posvpn v1 pair key 1", chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("ключ направления 1: %w", err)
	}
	k2, err := hkdf.Key(sha256.New, shared, []byte(networkID), "posvpn v1 pair key 2", chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("ключ направления 2: %w", err)
	}

	send, err = NewCipher(k1)
	if err != nil {
		return nil, nil, err
	}
	recv, err = NewCipher(k2)
	if err != nil {
		return nil, nil, err
	}
	if selfIndex > peerIndex {
		send, recv = recv, send
	}
	return send, recv, nil
}

// Cipher — одно направление пары: свой счётчик, свой epoch, своё окно приёма.
// Один Cipher используют только для одной пары узлов и одного направления.
type Cipher struct {
	aead    cipher.AEAD
	epoch   uint32
	counter atomic.Uint64
	replay  replaySet
}

// NewCipher собирает шифр направления со случайным epoch.
func NewCipher(key []byte) (*Cipher, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("ChaCha20-Poly1305: %w", err)
	}
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("epoch: %w", err)
	}
	epoch := binary.BigEndian.Uint32(raw[:])
	if epoch == 0 {
		epoch = 1
	}
	return &Cipher{aead: aead, epoch: epoch}, nil
}

// Epoch возвращает идентификатор текущей сессии узла.
func (c *Cipher) Epoch() uint32 { return c.epoch }

// Encrypt собирает кадр: заголовок с очередным счётчиком и зашифрованный
// пакет. Заголовок служит дополнительными данными AEAD — его подмена
// (адресат, отправитель, тип, счётчик) ломает проверку тега.
func (c *Cipher) Encrypt(h Header, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("%w: %d байт, максимум %d", ErrFrameTooLarge, len(payload), MaxPayloadSize)
	}
	h.Version = Version1
	h.Epoch = c.epoch
	h.Counter = c.counter.Add(1)

	frame := make([]byte, HeaderSize+len(payload)+TagSize)
	h.EncodeTo(frame[:HeaderSize])
	return c.aead.Seal(frame[:HeaderSize], nonceFor(h.Epoch, h.Counter), payload, frame[:HeaderSize]), nil
}

// Decrypt проверяет заголовок, защиту от повтора и тег, затем возвращает
// открытый пакет. Кадры с уже принятым счётчиком отбрасываются.
func (c *Cipher) Decrypt(frame []byte) (Header, []byte, error) {
	h, err := ParseHeader(frame)
	if err != nil {
		return Header{}, nil, err
	}
	if len(frame) < HeaderSize+TagSize {
		return Header{}, nil, ErrShortFrame
	}
	if !c.replay.check(h.Epoch, h.Counter) {
		return Header{}, nil, ErrReplay
	}
	payload, err := c.aead.Open(nil, nonceFor(h.Epoch, h.Counter), frame[HeaderSize:], frame[:HeaderSize])
	if err != nil {
		return Header{}, nil, ErrDecrypt
	}
	c.replay.commit(h.Epoch, h.Counter)
	return h, payload, nil
}

// nonceFor собирает 96-битный nonce: epoch (4 байта) плюс счётчик (8 байт).
// Epoch случаен для каждого запуска узла, поэтому после перезапуска nonce
// не повторяются, даже если счётчик снова начинается с единицы.
func nonceFor(epoch uint32, counter uint64) []byte {
	var nonce [12]byte
	binary.BigEndian.PutUint32(nonce[0:4], epoch)
	binary.BigEndian.PutUint64(nonce[4:12], counter)
	return nonce[:]
}

// replaySet хранит окна приёма по epoch'ам пира.
type replaySet struct {
	mu     sync.Mutex
	epochs map[uint32]*replayWindow
	order  []uint32
}

func (s *replaySet) check(epoch uint32, counter uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.epochs[epoch]
	if !ok {
		// Новый epoch: пропускаем к расшифровке. Окно появится только
		// после успешной проверки тега, чтобы подделки не сдвигали его.
		return true
	}
	return w.check(counter)
}

func (s *replaySet) commit(epoch uint32, counter uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epochs == nil {
		s.epochs = make(map[uint32]*replayWindow)
	}
	w, ok := s.epochs[epoch]
	if !ok {
		w = &replayWindow{}
		s.epochs[epoch] = w
		s.order = append(s.order, epoch)
		for len(s.order) > maxEpochs {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.epochs, oldest)
		}
	}
	w.commit(counter)
}

// replayWindow — скользящее окно на 64 кадра (та же идея, что в IPsec).
type replayWindow struct {
	max  uint64
	bits uint64
}

func (w *replayWindow) check(counter uint64) bool {
	if counter == 0 {
		return false
	}
	if counter > w.max {
		return true
	}
	diff := w.max - counter
	if diff >= 64 {
		return false
	}
	return w.bits&(1<<diff) == 0
}

func (w *replayWindow) commit(counter uint64) {
	if counter > w.max {
		shift := counter - w.max
		if shift >= 64 {
			w.bits = 1
		} else {
			w.bits = w.bits<<shift | 1
		}
		w.max = counter
		return
	}
	diff := w.max - counter
	if diff < 64 {
		w.bits |= 1 << diff
	}
}
