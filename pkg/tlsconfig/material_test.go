package tlsconfig_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/tlsconfig"
	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
)

func TestMaterialRejectsIncompleteChain(t *testing.T) {
	for _, tc := range []struct {
		name        string
		replacement func(leaf, intermediate []byte) []byte
	}{
		{
			name: "truncated intermediate",
			replacement: func(leaf, intermediate []byte) []byte {
				return append(leaf, intermediate[:len(intermediate)/2]...)
			},
		},
		{
			name: "malformed block before complete intermediate",
			replacement: func(leaf, intermediate []byte) []byte {
				return append(append(leaf, []byte("-----BEGIN CERTIFICATE-----\ninvalid\n")...), intermediate...)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tlstestutil.NewAuthority(t)
			intermediate := root.Intermediate(t)
			certPath, keyPath := intermediate.Issue(t, "server", "localhost")
			leafPEM, err := os.ReadFile(certPath)
			require.NoError(t, err)
			intermediatePEM, err := os.ReadFile(intermediate.CAFile)
			require.NoError(t, err)
			fullChain := append(bytes.Clone(leafPEM), intermediatePEM...)
			require.NoError(t, os.WriteFile(certPath, fullChain, 0o600)) //nolint:gosec // certPath is issued under t.TempDir().
			files := tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certPath, KeyFile: keyPath}
			material, err := files.Load()
			require.NoError(t, err)
			original, err := material.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Len(t, original.Certificate, 2)

			// The peer trusts the root, so the intermediate must remain available.
			rootPEM, err := os.ReadFile(root.CAFile)
			require.NoError(t, err)
			roots := x509.NewCertPool()
			require.True(t, roots.AppendCertsFromPEM(rootPEM))
			verify := func(cert *tls.Certificate) error {
				intermediates := x509.NewCertPool()
				for _, der := range cert.Certificate[1:] {
					parsed, err := x509.ParseCertificate(der)
					require.NoError(t, err)
					intermediates.AddCert(parsed)
				}
				_, err := cert.Leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: "localhost"})
				return err
			}
			require.NoError(t, verify(original))
			require.NoError(t, os.WriteFile(certPath, tc.replacement(bytes.Clone(leafPEM), intermediatePEM), 0o600))
			changed, err := material.Reload()
			require.ErrorContains(t, err, "PEM block")
			require.False(t, changed)
			retained, err := material.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Same(t, original, retained)
			require.NoError(t, verify(retained))
			_, err = files.Load()
			require.ErrorContains(t, err, "PEM block")

			// Complete PEM with CRLF and surrounding whitespace is still accepted.
			complete := append([]byte(" \n\t"), bytes.ReplaceAll(fullChain, []byte("\n"), []byte("\r\n"))...)
			complete = append(complete, []byte(" \n\t")...)
			require.NoError(t, os.WriteFile(certPath, complete, 0o600)) //nolint:gosec // certPath is issued under t.TempDir().
			changed, err = material.Reload()
			require.NoError(t, err)
			require.True(t, changed)
			rotated, err := material.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Len(t, rotated.Certificate, 2)
			require.NoError(t, verify(rotated))
		})
	}
}

