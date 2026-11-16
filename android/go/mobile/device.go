package mobile

import (
	"fmt"
	"os"
	"syscall"
)

// packetDevice — виртуальный адаптер глазами туннеля. Тот же интерфейс, что
// в posvpn-client: логика сессии не зависит от способа получения пакетов.
type packetDevice interface {
	ReadPacket() ([]byte, error)
	WritePacket(packet []byte) error
	MTU() int
	Close() error
}

// fdDevice — адаптер поверх файлового дескриптора, который выдал
// VpnService.establish() на стороне Java. Android-дескриптор TUN в
// блокирующем режиме: read() возвращает один IP-пакет целиком.
type fdDevice struct {
	f   *os.File
	mtu int
}

// newFdDevice оборачивает готовый дескриптор. Владение дескриптором
// переходит к устройству: Close закроет его.
func newFdDevice(fd, mtu int) (*fdDevice, error) {
	if fd < 0 {
		return nil, fmt.Errorf("некорректный дескриптор TUN: %d", fd)
	}
	if mtu <= 0 {
		mtu = 1100
	}
	return &fdDevice{
		f:   os.NewFile(uintptr(fd), "posvpn-tun"),
		mtu: mtu,
	}, nil
}

// ReadPacket читает один IP-пакет из TUN. Блокируется до появления пакета;
// при закрытии дескриптора возвращает ошибку и завершает sendLoop.
func (d *fdDevice) ReadPacket() ([]byte, error) {
	buf := make([]byte, d.mtu+512) // запас на заголовки без фрагментации
	n, err := d.f.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	if err != nil {
		return nil, err
	}
	return nil, syscall.EIO
}

// WritePacket отправляет IP-пакет в TUN — как если бы он пришёл из сети.
func (d *fdDevice) WritePacket(packet []byte) error {
	if len(packet) == 0 {
		return nil
	}
	if len(packet) > d.mtu+512 {
		return fmt.Errorf("пакет %d байт больше MTU+запаса", len(packet))
	}
	_, err := d.f.Write(packet)
	return err
}

// MTU возвращает расчётный MTU адаптера.
func (d *fdDevice) MTU() int { return d.mtu }

// Close закрывает дескриптор TUN. Повторный вызов безопасен.
func (d *fdDevice) Close() error {
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}
