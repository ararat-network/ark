package websocket

import (
	"context"
	"net/http"

	"github.com/coder/websocket"

	"ark/pricefeed/sidecar/providers/types"
)

// DialFunc opens a websocket connection to url using opts.
type DialFunc func(
	ctx context.Context,
	url string,
	opts *websocket.DialOptions,
) (*websocket.Conn, *http.Response, error)

// dialOptions builds the coder/websocket dial options for endpoint.
func (f *Fetcher) dialOptions(endpoint types.Endpoint) *websocket.DialOptions {
	mode := websocket.CompressionDisabled
	if f.config.EnableCompression {
		mode = websocket.CompressionNoContextTakeover
	}

	headers := f.headers.Clone()
	if auth := endpoint.Authentication; auth.Enabled() {
		if headers == nil {
			headers = make(http.Header)
		}
		headers.Set(auth.APIKeyHeader, auth.APIKey)
	}

	return &websocket.DialOptions{
		HTTPClient:      f.httpClient,
		HTTPHeader:      headers,
		CompressionMode: mode,
	}
}

// read reads one websocket message with the configured read timeout.
func (f *Fetcher) read(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	readCtx, cancel := context.WithTimeout(ctx, f.config.ReadTimeout)
	defer cancel()

	_, message, err := conn.Read(readCtx)
	return message, err
}

// write writes one text message with the configured write timeout.
func (f *Fetcher) write(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	// One goroutine owns reads. coder/websocket allows concurrent writes, so
	// heartbeat and update writes do not need fetcher-level locking.
	writeCtx, cancel := context.WithTimeout(ctx, f.config.WriteTimeout)
	defer cancel()

	return conn.Write(writeCtx, websocket.MessageText, msg)
}
