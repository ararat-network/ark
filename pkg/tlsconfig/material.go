package tlsconfig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"sync"
	"time"

	"cosmossdk.io/log/v2"
)

const (
	certificateReloadInterval  = time.Minute
	certificateWarningInterval = time.Hour
	certificateExpiryWarning   = 24 * time.Hour
)

// Material holds a fixed TLS policy and an optional rotating local identity.
// Config is nil for plaintext. Treat Config and callback results as immutable.
// Loading starts no goroutines; the connection owner starts and stops rotation.
type Material struct {
	Config *tls.Config
	pair   *keyPair
}

// Reload reads both identity files regardless of their metadata. A failed
// replacement leaves the current certificate in service. Trust roots never reload.
func (m *Material) Reload() (bool, error) {
	if m == nil || m.pair == nil {
		return false, nil
	}
	return m.pair.reload(time.Now())
}

// Start follows identity rotation until ctx ends or the returned stop function
// is called. Stop waits for cleanup. Call once per active connection lifecycle;
// material without a local identity starts no work. Reload errors are logged,
// never propagated as process failures.
func (m *Material) Start(ctx context.Context, logger log.Logger) func() {
	if m == nil || m.pair == nil {
		return func() {}
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.run(ctx, logger, certificateReloadInterval)
	}()
	var once sync.Once
	return func() { once.Do(cancel); <-done }
}

func (m *Material) run(ctx context.Context, logger log.Logger, interval time.Duration) {
	logger = logger.With("certificate", m.pair.certFile)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var failed bool
	var lastFailure, lastExpiry time.Time
	check := func() {
		now := time.Now()
		changed, err := m.pair.reload(now)
		if err != nil {
			if !failed || now.Sub(lastFailure) >= certificateWarningInterval {
				logger.Error("TLS certificate reload failed; retaining current certificate", "error", err)
				lastFailure = now
			}
			failed = true
		} else {
			if failed {
				logger.Info("TLS certificate reload recovered")
			}
			failed = false
			if changed {
				logger.Info("TLS certificate rotated", "expires_at", m.pair.cert.Load().Leaf.NotAfter)
				lastExpiry = time.Time{}
			}
		}
		// An unchanged identity can expire too. Warn even if no connection is made.
		cert := m.pair.cert.Load()
		expiry := cert.Leaf.NotAfter
		for _, der := range cert.Certificate[1:] {
			chainCert, parseErr := x509.ParseCertificate(der)
			if parseErr == nil && chainCert.NotAfter.Before(expiry) {
				expiry = chainCert.NotAfter
			}
		}
		if expiry.Sub(now) <= certificateExpiryWarning && (lastExpiry.IsZero() || now.Sub(lastExpiry) >= certificateWarningInterval) {
			logger.Warn("TLS certificate expires soon or has expired", "expires_at", expiry)
			lastExpiry = now
		}
	}
	if ctx.Err() != nil {
		return
	}
	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
