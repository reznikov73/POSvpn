//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// packetDevice — виртуальный адаптер глазами туннеля. Отдельный интерфейс
// нужен, чтобы логику туннеля можно было проверять без драйвера.
type packetDevice interface {
	ReadPacket() ([]byte, error)
	WritePacket(packet []byte) error
	MTU() int
	Close() error
}

// wintunDevice — L3-адаптер Wintun. Пакеты читаются в отдельной горутине,
// потому что Wintun отдаёт их по событию, а не блокирующим вызовом.
type wintunDevice struct {
	adapter *wintun.Adapter
	session wintun.Session
	luid    winipcfg.LUID
	mtu     int

	packets chan []byte
	done    chan struct{}
	once    sync.Once
	readErr atomic.Pointer[error]
}

// openDevice создаёт (или открывает) адаптер, назначает адрес и маршрут.
// Нужны права администратора и wintun.dll рядом с exe.
func openDevice(name, ip string, mtu int) (packetDevice, error) {
	adapter, err := createAdapter(name)
	if err != nil {
		return nil, fmt.Errorf("адаптер %q: %w — нужны права администратора и wintun.dll рядом с exe", name, err)
	}

	session, err := adapter.StartSession(0x400000)
	if err != nil {
		_ = adapter.Close()
		return nil, fmt.Errorf("сессия адаптера: %w", err)
	}

	dev := &wintunDevice{
		adapter: adapter,
		session: session,
		luid:    winipcfg.LUID(adapter.LUID()),
		mtu:     mtu,
		packets: make(chan []byte, 256),
		done:    make(chan struct{}),
	}
	if err := dev.configure(ip, mtu); err != nil {
		session.End()
		_ = adapter.Close()
		return nil, err
	}
	go dev.readLoop()
	return dev, nil
}

// createAdapter создаёт адаптер и лечит известную поломку Wintun: после
// обновления Windows запись службы драйвера протухает, и создание падает
// с ERROR_GEN_FAILURE. Помогает только удаление службы и повтор.
func createAdapter(name string) (*wintun.Adapter, error) {
	adapter, err := wintun.CreateAdapter(name, "POSvpn", nil)
	if err == nil {
		return adapter, nil
	}
	if !errors.Is(err, windows.ERROR_GEN_FAILURE) {
		return nil, err
	}

	// sc delete wintun — требование апстрима при этой ошибке.
	_ = runHidden("sc.exe", "delete", "wintun")
	adapter, retryErr := wintun.CreateAdapter(name, "POSvpn", nil)
	if retryErr != nil {
		return nil, fmt.Errorf("%w (после пересоздания службы: %v)", err, retryErr)
	}
	return adapter, nil
}

// configure назначает адрес узла и маршрут всей сети 10.77.0.0/16.
// Маршрут по умолчанию не трогаем: next hop — 0.0.0.0, то есть on-link.
func (d *wintunDevice) configure(ip string, mtu int) error {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("адрес узла %q: %w", ip, err)
	}
	if err := d.luid.SetIPAddresses([]netip.Prefix{netip.PrefixFrom(addr, 24)}); err != nil {
		return fmt.Errorf("назначение адреса %s: %w", ip, err)
	}

	// Сразу после создания адаптера Windows ещё не готова принимать записи
	// маршрутов: CreateIpForwardEntry2 отвечает ERROR_NOT_FOUND. Ждём, пока
	// адрес станет Preferred.
	if err := waitForAddress(d.luid, addr); err != nil {
		return err
	}

	if iface, err := d.luid.IPInterface(winipcfg.AddressFamily(windows.AF_INET)); err == nil {
		iface.NLMTU = uint32(mtu)
		if err := iface.Set(); err != nil {
			return fmt.Errorf("MTU %d: %w", mtu, err)
		}
	}

	route := &winipcfg.RouteData{
		Destination: netip.MustParsePrefix("10.77.0.0/16"),
		NextHop:     netip.IPv4Unspecified(),
		Metric:      0,
	}
	return addRouteWithRetry(d.luid, route)
}

