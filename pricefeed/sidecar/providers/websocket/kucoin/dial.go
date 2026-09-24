// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kucoin/hooks.go.
// Modified for Ark: token dial in place of a config-mutating pre-dial hook.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kucoin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	coderws "github.com/coder/websocket"

	basews "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
)

const (
	// SuccessCode is the code of a successful token response.
	SuccessCode = "200000"

	// TokenQueryParameter carries the connect token on the dial URL, which
	// is where KuCoin's protocol reads it.
	TokenQueryParameter = "token"

	// MaxTokenResponseBytes bounds the token response body.
	MaxTokenResponseBytes = 64 << 10
)

var _ basews.Dialer = (*Handler)(nil)

// TokenResponse is the bullet-public response. Only the token is used; the
// instance servers and ping timings it also carries are left to config. See
// README.md for the wire shape.
type TokenResponse struct {
	Code string    `json:"code"`
	Data TokenData `json:"data"`
}

// TokenData carries the connect token.
type TokenData struct {
	Token string `json:"token"`
}

// DialFunc satisfies websocket.Dialer with the production token endpoint.
func (h *Handler) DialFunc(client *http.Client) basews.DialFunc {
	return NewDialFunc(client, TokenURL, coderws.Dial)
}

// NewDialFunc returns a dial that requests a connect token from tokenURL
// with client, then dials the endpoint with the token as its query
// parameter. A dial error is returned with the token redacted, since the
// URL it names would otherwise reach the logs.
func NewDialFunc(client *http.Client, tokenURL string, dial basews.DialFunc) basews.DialFunc {
	return func(ctx context.Context, endpoint string, opts *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
		token, err := fetchToken(ctx, client, tokenURL)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching KuCoin connect token: %w", err)
		}

		parsedURL, err := url.Parse(endpoint)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing KuCoin endpoint: %w", err)
		}
		query := parsedURL.Query()
		query.Set(TokenQueryParameter, token)
		parsedURL.RawQuery = query.Encode()

		conn, resp, err := dial(ctx, parsedURL.String(), opts)
		if err != nil {
			return nil, resp, errors.New(strings.ReplaceAll(err.Error(), token, "<token>"))
		}
		return conn, resp, nil
	}
}

// fetchToken requests a connect token. The request is a POST with no body.
func fetchToken(ctx context.Context, client *http.Client, tokenURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxTokenResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}
	if len(body) > MaxTokenResponseBytes {
		return "", fmt.Errorf("token response exceeds %d bytes", MaxTokenResponseBytes)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}
	if tokenResp.Code != SuccessCode {
		return "", fmt.Errorf("token request returned code %q", tokenResp.Code)
	}
	if tokenResp.Data.Token == "" {
		return "", errors.New("token response carries no token")
	}

	return tokenResp.Data.Token, nil
}
