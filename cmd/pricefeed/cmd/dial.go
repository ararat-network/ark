package cmd

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
)

// dial opens a client connection to peer at address over the transport files
// select. peer names the far end in errors: sidecar or chain.
func dial(peer, address string, files tlsconfig.Client, dialOptions ...grpc.DialOption) (*grpc.ClientConn, error) {
	if strings.TrimSpace(address) == "" {
		return nil, fmt.Errorf("%s address cannot be empty", peer)
	}

	material, err := grpcconn.LoadClient(files, address)
	if err != nil {
		return nil, fmt.Errorf("%s connection credentials: %w", peer, err)
	}

	conn, err := grpc.NewClient(address, grpcconn.DialOptions(material.Config, dialOptions...)...)
	if err != nil {
		return nil, fmt.Errorf("dialling %s: %w", peer, err)
	}

	return conn, nil
}

// closeConn closes conn in a defer, joining a failure onto the call's own
// error rather than replacing it.
func closeConn(err *error, peer string, conn *grpc.ClientConn) {
	if closeErr := conn.Close(); closeErr != nil {
		*err = errors.Join(*err, fmt.Errorf("closing %s connection: %w", peer, closeErr))
	}
}
