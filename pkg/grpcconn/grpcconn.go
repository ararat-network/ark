// Package grpcconn prepares the price feed's gRPC client transport, owns shared
// connection cleanup, and checks endpoint locality for the local-mode policy.
package grpcconn

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ararat-network/ark/pkg/tlsconfig"
)

// LoadClient checks endpoint policy and loads the client's TLS material without
// opening connections or starting certificate rotation. Long-lived callers own
// Material.Start; one-shot callers only need DialOptions.
func LoadClient(c tlsconfig.Client, targets ...string) (*tlsconfig.Material, error) {
	if err := ValidateTargets(c.Mode, targets...); err != nil {
		return nil, err
	}
	return c.Load()
}

// credentialsFor adapts already loaded TLS material to gRPC. Nil is plaintext.
func credentialsFor(cfg *tls.Config) credentials.TransportCredentials {
	if cfg == nil {
		return insecure.NewCredentials()
	}
	return credentials.NewTLS(cfg)
}

// DialOptions applies the loaded credentials and bypasses ambient proxies for
// operator-owned peers. Callers prepare material with LoadClient first.
func DialOptions(cfg *tls.Config, extra ...grpc.DialOption) []grpc.DialOption {
	opts := make([]grpc.DialOption, 0, 2+len(extra))
	opts = append(opts, grpc.WithTransportCredentials(credentialsFor(cfg)), grpc.WithNoProxy())
	return append(opts, extra...)
}

// ValidateTargets rejects non-local endpoints unless TLS or remote plaintext
// was selected explicitly. Unknown resolver schemes cannot establish locality.
func ValidateTargets(mode string, targets ...string) error {
	if err := tlsconfig.ValidateMode(mode); err != nil {
		return err
	}
	if mode == tlsconfig.TLS || mode == tlsconfig.Plaintext {
		return nil
	}
	for _, target := range targets {
		if !Loopback(target) {
			return fmt.Errorf("endpoint %q requires mode tls or explicit mode plaintext; local mode only permits local endpoints", target)
		}
	}
	return nil
}

// Open returns one connection per address, in order, dialled with opts. Each
// connection connects on its first RPC. On failure Open closes what it opened
// and returns the error.
func Open(addresses []string, opts ...grpc.DialOption) ([]*grpc.ClientConn, error) {
	conns := make([]*grpc.ClientConn, 0, len(addresses))
	for _, address := range addresses {
		conn, err := grpc.NewClient(address, opts...)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("open connection to %s: %w", address, err), Close(conns))
		}
		conns = append(conns, conn)
	}
	return conns, nil
}

// Close closes every connection and joins the failures.
func Close(conns []*grpc.ClientConn) error {
	var errs []error
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close connection to %s: %w", conn.Target(), err))
		}
	}
	return errors.Join(errs...)
}

// Loopback reports whether target names a loopback endpoint: a unix socket,
// localhost, or a loopback IP. A hostname it cannot judge is not loopback,
// and neither is a wildcard listen address. A custom resolver authority cannot
// establish locality.
func Loopback(target string) bool {
	target = strings.TrimSpace(target)
	if scheme, rest, ok := strings.Cut(target, "://"); ok {
		switch scheme {
		case "unix", "unix-abstract":
			return true
		case "dns", "passthrough":
			// scheme://authority/endpoint, with the authority usually empty.
			if authority, endpoint, found := strings.Cut(rest, "/"); found {
				if authority != "" {
					return false
				}
				target = endpoint
			} else {
				target = rest
			}
		default:
			return false
		}
	} else if strings.HasPrefix(target, "unix:") || strings.HasPrefix(target, "unix-abstract:") {
		return true
	}
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RemotePlaintext returns the targets c would dial in plaintext off
// loopback: the case the plaintext default is not safe for.
func RemotePlaintext(c tlsconfig.Client, targets ...string) []string {
	if c.Enabled() {
		return nil
	}
	var remote []string
	for _, target := range targets {
		if !Loopback(target) {
			remote = append(remote, target)
		}
	}
	return remote
}
