package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// echoServer поднимает TCP-эхо и возвращает его адрес.
func echoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return listener.Addr().String()
}

func startProxy(t *testing.T, cfg Config) *Proxy {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	proxy, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	return proxy
}

// dialSocks устанавливает соединение через прокси до target (host:port).
func dialSocks(t *testing.T, proxyAddr, target string, user, pass string) (net.Conn, []byte) {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	methods := []byte{methodNone}
	if user != "" {
		methods = []byte{methodNone, methodPassword}
	}
	if _, err := conn.Write(append([]byte{version5, byte(len(methods))}, methods...)); err != nil {
		t.Fatal(err)
	}
	choice := make([]byte, 2)
	if _, err := io.ReadFull(conn, choice); err != nil {
		t.Fatal(err)
	}
	if choice[0] != version5 {
		t.Fatalf("версия ответа %d", choice[0])
	}

	if choice[1] == methodPassword {
		req := []byte{authVer, byte(len(user))}
		req = append(req, user...)
		req = append(req, byte(len(pass)))
		req = append(req, pass...)
		if _, err := conn.Write(req); err != nil {
			t.Fatal(err)
		}
		status := make([]byte, 2)
		if _, err := io.ReadFull(conn, status); err != nil {
			t.Fatal(err)
		}
		return conn, status
	}

	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}

	req := []byte{version5, cmdConnect, 0x00}
	ip := net.ParseIP(host)
	if ip == nil {
		req = append(req, atypDomain, byte(len(host)))
		req = append(req, host...)
	} else if v4 := ip.To4(); v4 != nil {
		req = append(req, atypIPv4)
		req = append(req, v4...)
	} else {
		req = append(req, atypIPv6)
		req = append(req, ip.To16()...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	return conn, reply
}

// TestProxyRelaysTraffic — основной путь: CONNECT и обратное эхо.
func TestProxyRelaysTraffic(t *testing.T) {
	echo := echoServer(t)
	proxy := startProxy(t, Config{})

	conn, reply := dialSocks(t, proxy.Addr(), echo, "", "")
	defer conn.Close()
	if reply[1] != repSuccess {
		t.Fatalf("код ответа %d, ожидался 0", reply[1])
	}

	payload := []byte("привет через прокси")
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("вернулось %q, ожидалось %q", got, payload)
	}
}

// TestProxyRequiresPassword — без верной пары соединение не устанавливается.
func TestProxyRequiresPassword(t *testing.T) {
	echo := echoServer(t)
	proxy := startProxy(t, Config{Username: "user", Password: "pass"})

	conn, status := dialSocks(t, proxy.Addr(), echo, "user", "неверный")
	defer conn.Close()
	if status[1] == 0 {
		t.Fatal("неверный пароль принят")
	}

	conn2, reply := dialSocks(t, proxy.Addr(), echo, "user", "pass")
	defer conn2.Close()
	if reply[1] != repSuccess {
		t.Fatalf("верная пара отвергнута: код %d", reply[1])
	}
}

// TestProxyRejectsForeignSource — клиент не из сети не обслуживается.
func TestProxyRejectsForeignSource(t *testing.T) {
	echo := echoServer(t)
	proxy := startProxy(t, Config{
		Allow: func(addr netip.Addr) bool { return addr == netip.MustParseAddr("10.77.1.9") },
	})

	conn, err := net.Dial("tcp", proxy.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write([]byte{version5, 1, methodNone}); err != nil {
		// Соединение могло быть закрыто сразу — это и есть ожидаемое поведение.
		return
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err == nil {
		t.Fatal("прокси ответил клиенту не из сети")
	}
	_ = echo
}

// TestProxyRejectsOtherCommands — BIND и UDP не поддерживаются.
func TestProxyRejectsOtherCommands(t *testing.T) {
	proxy := startProxy(t, Config{})

	conn, err := net.Dial("tcp", proxy.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write([]byte{version5, 1, methodNone}); err != nil {
		t.Fatal(err)
	}
	choice := make([]byte, 2)
	if _, err := io.ReadFull(conn, choice); err != nil {
		t.Fatal(err)
	}

	// Команда BIND (0x02) с адресом 127.0.0.1:80.
	req := []byte{version5, 0x02, 0x00, atypIPv4, 127, 0, 0, 1, 0, 80}
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != repCmdNotSupport {
		t.Fatalf("код %d, ожидался %d", reply[1], repCmdNotSupport)
	}
}

// TestProxyResolvesDomain — имя домена разрешает шлюз, а не клиент.
func TestProxyResolvesDomain(t *testing.T) {
	echo := echoServer(t)
	proxy := startProxy(t, Config{})

	_, port, err := net.SplitHostPort(echo)
	if err != nil {
		t.Fatal(err)
	}
	conn, reply := dialSocks(t, proxy.Addr(), "localhost:"+port, "", "")
	defer conn.Close()
	if reply[1] != repSuccess {
		t.Fatalf("localhost не разрешён: код %d", reply[1])
	}
}

// TestProxyReportsRefused — недоступный порт даёт код отказа, а не зависание.
func TestProxyReportsRefused(t *testing.T) {
	proxy := startProxy(t, Config{DialTimeout: time.Second})

	// Порт, который наверняка закрыт: слушаем и сразу закрываем.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	conn, reply := dialSocks(t, proxy.Addr(), addr, "", "")
	defer conn.Close()
	if reply[1] == repSuccess {
		t.Fatal("прокси сообщил об успехе для закрытого порта")
	}
}

// TestCloseStopsAccept — после Close порт освобождается.
func TestCloseStopsAccept(t *testing.T) {
	proxy := startProxy(t, Config{})
	addr := proxy.Addr()
	if err := proxy.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("закрытие: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("после закрытия порт продолжает принимать")
	}
}