func TestMaterialRejectsInvalidReplacement(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*x509.Certificate)
		corrupt  bool
		mismatch bool
	}{
		{name: "expired", mutate: func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) }},
		{name: "not yet valid", mutate: func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Hour) }},
		{name: "wrong role", mutate: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} }},
		{name: "missing server SAN", mutate: func(c *x509.Certificate) { c.DNSNames = nil; c.IPAddresses = nil }},
		{name: "half-written certificate", corrupt: true},
		{name: "mismatched key", mismatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ca := tlstestutil.NewAuthority(t)
			certPath, keyPath := ca.Issue(t, "original", "localhost")
			m, err := (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certPath, KeyFile: keyPath}).Load()
			require.NoError(t, err)
			original, err := m.Config.GetCertificate(nil)
			require.NoError(t, err)
			nextCert, nextKey := ca.IssueWith(t, "replacement", tc.mutate, "localhost")
			copyFileAt(t, certPath, nextCert, time.Now())
			if !tc.mismatch {
				copyFileAt(t, keyPath, nextKey, time.Now())
			}
			if tc.corrupt {
				require.NoError(t, os.WriteFile(certPath, []byte("partial"), 0o600))
			}
			changed, err := m.Reload()
			require.Error(t, err)
			require.False(t, changed)
			served, err := m.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Same(t, original, served)
			// The same rejection must apply at startup, where no fallback is available.
			_, err = (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certPath, KeyFile: keyPath}).Load()
			require.Error(t, err)
			fixedCert, fixedKey := ca.Issue(t, "fixed", "localhost")
			copyFileAt(t, certPath, fixedCert, time.Now())
			copyFileAt(t, keyPath, fixedKey, time.Now())
			changed, err = m.Reload()
			require.NoError(t, err)
			require.True(t, changed)
			served, err = m.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Equal(t, "fixed", served.Leaf.Subject.CommonName)
		})
	}
}

