// Package socks5 — минимальный SOCKS5-сервер для выхода в интернет через узел
// сети POSvpn.
//
// Зачем прокси, а не маршрут по умолчанию: Windows маршрутизирует по IP,
// поэтому «весь трафик через другой ПК» требует NAT и правки маршрутов, а
// приложению — отдельного правила. SOCKS5 решает задачу иначе: настроенный
// клиент (браузер, Telegram, SSH) сам приходит на виртуальный адрес шлюза,
// а шлюз открывает соединение уже со своего физического интерфейса.
//
// Безопасность: сервер слушает все интерфейсы, но принимает только тех, кто
// пришёл из подсети сети (10.77.0.0/16) — то есть через туннель. Дополнительно
// можно требовать логин и пароль.
package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Версии и коды протокола SOCKS5 (RFC 1928, RFC 1929).
const (
	version5 = 0x05
	authVer  = 0x01

	methodNone     = 0x00
	methodPassword = 0x02
	methodNoneOK   = 0xFF

	cmdConnect = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	repSuccess       = 0x00
	repGeneralFail   = 0x01
	repNotAllowed    = 0x02
	repHostUnreach   = 0x04
	repConnRefused   = 0x05
	repCmdNotSupport = 0x07
	repAddrNotSupp   = 0x08
)

// Config — настройки прокси.
type Config struct {
	// Addr — адрес прослушивания, например 0.0.0.0:1080.
	Addr string
	// Allow — фильтр источника. nil означает «разрешить всех»: так удобно
	// в тестах, в клиенте фильтр всегда задан.
	Allow func(netip.Addr) bool
	// Username и Password включают авторизацию, если логин не пуст.
	Username string
	Password string
	// Logf — приёмник сообщений; nil означает стандартный log.
	Logf func(format string, args ...any)
	// DialTimeout — предел на установку исходящего соединения.
	DialTimeout time.Duration
	// HandshakeTimeout — предел на приветствие и запрос клиента.
	HandshakeTimeout time.Duration
	// MaxConns — предел одновременных соединений.
	MaxConns int
}

// Proxy — запущенный SOCKS5-сервер.
type Proxy struct {
	cfg      Config
	listener net.Listener
	closed   chan struct{}
	once     sync.Once
	slots    chan struct{}
	wg       sync.WaitGroup
}

// Listen запускает сервер. Возвращённый Proxy нужно закрыть.
func Listen(cfg Config) (*Proxy, error) {
	if cfg.Addr == "" {
		return nil, errors.New("socks5: не задан адрес")
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 10 * time.Second
	}
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 64
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("socks5: порт %s: %w", cfg.Addr, err)
	}

	p := &Proxy{
		cfg:      cfg,
		listener: listener,
		closed:   make(chan struct{}),
		slots:    make(chan struct{}, cfg.MaxConns),
	}
	go p.acceptLoop()
	return p, nil
}

// Addr — фактический адрес прослушивания (полезно при порте 0).
func (p *Proxy) Addr() string { return p.listener.Addr().String() }

// Close останавливает сервер и ждёт завершения активных соединений.
func (p *Proxy) Close() error {
	var err error
	p.once.Do(func() {
		close(p.closed)
		err = p.listener.Close()
	})
	p.wg.Wait()
	return err
}

func (p *Proxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			p.cfg.Logf("прокси: accept: %v", err)
			continue
		}

		if !p.sourceAllowed(conn) {
			_ = conn.Close()
			continue
		}

		select {
		case p.slots <- struct{}{}:
		default:
			// Предел одновременных соединений: отказываем сразу, чтобы не
			// копить полуоткрытые сокеты.
			_ = conn.Close()
			continue
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() { <-p.slots }()
			defer conn.Close()
			p.handle(conn)
		}()
	}
}

// sourceAllowed проверяет, что клиент пришёл из сети через туннель.
func (p *Proxy) sourceAllowed(conn net.Conn) bool {
	if p.cfg.Allow == nil {
		return true
	}
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return false
	}
	ip, ok := netip.AddrFromSlice(addr.IP)
	if !ok {
		return false
	}
	if !p.cfg.Allow(ip.Unmap()) {
		p.cfg.Logf("прокси: отказ источнику %s (не из сети)", addr.IP)
		return false
	}
	return true
}

