package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// ErrRejected — хаб отказал в подключении (неверный токен, узел заблокирован).
var ErrRejected = errors.New("хаб отказал в подключении")

// Client — подключение узла к хабу. Кадры ходят дейтаграммами QUIC,
// поэтому потеря одного кадра не блокирует остальные.
type Client struct {
	conn   *quic.Conn
	sendMu sync.Mutex
}

// Dial подключается к хабу и проходит рукопожатие по токену.
func Dial(ctx context.Context, addr, token string, tlsConf *tls.Config) (*Client, error) {
	if token == "" {
		return nil, errors.New("пустой токен узла")
	}
	conn, err := quic.DialAddr(ctx, addr, tlsConf, &quic.Config{
		EnableDatagrams:      true,
		MaxIdleTimeout:       60 * time.Second,
		KeepAlivePeriod:      25 * time.Second,
		HandshakeIdleTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("подключение к хабу %s: %w", addr, err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "")
		return nil, fmt.Errorf("поток рукопожатия: %w", err)
	}
	_ = stream.SetDeadline(time.Now().Add(10 * time.Second))

	if err := json.NewEncoder(stream).Encode(helloRequest{Version: handshakeVersion, Token: token}); err != nil {
		_ = conn.CloseWithError(0, "")
		return nil, fmt.Errorf("отправка рукопожатия: %w", err)
	}

	var resp helloResponse
	if err := json.NewDecoder(io.LimitReader(stream, 4096)).Decode(&resp); err != nil {
		_ = conn.CloseWithError(0, "")
		return nil, fmt.Errorf("ответ хаба: %w", err)
	}
	_ = stream.Close()

	if !resp.OK {
		_ = conn.CloseWithError(0, "")
		if resp.Error == "" {
			resp.Error = "причина не указана"
		}
		return nil, fmt.Errorf("%w: %s", ErrRejected, resp.Error)
	}
	return &Client{conn: conn}, nil
}

// Send отправляет готовый кадр в хаб.
func (c *Client) Send(frame []byte) error {
	if len(frame) > MaxFrameSize {
		return fmt.Errorf("%w: %d байт", ErrFrameTooLarge, len(frame))
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.conn.SendDatagram(frame); err != nil {
		return fmt.Errorf("отправка кадра: %w", err)
	}
	return nil
}

// Receive ждёт следующий кадр от хаба.
func (c *Client) Receive(ctx context.Context) ([]byte, error) {
	frame, err := c.conn.ReceiveDatagram(ctx)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// Close закрывает соединение.
func (c *Client) Close() error {
	return c.conn.CloseWithError(0, "клиент закрыт")
}

// RemoteAddr — адрес хаба, LocalAddr — локальный адрес соединения.
func (c *Client) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *Client) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
