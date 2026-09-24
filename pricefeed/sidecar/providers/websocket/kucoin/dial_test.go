package kucoin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	basews "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/kucoin"
)

const tokenBody = `{"code":"200000","data":{"token":"tok-abc","instanceServers":[{"endpoint":"wss://ws-api-spot.kucoin.com/","protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`

// TestHandlerIsADialer pins the registry contract: the fetcher must dial
// through the handler, or KuCoin refuses the connection for want of a token.
func TestHandlerIsADialer(t *testing.T) {
	handler, err := NewHandler(log.NewNopLogger(), DefaultWebSocketConfig)
	require.NoError(t, err)

	dialer, ok := handler.(basews.Dialer)
	require.True(t, ok)
	require.NotNil(t, dialer.DialFunc(http.DefaultClient))
}

func TestNewDialFunc(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		status      int
		body        string
		errContains string
	}{
		{
			name:   "token is carried on the dial URL",
			status: http.StatusOK,
			body:   tokenBody,
		},
		{
			name:        "non-200 status",
			status:      http.StatusTooManyRequests,
			body:        tokenBody,
			errContains: "token request returned status 429",
		},
		{
			name:        "failure code",
			status:      http.StatusOK,
			body:        `{"code":"400001","msg":"bad request"}`,
			errContains: `token request returned code "400001"`,
		},
		{
			name:        "missing token",
			status:      http.StatusOK,
			body:        `{"code":"200000","data":{"instanceServers":[]}}`,
			errContains: "token response carries no token",
		},
		{
			name:        "malformed body",
			status:      http.StatusOK,
			body:        `{`,
			errContains: "decoding token response",
		},
		{
			name:        "oversized body",
			status:      http.StatusOK,
			body:        `{"code":"200000","data":{"token":"` + strings.Repeat("a", MaxTokenResponseBytes) + `"}}`,
			errContains: "token response exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			var dialled string
			dial := func(_ context.Context, endpoint string, _ *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
				dialled = endpoint
				return nil, nil, nil
			}

			_, _, err := NewDialFunc(server.Client(), server.URL, dial)(context.Background(), URL, nil)
			require.Equal(t, http.MethodPost, gotMethod)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				require.Empty(t, dialled)
				return
			}

			require.NoError(t, err)
			parsed, err := url.Parse(dialled)
			require.NoError(t, err)
			require.Equal(t, "wss", parsed.Scheme)
			require.Equal(t, "ws-api-spot.kucoin.com", parsed.Host)
			require.Equal(t, "tok-abc", parsed.Query().Get("token"))
		})
	}
}

// TestDialErrorRedactsTheToken pins the log hygiene: a dial error names the
// URL it tried, and that URL carries the token.
func TestDialErrorRedactsTheToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(tokenBody))
	}))
	defer server.Close()

	dial := func(_ context.Context, endpoint string, _ *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
		return nil, nil, errors.New("failed to WebSocket dial: " + endpoint)
	}

	_, _, err := NewDialFunc(server.Client(), server.URL, dial)(context.Background(), URL, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "tok-abc")
	require.Contains(t, err.Error(), "token=<token>")
}

// TestTokenRequestHonoursTheContext pins that a dial deadline bounds the
// token request too, since both share the handshake timeout.
func TestTokenRequestHonoursTheContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := NewDialFunc(server.Client(), server.URL, nil)(ctx, URL, nil)
	require.ErrorContains(t, err, "fetching KuCoin connect token")
	require.ErrorIs(t, err, context.Canceled)
}