// waitForAddress ждёт, пока Windows доведёт адрес до состояния Preferred.
func waitForAddress(luid winipcfg.LUID, addr netip.Addr) error {
	deadline := time.Now().Add(3 * time.Second)
	var last error
	for {
		row, err := luid.IPAddress(addr)
		if err == nil && row != nil && row.DadState == winipcfg.DadStatePreferred {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			if last == nil {
				last = errors.New("адрес не перешёл в состояние Preferred")
			}
			return fmt.Errorf("адрес %s не активировался: %w", addr, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// addRouteWithRetry добавляет маршрут, повторяя попытку, пока интерфейс
// не будет готов. Чужие маршруты не трогаем: автоматический on-link маршрут
// подсети нужен Windows для локальной доставки.
func addRouteWithRetry(luid winipcfg.LUID, route *winipcfg.RouteData) error {
	deadline := time.Now().Add(3 * time.Second)
	var last error
	for {
		err := luid.AddRoute(route.Destination, route.NextHop, route.Metric)
		if err == nil || errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("маршрут %s: %w", route.Destination, last)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (d *wintunDevice) readLoop() {
	event := d.session.ReadWaitEvent()
	for {
		select {
		case <-d.done:
			return
		default:
		}

		packet, err := d.session.ReceivePacket()
		if err == nil {
			copied := make([]byte, len(packet))
			copy(copied, packet)
			d.session.ReleaseReceivePacket(packet)
			select {
			case d.packets <- copied:
			case <-d.done:
				return
			}
			continue
		}

		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			// Пакетов нет — ждём событие. Таймаут нужен, чтобы заметить Close.
			_, _ = windows.WaitForSingleObject(event, 200)
			continue
		}
		d.fail(fmt.Errorf("чтение из адаптера: %w", err))
		return
	}
}

// ReadPacket блокируется до пакета или закрытия устройства.
func (d *wintunDevice) ReadPacket() ([]byte, error) {
	select {
	case packet := <-d.packets:
		return packet, nil
	case <-d.done:
		if err := d.readErr.Load(); err != nil {
			return nil, *err
		}
		return nil, io.EOF
	}
}

// WritePacket отдаёт пакет в адаптер.
func (d *wintunDevice) WritePacket(packet []byte) error {
	buf, err := d.session.AllocateSendPacket(len(packet))
	if err != nil {
		return fmt.Errorf("буфер отправки: %w", err)
	}
	copy(buf, packet)
	d.session.SendPacket(buf)
	return nil
}

func (d *wintunDevice) MTU() int { return d.mtu }

// Close снимает адреса и маршруты, чтобы после выхода не осталось «висящего»
// адреса на адаптере, и закрывает сессию.
func (d *wintunDevice) Close() error {
	d.once.Do(func() {
		close(d.done)
		_ = d.luid.FlushRoutes(winipcfg.AddressFamily(windows.AF_UNSPEC))
		_ = d.luid.FlushIPAddresses(winipcfg.AddressFamily(windows.AF_UNSPEC))
		d.session.End()
		_ = d.adapter.Close()
	})
	return nil
}

func (d *wintunDevice) fail(err error) {
	d.readErr.Store(&err)
	d.once.Do(func() { close(d.done) })
}

// flushInterface снимает адреса и маршруты с существующего адаптера —
// используется командой down, когда туннель уже не запущен.
func flushInterface(name string) error {
	adapter, err := wintun.OpenAdapter(name)
	if err != nil {
		return fmt.Errorf("адаптер %q не найден: %w", name, err)
	}
	defer adapter.Close()

	luid := winipcfg.LUID(adapter.LUID())
	_ = luid.FlushRoutes(winipcfg.AddressFamily(windows.AF_UNSPEC))
	if err := luid.FlushIPAddresses(winipcfg.AddressFamily(windows.AF_UNSPEC)); err != nil {
		return fmt.Errorf("снятие адресов: %w", err)
	}
	return nil
}

// runHidden запускает консольную утилиту без мелькающего окна.
func runHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
