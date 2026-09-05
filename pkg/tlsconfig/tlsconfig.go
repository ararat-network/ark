// Package tlsconfig turns the TLS files an operator names into connection
// material: what a client dials with and what a server serves with. The
// rules live here, once:
//
//   - Presence is the switch. A client with a CA file dials over TLS and a
//     server with a certificate serves over it. There is no enabled flag,
//     because a flag that disagrees with the files is a config that says two
//     things.
//   - A certificate and its key are set together or not at all.
//   - A client certificate or server name means nothing without a CA to
//     verify the peer against, so both require one.
//   - The floor is TLS 1.3, so there is no cipher suite to choose.
//   - A certificate and key are re-read when they change on disk, so a
//     rotation is a file swap. A trust anchor is read once; changing one is
//     a restart.
//
// Validate checks the shape and reads nothing. Load reads the files, and
// callers load at construction so a bad file fails the start rather than the
// first connection.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Client names what a client dials with. The tags name its keys in a TOML table.
type Client struct {
	// CAFile is the PEM bundle the server's certificate must chain to. Empty
	// dials in plaintext.
	CAFile string `mapstructure:"ca_file"`
	// CertFile and KeyFile are the client's PEM certificate and key, for a
	// server that requires one.
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	// ServerName is the name the server's certificate is verified against
	// when it carries neither the dialled host nor its IP.
	ServerName string `mapstructure:"server_name"`
}

// Enabled reports whether the client dials over TLS.
func (c Client) Enabled() bool {
	return strings.TrimSpace(c.CAFile) != ""
}

// Validate checks the shape.
func (c Client) Validate() error {
	hasCert := strings.TrimSpace(c.CertFile) != ""
	hasKey := strings.TrimSpace(c.KeyFile) != ""
	if hasCert != hasKey {
		return errors.New("tls cert file and key file must be set together")
	}
	if c.Enabled() {
		return nil
	}
	if hasCert {
		return errors.New("tls cert file requires a ca file")
	}
	if strings.TrimSpace(c.ServerName) != "" {
		return errors.New("tls server name requires a ca file")
	}
	return nil
}

// Load reads the files. A plaintext client loads to nil.
func (c Client) Load() (*tls.Config, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.Enabled() {
		return nil, nil
	}
	pool, err := loadPool(c.CAFile)
	if err != nil {
		return nil, fmt.Errorf("tls ca file: %w", err)
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
		ServerName: strings.TrimSpace(c.ServerName),
	}
	if strings.TrimSpace(c.CertFile) != "" {
		pair, err := loadKeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls cert file: %w", err)
		}
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return pair.current(), nil
		}
	}
	return cfg, nil
}

// Server names what a server serves with. The tags name its keys in a TOML table.
type Server struct {
	// CertFile and KeyFile are the server's PEM certificate and key. Neither
	// serves in plaintext.
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	// ClientCAFile is the PEM bundle every client's certificate must chain
	// to. Empty accepts any client.
	ClientCAFile string `mapstructure:"client_ca_file"`
}

// Enabled reports whether the server serves over TLS.
func (s Server) Enabled() bool {
	return strings.TrimSpace(s.CertFile) != "" || strings.TrimSpace(s.KeyFile) != ""
}

// Validate checks the shape.
func (s Server) Validate() error {
	hasCert := strings.TrimSpace(s.CertFile) != ""
	hasKey := strings.TrimSpace(s.KeyFile) != ""
	if hasCert != hasKey {
		return errors.New("tls cert file and key file must be set together")
	}
	if !hasCert && strings.TrimSpace(s.ClientCAFile) != "" {
		return errors.New("tls client ca file requires a cert file")
	}
	return nil
}

// Load reads the files. A plaintext server loads to nil.
func (s Server) Load() (*tls.Config, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if !s.Enabled() {
		return nil, nil
	}
	pair, err := loadKeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("tls cert file: %w", err)
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return pair.current(), nil
		},
	}
	if strings.TrimSpace(s.ClientCAFile) != "" {
		pool, err := loadPool(s.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("tls client ca file: %w", err)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// loadPool reads a PEM bundle into a pool and refuses one that holds no
// certificate: an empty pool trusts nothing, and the handshake failure it
// produces names the peer rather than the file.
func loadPool(path string) (*x509.CertPool, error) {
	path = strings.TrimSpace(path)
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bz) {
		return nil, fmt.Errorf("no certificates in %s", path)
	}
	return pool, nil
}

// keyPair is a certificate and key on disk, re-read when either file
// changes. A re-read that fails keeps the last pair that loaded: a rotation
// written wrong, or caught half-written, must not take the link down, and
// the old certificate's expiry is what makes it visible.
type keyPair struct {
	certFile string
	keyFile  string

	mu       sync.Mutex
	cert     *tls.Certificate
	certTime time.Time
	keyTime  time.Time
}

func loadKeyPair(certFile, keyFile string) (*keyPair, error) {
	p := &keyPair{certFile: strings.TrimSpace(certFile), keyFile: strings.TrimSpace(keyFile)}
	if err := p.reload(); err != nil {
		return nil, err
	}
	return p, nil
}

// current returns the pair on disk, re-reading it when a file has changed
// since the last load.
func (p *keyPair) current() *tls.Certificate {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changed() {
		_ = p.reload() // a failed re-read keeps the last pair; see keyPair
	}
	return p.cert
}

// changed reports whether either file's modification time differs from the
// last load. A file that cannot be read reads as unchanged.
func (p *keyPair) changed() bool {
	certTime, err := modTime(p.certFile)
	if err != nil {
		return false
	}
	keyTime, err := modTime(p.keyFile)
	if err != nil {
		return false
	}
	return !certTime.Equal(p.certTime) || !keyTime.Equal(p.keyTime)
}

// reload reads both files. The modification times are recorded only with a
// pair that loaded, so a half-written rotation is retried on the next call.
func (p *keyPair) reload() error {
	certTime, err := modTime(p.certFile)
	if err != nil {
		return err
	}
	keyTime, err := modTime(p.keyFile)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(p.certFile, p.keyFile)
	if err != nil {
		return err
	}
	p.cert, p.certTime, p.keyTime = &cert, certTime, keyTime
	return nil
}

func modTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
