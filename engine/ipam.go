package engine

import (
	"errors"
	"fmt"
	"net/netip"
)

// ErrNoFreeIP возвращается, когда в подсети не осталось свободных адресов.
var ErrNoFreeIP = errors.New("в подсети нет свободных адресов")

// NetworkSubnet строит подсеть сети по её порядковому номеру: 10.77.<index>.0/24.
// Индексы 1..254 — по одному /24 на сеть внутри выделенного /16.
func NetworkSubnet(index int) (string, error) {
	if index < 1 || index > 254 {
		return "", fmt.Errorf("индекс сети %d вне диапазона 1..254", index)
	}
	return fmt.Sprintf("%s.%d.0/24", DefaultSubnetPrefix, index), nil
}

// HostIP возвращает адрес узла с номером host в подсети.
// host = 1 — первый адрес после адреса сети; адрес сети и широковещательный
// адрес не выдаются.
func HostIP(subnet string, host int) (string, error) {
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil {
		return "", fmt.Errorf("некорректная подсеть %q: %w", subnet, err)
	}
	if host < 1 {
		return "", fmt.Errorf("номер узла %d должен быть не меньше 1", host)
	}
	addr := prefix.Masked().Addr()
	for i := 0; i < host; i++ {
		addr = addr.Next()
	}
	if !prefix.Contains(addr) || addr == lastAddr(prefix) {
		return "", fmt.Errorf("адрес %s вне допустимого диапазона подсети %s", addr, subnet)
	}
	return addr.String(), nil
}

// lastAddr возвращает широковещательный адрес подсети.
func lastAddr(prefix netip.Prefix) netip.Addr {
	addr := prefix.Masked().Addr()
	hostBits := addr.BitLen() - prefix.Bits()
	if addr.Is4() {
		b := addr.As4()
		for i := 0; i < hostBits; i++ {
			b[3-i/8] |= 1 << (i % 8)
		}
		return netip.AddrFrom4(b)
	}
	b := addr.As16()
	for i := 0; i < hostBits; i++ {
		b[15-i/8] |= 1 << (i % 8)
	}
	return netip.AddrFrom16(b)
}

// NextFreeIP ищет первый свободный адрес в подсети, начиная с .1.
// Адрес сети и широковещательный адрес не выдаются.
func NextFreeIP(subnet string, used map[string]struct{}) (string, error) {
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil {
		return "", fmt.Errorf("некорректная подсеть %q: %w", subnet, err)
	}
	addr := prefix.Masked().Addr()
	for i := 1; i <= 254; i++ {
		addr = addr.Next()
		if !prefix.Contains(addr) {
			break
		}
		if _, busy := used[addr.String()]; busy {
			continue
		}
		return addr.String(), nil
	}
	return "", ErrNoFreeIP
}
