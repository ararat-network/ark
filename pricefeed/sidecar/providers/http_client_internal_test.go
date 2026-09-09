package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestProviderClientsRefuseRedirectsBeforeForwardingCredentials(t *testing.T) {
	for _, protocol := range []string{"https", "wss"} {
		for _, destination := range []string{"https", "http", "wss", "ws"} {
			t.Run(protocol+" to "+destination, func(t *testing.T) {
				var originCalls, targetCalls atomic.Int32
				target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
				defer target.Close()
				location := strings.Replace(target.URL, "https://", destination+"://", 1)
				origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					originCalls.Add(1)
					if r.Header.Get("X-API-Key") != "dummy" {
						t.Error("origin did not receive dummy credentials")
					}
					http.Redirect(w, r, location, http.StatusFound)
				}))
				defer origin.Close()
				client := newHTTPClient()
				client.Transport = origin.Client().Transport
				client.Timeout = time.Second
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				var err error
				if protocol == "wss" {
					_, _, err = websocket.Dial(ctx, strings.Replace(origin.URL, "https://", "wss://", 1), &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"X-Api-Key": []string{"dummy"}}})
				} else {
					req, e := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
					require.NoError(t, e)
					req.Header.Set("X-API-Key", "dummy")
					_, err = client.Do(req)
				}
				require.ErrorContains(t, err, "provider redirects are refused")
				require.Equal(t, int32(1), originCalls.Load())
				require.Zero(t, targetCalls.Load())
			})
		}
	}
}
