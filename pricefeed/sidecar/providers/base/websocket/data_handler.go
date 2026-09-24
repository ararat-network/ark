package websocket

import (
	"net/http"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// DataHandler defines provider-specific websocket protocol behaviour.
// Fetcher uses it to build subscription messages, parse incoming messages, and
// create optional follow-up or heartbeat messages for the same connection.
type DataHandler interface {
	// HandleMessage parses a provider message and returns the provider response
	// plus any required messages that should be written back to the websocket.
	HandleMessage(message []byte) (response types.Response, updateMessages [][]byte, err error)

	// CreateMessages builds the initial subscription messages for tickers.
	CreateMessages(tickers []types.Ticker) ([][]byte, error)

	// HeartBeatMessages builds periodic heartbeat messages. Implementations that
	// derive heartbeat state from provider messages must be safe for concurrent
	// calls with HandleMessage.
	HeartBeatMessages() ([][]byte, error)

	// Copy returns an independent handler for one websocket connection.
	Copy() DataHandler
}

// Dialer is implemented by handlers whose venue needs more than a plain dial
// to open a connection, such as a per-connection token fetched over HTTPS.
// The registry installs the returned DialFunc on the fetcher; client is the
// redirect-refusing HTTP client the fetcher dials with.
type Dialer interface {
	DialFunc(client *http.Client) DialFunc
}
