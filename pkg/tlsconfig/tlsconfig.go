// Package tlsconfig loads TLS material without starting background work.
// Transport mode is independent of trust roots. TLS uses version 1.3 or newer;
// an empty client CA file uses system roots. Owners start certificate rotation
// during their connection lifecycle. Trust bundles are fixed at load time.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// Local permits plaintext only to local endpoints, checked by the transport.
	Local = "local"
	// TLS enables authenticated TLS, with optional client authentication.
	TLS = "tls"
	// Plaintext explicitly permits unencrypted remote connections.
	Plaintext = "plaintext"
)

// ValidateMode accepts the zero value as local for existing local deployments.
func ValidateMode(mode string) error {
	switch mode {
	case "", Local, TLS, Plaintext:
		return nil
	}
	return fmt.Errorf("invalid tls mode %q; expected local, tls, or plaintext", mode)
}

// Client configures peer verification and optional client authentication.
type Client struct {
	Mode string `mapstructure:"mode"`
	// CAFile replaces system roots with this PEM bundle when set.
	CAFile     string `mapstructure:"ca_file"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	ServerName string `mapstructure:"server_name"`
}

// Enabled reports whether the client uses TLS.
func (c Client) Enabled() bool { return c.Mode == TLS }

// Validate checks mode and field combinations without reading files.
func (c Client) Validate() error {
	if err := ValidateMode(c.Mode); err != nil {
		return err
	}
	hasCert, hasKey := present(c.CertFile), present(c.KeyFile)
	if hasCert != hasKey {
		return errors.New("tls cert file and key file must be set together")
	}
	if !c.Enabled() && (hasCert || present(c.CAFile) || present(c.ServerName)) {
		return errors.New("tls files and server name require mode tls")
	}
	return nil
}

// Load validates and reads a snapshot. Call Material.Start from the connection
// owner's Run method to follow identity rotation; one-shot clients need not.
func (c Client) Load() (*Material, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	m := &Material{}
	if !c.Enabled() {
		return m, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: strings.TrimSpace(c.ServerName)}
	if present(c.CAFile) {
		pool, err := loadPool(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("tls ca file: %w", err)
		}
		cfg.RootCAs = pool
	}
	if present(c.CertFile) {
		pair, err := loadKeyPair(c.CertFile, c.KeyFile, x509.ExtKeyUsageClientAuth)
		if err != nil {
			return nil, fmt.Errorf("tls cert file: %w", err)
		}
		m.pair = pair
		cfg.GetClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert := pair.cert.Load()
			if info != nil {
				if err := info.SupportsCertificate(cert); err != nil {
					return &tls.Certificate{}, nil
				}
			}
			return cert, nil
		}
	}
	m.Config = cfg
	return m, nil
}

// Server configures a listener's identity and optional client trust bundle.
type Server struct {
	Mode     string `mapstructure:"mode"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	// ClientCAFile requires client certificates chaining to this bundle when set.
	ClientCAFile string `mapstructure:"client_ca_file"`
}

// Enabled reports whether the listener uses TLS.
func (s Server) Enabled() bool { return s.Mode == TLS }

// Validate checks mode and field combinations without reading files.
func (s Server) Validate() error {
	if err := ValidateMode(s.Mode); err != nil {
		return err
	}
	hasCert, hasKey := present(s.CertFile), present(s.KeyFile)
	if hasCert != hasKey {
		return errors.New("tls cert file and key file must be set together")
	}
	if !s.Enabled() && (hasCert || present(s.ClientCAFile)) {
		return errors.New("tls files require mode tls")
	}
	if s.Enabled() && !hasCert {
		return errors.New("tls mode requires a cert file and key file")
	}
	return nil
}

// Load reads and validates listener material without starting rotation.
func (s Server) Load() (*Material, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	m := &Material{}
	if !s.Enabled() {
		return m, nil
	}
	pair, err := loadKeyPair(s.CertFile, s.KeyFile, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, fmt.Errorf("tls cert file: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return pair.cert.Load(), nil },
	}
	if present(s.ClientCAFile) {
		pool, err := loadPool(s.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("tls client ca file: %w", err)
		}
		cfg.ClientCAs, cfg.ClientAuth = pool, tls.RequireAndVerifyClientCert
	}
	m.Config, m.pair = cfg, pair
	return m, nil
}

func present(s string) bool { return strings.TrimSpace(s) != "" }

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