func (p *Proxy) handle(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(p.cfg.HandshakeTimeout))

	if err := p.handshake(conn); err != nil {
		p.cfg.Logf("прокси: рукопожатие с %s: %v", conn.RemoteAddr(), err)
		return
	}

	target, err := p.readRequest(conn)
	if err != nil {
		p.cfg.Logf("прокси: запрос от %s: %v", conn.RemoteAddr(), err)
		return
	}

	dialer := net.Dialer{Timeout: p.cfg.DialTimeout}
	upstream, err := dialer.Dial("tcp", target)
	if err != nil {
		p.cfg.Logf("прокси: %s → %s: %v", conn.RemoteAddr(), target, err)
		_ = writeReply(conn, replyCode(err))
		return
	}
	defer upstream.Close()

	if err := writeReply(conn, repSuccess); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})

	p.cfg.Logf("прокси: %s → %s", conn.RemoteAddr(), target)
	relay(conn, upstream)
}

// handshake выбирает метод авторизации и при необходимости проверяет пару.
func (p *Proxy) handshake(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != version5 {
		return fmt.Errorf("версия %d, ожидалась 5", header[0])
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}

	needAuth := p.cfg.Username != ""
	want := byte(methodNone)
	if needAuth {
		want = methodPassword
	}
	ok := false
	for _, m := range methods {
		if m == want {
			ok = true
			break
		}
	}
	if !ok {
		_, _ = conn.Write([]byte{version5, methodNoneOK})
		return errors.New("клиент не предложил нужный метод авторизации")
	}
	if _, err := conn.Write([]byte{version5, want}); err != nil {
		return err
	}

	if !needAuth {
		return nil
	}
	return p.checkPassword(conn)
}

// checkPassword читает пару логин-пароль по RFC 1929.
func (p *Proxy) checkPassword(conn net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != authVer {
		return fmt.Errorf("версия авторизации %d, ожидалась 1", head[0])
	}

	user := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, user); err != nil {
		return err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(conn, plen); err != nil {
		return err
	}
	pass := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(conn, pass); err != nil {
		return err
	}

	if string(user) != p.cfg.Username || string(pass) != p.cfg.Password {
		_, _ = conn.Write([]byte{authVer, 1})
		return errors.New("неверный логин или пароль")
	}
	_, err := conn.Write([]byte{authVer, 0})
	return err
}

// readRequest разбирает запрос CONNECT и возвращает адрес назначения.
func (p *Proxy) readRequest(conn net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", err
	}
	if head[0] != version5 {
		return "", fmt.Errorf("версия %d, ожидалась 5", head[0])
	}
	if head[1] != cmdConnect {
		_ = writeReply(conn, repCmdNotSupport)
		return "", fmt.Errorf("команда %d не поддерживается (только CONNECT)", head[1])
	}

	var host string
	switch head[3] {
	case atypIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = net.IP(buf).String()
	case atypIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = net.IP(buf).String()
	case atypDomain:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		buf := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = string(buf)
	default:
		_ = writeReply(conn, repAddrNotSupp)
		return "", fmt.Errorf("тип адреса %d не поддерживается", head[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)
	if host == "" || port == 0 {
		_ = writeReply(conn, repAddrNotSupp)
		return "", errors.New("пустой адрес или порт")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// writeReply отправляет ответ по RFC 1928: IPv4 0.0.0.0:0.
func writeReply(w io.Writer, code byte) error {
	_, err := w.Write([]byte{version5, code, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// replyCode переводит ошибку соединения в код ответа SOCKS5.
func replyCode(err error) byte {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return repHostUnreach
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "refused"):
		return repConnRefused
	case strings.Contains(text, "no such host"), strings.Contains(text, "no route"):
		return repHostUnreach
	case strings.Contains(text, "permission"), strings.Contains(text, "forbidden"):
		return repNotAllowed
	default:
		return repGeneralFail
	}
}

// relay перекачивает данные в обе стороны и закрывает запись при завершении.
func relay(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		closeWrite(upstream)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
	<-done
}

// closeWrite закрывает половину соединения, чтобы вторая сторона увидела конец.
func closeWrite(conn net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = conn.Close()
}

// ContextDone закрывает прокси при отмене контекста. Удобно для туннеля:
// прокси живёт ровно столько же, сколько сеанс.
func (p *Proxy) ContextDone(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Close()
		case <-p.closed:
		}
	}()
}
