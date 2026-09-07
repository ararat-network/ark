package tlsconfig

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	tlstestutil "github.com/ararat-network/ark/pkg/tlsconfig/testutil"
)

type safeLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *safeLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestMaterialMonitorReportsFailureAndRecovery(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	cert, key := ca.Issue(t, "original", "localhost")
	m, err := (Server{Mode: TLS, CertFile: cert, KeyFile: key}).Load()
	require.NoError(t, err)
	output := &safeLog{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.run(ctx, log.NewLogger(output, log.ColorOption(false)), 5*time.Millisecond)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("certificate loop did not stop")
		}
	})
	require.Eventually(t, func() bool { return bytes.Contains([]byte(output.String()), []byte("expires soon")) }, time.Second, time.Millisecond)
	require.NoError(t, os.WriteFile(cert, []byte("partial"), 0o600))
	require.Eventually(t, func() bool { return bytes.Contains([]byte(output.String()), []byte("reload failed")) }, time.Second, time.Millisecond)
	// Metadata does not matter; the same files recover when a complete pair arrives.
	nextCert, nextKey := ca.Issue(t, "next", "localhost")
	bz, err := os.ReadFile(nextCert)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cert, bz, 0o600)) //nolint:gosec // cert is issued under t.TempDir().
	bz, err = os.ReadFile(nextKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(key, bz, 0o600)) //nolint:gosec // key is issued under t.TempDir().
	require.Eventually(t, func() bool { return bytes.Contains([]byte(output.String()), []byte("reload recovered")) }, time.Second, time.Millisecond)
	cancel()
	<-done
	require.Contains(t, output.String(), "certificate rotated")
	require.Equal(t, 1, bytes.Count([]byte(output.String()), []byte("reload failed")))
	require.Equal(t, "next", m.pair.cert.Load().Leaf.Subject.CommonName)
}

func TestMaterialStartStopsAndHandlesNoIdentity(t *testing.T) {
	ca := tlstestutil.NewAuthority(t)
	cert, key := ca.Issue(t, "server", "localhost")
	withPair, err := (Server{Mode: TLS, CertFile: cert, KeyFile: key}).Load()
	require.NoError(t, err)
	withoutPair, err := (Client{Mode: TLS}).Load()
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		m    *Material
	}{{"certificate", withPair}, {"server authentication only", withoutPair}, {"disabled", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			stop := tc.m.Start(ctx, log.NewNopLogger())
			cancel()
			done := make(chan struct{})
			go func() { stop(); stop(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("certificate owner did not stop")
			}
		})
	}
}
