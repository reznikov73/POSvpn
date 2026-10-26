package tunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// ALPN — протокол QUIC-хаба. Клиент и сервер обязаны совпасть, иначе
// рукопожатие не состоится.
const ALPN = "posvpn-hub/1"

// SelfSignedCertificate выпускает самоподписанный сертификат для хаба.
// Для MVP этого достаточно: трафик узлов всё равно шифруется отдельно,
// а от подмены сервера защищает pinning отпечатка (см. ClientTLSConfig).
func SelfSignedCertificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("ключ сертификата: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("серийный номер: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "POSvpn hub", Organization: []string{"POSvpn"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"posvpn", "localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("сертификат: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// ServerTLSConfig собирает TLS-настройки хаба.
func ServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{ALPN},
		MinVersion:   tls.VersionTLS13,
	}
}

// ClientTLSConfig собирает TLS-настройки клиента.
//
// pin — SHA-256 отпечаток сертификата хаба в hex (двоеточия и пробелы
// игнорируются). Пустой pin означает «доверять любому»: это допустимо только
// для MVP и локальной отладки, потому что не защищает от подмены сервера.
func ClientTLSConfig(pin string) *tls.Config {
	cfg := &tls.Config{
		// Проверку цепочки заменяет pinning: сертификат самоподписанный.
		InsecureSkipVerify: true,
		NextProtos:         []string{ALPN},
		MinVersion:         tls.VersionTLS13,
	}
	if pin != "" {
		want := normalizePin(pin)
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("хаб не прислал сертификат")
			}
			got := Fingerprint(rawCerts[0])
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				return fmt.Errorf("отпечаток хаба не совпал: получен %s", got)
			}
			return nil
		}
	}
	return cfg
}

// Fingerprint считает SHA-256 отпечаток DER-сертификата в hex.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

func normalizePin(pin string) string {
	pin = strings.ToLower(strings.TrimSpace(pin))
	pin = strings.ReplaceAll(pin, ":", "")
	pin = strings.ReplaceAll(pin, " ", "")
	return pin
}
