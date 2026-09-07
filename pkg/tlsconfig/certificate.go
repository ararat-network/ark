package tlsconfig

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// keyPair serialises reloads and publishes immutable certificates to handshakes.
// Fingerprints describe the last accepted contents, never a rejected rotation.
type keyPair struct {
	certFile, keyFile string
	usage             x509.ExtKeyUsage
	mu                sync.Mutex
	certHash, keyHash [sha256.Size]byte
	cert              atomic.Pointer[tls.Certificate]
}

func loadKeyPair(certFile, keyFile string, usage x509.ExtKeyUsage) (*keyPair, error) {
	p := &keyPair{certFile: strings.TrimSpace(certFile), keyFile: strings.TrimSpace(keyFile), usage: usage}
	if _, err := p.reload(time.Now()); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *keyPair) reload(now time.Time) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	certPEM, err := os.ReadFile(p.certFile)
	if err != nil {
		return false, fmt.Errorf("read certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(p.keyFile)
	if err != nil {
		return false, fmt.Errorf("read key: %w", err)
	}
	certHash, keyHash := sha256.Sum256(certPEM), sha256.Sum256(keyPEM)
	if p.cert.Load() != nil && certHash == p.certHash && keyHash == p.keyHash {
		return false, nil
	}
	certPEM, err = normaliseCertificatePEM(certPEM)
	if err != nil {
		return false, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return false, err
	}
	if err := validateCertificate(&cert, p.usage, now); err != nil {
		return false, err
	}
	p.certHash, p.keyHash = certHash, keyHash
	p.cert.Store(&cert)
	return true, nil
}

// X509KeyPair tolerates incomplete or malformed PEM blocks after a valid leaf.
// Require complete certificate input so an incomplete intermediate cannot be
// silently discarded from a replacement chain. Re-encode accepted blocks
// because X509KeyPair can also skip a block whose BEGIN line is indented.
func normaliseCertificatePEM(data []byte) ([]byte, error) {
	const begin = "-----BEGIN CERTIFICATE-----"
	var normalised []byte
	for data = bytes.TrimSpace(data); len(data) > 0; data = bytes.TrimSpace(data) {
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || !bytes.HasPrefix(data, []byte(begin)) {
			return nil, fmt.Errorf("certificate file must contain only complete certificate PEM blocks")
		}
		// pem.Decode can skip a malformed block and return a later valid one.
		consumed := data[:len(data)-len(rest)]
		if bytes.Count(consumed, []byte("-----BEGIN ")) != 1 {
			return nil, fmt.Errorf("certificate file contains a malformed PEM block")
		}
		normalised = append(normalised, pem.EncodeToMemory(block)...)
		data = rest
	}
	return normalised, nil
}

// validateCertificate checks what can be proved locally. The remote peer still
// checks trust and identity during the handshake. Unrestricted EKUs are valid.
func validateCertificate(cert *tls.Certificate, usage x509.ExtKeyUsage, now time.Time) error {
	for i, der := range cert.Certificate {
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			return err
		}
		if now.Before(parsed.NotBefore) {
			return fmt.Errorf("certificate %d is not valid before %s", i, parsed.NotBefore)
		}
		if !now.Before(parsed.NotAfter) {
			return fmt.Errorf("certificate %d expired at %s", i, parsed.NotAfter)
		}
		if len(parsed.ExtKeyUsage) != 0 || len(parsed.UnknownExtKeyUsage) != 0 {
			allowed := false
			for _, eku := range parsed.ExtKeyUsage {
				if eku == usage || eku == x509.ExtKeyUsageAny {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("certificate %d does not permit TLS role %d", i, usage)
			}
		}
		if i == 0 {
			cert.Leaf = parsed
			if usage == x509.ExtKeyUsageServerAuth && len(parsed.DNSNames) == 0 && len(parsed.IPAddresses) == 0 {
				return fmt.Errorf("server certificate requires a DNS or IP subject alternative name")
			}
		}
	}
	return nil
}
