// Package testutil issues throwaway certificates for transport tests.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Authority is a self-signed CA whose PEM bundle is on disk at CAFile.
type Authority struct {
	CAFile string

	dir    string
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	serial int64
}

// NewAuthority creates a CA under t's temporary directory.
func NewAuthority(t *testing.T) *Authority {
	t.Helper()
	return newAuthority(t, nil)
}

// Intermediate creates a CA signed by a, for tests that need a full chain.
func (a *Authority) Intermediate(t *testing.T) *Authority {
	t.Helper()
	return newAuthority(t, a)
}

func newAuthority(t *testing.T, parent *Authority) *Authority {
	t.Helper()

	dir := t.TempDir()
	key := newKey(t)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	issuer, signer := template, key
	if parent != nil {
		parent.serial++
		template.SerialNumber = big.NewInt(parent.serial)
		template.Subject.CommonName = "test intermediate ca"
		issuer, signer = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, signer)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	caFile := filepath.Join(dir, "ca.pem")
	writePEM(t, caFile, "CERTIFICATE", der)

	return &Authority{CAFile: caFile, dir: dir, cert: cert, key: key, serial: 1}
}

// Issue signs a certificate for hosts, DNS names or IP addresses, usable for
// either side of a handshake, and returns its PEM certificate and key files.
func (a *Authority) Issue(t *testing.T, label string, hosts ...string) (certFile, keyFile string) {
	t.Helper()
	return a.IssueWith(t, label, nil, hosts...)
}

// IssueWith lets transport tests exercise certificate validity and role limits.
func (a *Authority) IssueWith(t *testing.T, label string, mutate func(*x509.Certificate), hosts ...string) (certFile, keyFile string) {
	t.Helper()

	key := newKey(t)
	a.serial++
	template := &x509.Certificate{
		SerialNumber: big.NewInt(a.serial),
		Subject:      pkix.Name{CommonName: label},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	if mutate != nil {
		mutate(template)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certFile = filepath.Join(a.dir, label+".pem")
	keyFile = filepath.Join(a.dir, label+".key")
	writePEM(t, certFile, "CERTIFICATE", der)
	writePEM(t, keyFile, "EC PRIVATE KEY", keyDER)

	return certFile, keyFile
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()

	bz := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	require.NoError(t, os.WriteFile(path, bz, 0o600))
}
