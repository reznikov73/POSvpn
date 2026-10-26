// Package tunnel — транспорт POSvpn: формат кадра, ключи пары, QUIC-хаб
// и клиентское подключение к нему.
//
// Кадр несёт зашифрованный IP-пакет. Заголовок открыт: хабу нужно знать,
// кому переслать кадр, но содержимое он не читает.
package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// Version1 — текущая версия протокола кадров.
	Version1 byte = 1

	// HeaderSize — размер заголовка:
	// версия (1) + тип (1) + dst (4) + src (4) + epoch (4) + counter (8).
	HeaderSize = 22

	// TagSize — размер тега Poly1305.
	TagSize = 16

	// MaxFrameSize — максимум кадра, который помещается в одну
	// QUIC-дейтаграмму. QUIC гарантирует MTU 1200 байт, оставляем запас.
	MaxFrameSize = 1180

	// MaxPayloadSize — максимум IP-пакета в кадре.
	MaxPayloadSize = MaxFrameSize - HeaderSize - TagSize

	// DefaultMTU — MTU виртуального адаптера. Меньше MaxPayloadSize,
	// чтобы заголовок, тег и служебные поля QUIC гарантированно помещались.
	DefaultMTU = 1100

	// BroadcastIndex — dst = 0: кадр всем участникам сети, кроме отправителя.
	BroadcastIndex uint32 = 0
)

// FrameType — тип содержимого кадра.
type FrameType uint8

const (
	// FrameIPv4 — IPv4-пакет.
	FrameIPv4 FrameType = 1
	// FrameIPv6 — IPv6-пакет.
	FrameIPv6 FrameType = 2
	// FramePing — проверка связи.
	FramePing FrameType = 3
	// FramePong — ответ на проверку связи.
	FramePong FrameType = 4
)

func (t FrameType) String() string {
	switch t {
	case FrameIPv4:
		return "ipv4"
	case FrameIPv6:
		return "ipv6"
	case FramePing:
		return "ping"
	case FramePong:
		return "pong"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(t))
	}
}

var (
	// ErrShortFrame — кадр короче заголовка или заголовка с тегом.
	ErrShortFrame = errors.New("кадр короче минимального размера")
	// ErrFrameVersion — неизвестная версия протокола.
	ErrFrameVersion = errors.New("неизвестная версия кадра")
	// ErrFrameType — неизвестный тип кадра.
	ErrFrameType = errors.New("неизвестный тип кадра")
	// ErrFrameTooLarge — пакет не помещается в кадр.
	ErrFrameTooLarge = errors.New("пакет больше допустимого размера")
	// ErrReplay — кадр уже был принят или слишком старый.
	ErrReplay = errors.New("повторный или устаревший кадр")
	// ErrDecrypt — кадр не прошёл проверку подлинности.
	ErrDecrypt = errors.New("кадр не расшифрован")
)

// Header — открытая часть кадра. Epoch меняется при каждом запуске узла,
// чтобы после перезапуска счётчики начинались заново без риска повтора nonce
// и без ложного срабатывания защиты от повтора у пира.
type Header struct {
	Version byte
	Type    FrameType
	Dst     uint32
	Src     uint32
	Epoch   uint32
	Counter uint64
}

// EncodeTo записывает заголовок в первые HeaderSize байт dst.
func (h Header) EncodeTo(dst []byte) {
	_ = dst[HeaderSize-1] // граница проверяется вызывающим
	dst[0] = h.Version
	dst[1] = byte(h.Type)
	binary.BigEndian.PutUint32(dst[2:6], h.Dst)
	binary.BigEndian.PutUint32(dst[6:10], h.Src)
	binary.BigEndian.PutUint32(dst[10:14], h.Epoch)
	binary.BigEndian.PutUint64(dst[14:22], h.Counter)
}

// ParseHeader читает и проверяет заголовок кадра.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrShortFrame
	}
	h := Header{
		Version: b[0],
		Type:    FrameType(b[1]),
		Dst:     binary.BigEndian.Uint32(b[2:6]),
		Src:     binary.BigEndian.Uint32(b[6:10]),
		Epoch:   binary.BigEndian.Uint32(b[10:14]),
		Counter: binary.BigEndian.Uint64(b[14:22]),
	}
	if h.Version != Version1 {
		return Header{}, fmt.Errorf("%w: %d", ErrFrameVersion, h.Version)
	}
	switch h.Type {
	case FrameIPv4, FrameIPv6, FramePing, FramePong:
	default:
		return Header{}, fmt.Errorf("%w: %d", ErrFrameType, h.Type)
	}
	return h, nil
}

// SetDst переписывает адресата в готовом кадре. Используется хабом при
// широковещательной рассылке.
func SetDst(frame []byte, dst uint32) {
	binary.BigEndian.PutUint32(frame[2:6], dst)
}

// SetSrc переписывает отправителя в готовом кадре. Хаб всегда ставит сюда
// индекс аутентифицированного узла.
func SetSrc(frame []byte, src uint32) {
	binary.BigEndian.PutUint32(frame[6:10], src)
}

// IsData сообщает, несёт ли кадр IP-пакет (а не служебное сообщение).
func (t FrameType) IsData() bool {
	return t == FrameIPv4 || t == FrameIPv6
}