func TestMaterialDetectsContentsAndSymlinkRotation(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "preserved timestamps"
		if symlink {
			name = "directory symlink swap"
		}
		t.Run(name, func(t *testing.T) {
			ca := tlstestutil.NewAuthority(t)
			cert, key := ca.Issue(t, "original", "localhost")
			nextCert, nextKey := ca.Issue(t, "next", "localhost")
			at := time.Now().Add(-time.Hour)
			require.NoError(t, os.Chtimes(cert, at, at))
			require.NoError(t, os.Chtimes(key, at, at))
			certPath, keyPath := cert, key
			var link string
			if symlink {
				dir := t.TempDir()
				first := filepath.Join(dir, "first")
				second := filepath.Join(dir, "second")
				require.NoError(t, os.Mkdir(first, 0o700))
				require.NoError(t, os.Mkdir(second, 0o700))
				copyFileAt(t, filepath.Join(first, "cert"), cert, at)
				copyFileAt(t, filepath.Join(first, "key"), key, at)
				copyFileAt(t, filepath.Join(second, "cert"), nextCert, at)
				copyFileAt(t, filepath.Join(second, "key"), nextKey, at)
				link = filepath.Join(dir, "current")
				require.NoError(t, os.Symlink(first, link))
				certPath, keyPath = filepath.Join(link, "cert"), filepath.Join(link, "key")
			}
			m, err := (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: certPath, KeyFile: keyPath}).Load()
			require.NoError(t, err)
			if symlink {
				temporary := link + ".next"
				require.NoError(t, os.Symlink(filepath.Join(filepath.Dir(link), "second"), temporary))
				require.NoError(t, os.Rename(temporary, link))
			} else {
				copyFileAt(t, certPath, nextCert, at)
				copyFileAt(t, keyPath, nextKey, at)
			}
			changed, err := m.Reload()
			require.NoError(t, err)
			require.True(t, changed)
			certNow, err := m.Config.GetCertificate(nil)
			require.NoError(t, err)
			require.Equal(t, "next", certNow.Leaf.Subject.CommonName)
			changed, err = m.Reload()
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}

func TestMaterialCallbacksOnlyReadPublishedCertificate(t *testing.T) {
	for _, client := range []bool{false, true} {
		name := "server"
		if client {
			name = "client"
		}
		t.Run(name, func(t *testing.T) {
			ca := tlstestutil.NewAuthority(t)
			cert, key := ca.Issue(t, "identity", "localhost")
			var m *tlsconfig.Material
			var err error
			var current func() (*tls.Certificate, error)
			if client {
				m, err = (tlsconfig.Client{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
				require.NoError(t, err)
				current = func() (*tls.Certificate, error) { return m.Config.GetClientCertificate(nil) }
			} else {
				m, err = (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
				require.NoError(t, err)
				current = func() (*tls.Certificate, error) { return m.Config.GetCertificate(nil) }
			}
			require.NoError(t, os.Remove(cert))
			require.NoError(t, os.Remove(key))
			served, err := current()
			require.NoError(t, err)
			require.Equal(t, "identity", served.Leaf.Subject.CommonName)
			_, err = m.Reload()
			require.Error(t, err)
		})
	}
}

func TestMaterialConcurrentPublication(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	cert, key := ca.Issue(t, "original", "localhost")
	nextCert, nextKey := ca.Issue(t, "next", "localhost")
	m, err := (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 1000 {
				c, e := m.Config.GetCertificate(nil)
				if e != nil || c.Leaf == nil {
					t.Error("invalid published certificate")
				}
			}
		})
	}
	copyFileAt(t, cert, nextCert, time.Now())
	copyFileAt(t, key, nextKey, time.Now())
	_, err = m.Reload()
	require.NoError(t, err)
	wg.Wait()
}

func TestTransportModes(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		client               tlsconfig.Client
		server               tlsconfig.Server
		badClient, badServer bool
	}{
		{name: "default local"},
		{name: "local", client: tlsconfig.Client{Mode: tlsconfig.Local}, server: tlsconfig.Server{Mode: tlsconfig.Local}},
		{name: "explicit plaintext", client: tlsconfig.Client{Mode: tlsconfig.Plaintext}, server: tlsconfig.Server{Mode: tlsconfig.Plaintext}},
		{name: "system roots", client: tlsconfig.Client{Mode: tlsconfig.TLS}, server: tlsconfig.Server{Mode: tlsconfig.TLS}, badServer: true},
		{name: "unknown", client: tlsconfig.Client{Mode: "auto"}, server: tlsconfig.Server{Mode: "auto"}, badClient: true, badServer: true},
		{name: "files without TLS", client: tlsconfig.Client{CAFile: "ca.pem"}, server: tlsconfig.Server{CertFile: "cert.pem", KeyFile: "key.pem"}, badClient: true, badServer: true},
		{name: "plaintext with files", client: tlsconfig.Client{Mode: tlsconfig.Plaintext, CAFile: "ca.pem"}, server: tlsconfig.Server{Mode: tlsconfig.Plaintext, ClientCAFile: "ca.pem"}, badClient: true, badServer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.badClient, tc.client.Validate() != nil)
			require.Equal(t, tc.badServer, tc.server.Validate() != nil)
		})
	}
	t.Run("system roots are left to TLS", func(t *testing.T) {
		m, err := (tlsconfig.Client{Mode: tlsconfig.TLS, ServerName: "sidecar.example"}).Load()
		require.NoError(t, err)
		require.Nil(t, m.Config.RootCAs)
		require.Equal(t, "sidecar.example", m.Config.ServerName)
		require.False(t, m.Config.InsecureSkipVerify)
	})
	t.Run("client refuses server-only identity", func(t *testing.T) {
		ca := tlstestutil.NewAuthority(t)
		cert, key := ca.IssueWith(t, "server", func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }, "localhost")
		_, err := (tlsconfig.Client{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
		require.ErrorContains(t, err, "does not permit TLS role")
	})
	for _, usage := range [][]x509.ExtKeyUsage{nil, {x509.ExtKeyUsageAny}} {
		name := "absent EKU"
		if len(usage) > 0 {
			name = "any EKU"
		}
		t.Run(name, func(t *testing.T) {
			ca := tlstestutil.NewAuthority(t)
			cert, key := ca.IssueWith(t, "unrestricted", func(c *x509.Certificate) { c.ExtKeyUsage = usage }, "localhost")
			_, err := (tlsconfig.Client{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
			require.NoError(t, err)
			_, err = (tlsconfig.Server{Mode: tlsconfig.TLS, CertFile: cert, KeyFile: key}).Load()
			require.NoError(t, err)
		})
	}
}
