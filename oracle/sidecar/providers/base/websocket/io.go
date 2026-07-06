package websocket

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

// DialFunc opens a websocket connection to url using opts.
type DialFunc func(
	ctx context.Context,
	url string,
	opts *websocket.DialOptions,
) (*websocket.Conn, *http.Response, error)

// dialOptions builds the coder/websocket dial options from fetcher settings.
func (f *Fetcher) dialOptions() *websocket.DialOptions {
	mode := websocket.CompressionDisabled
	if f.config.EnableCompression {
		mode = websocket.CompressionNoContextTakeover
	}

	return &websocket.DialOptions{
		HTTPClient:      f.httpClient,
		HTTPHeader:      f.headers,
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
