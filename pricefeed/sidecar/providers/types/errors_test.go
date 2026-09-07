package types_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestErrorCodeError(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{name: "ok", code: OK},
		{name: "rate limit", code: ErrorRateLimitExceeded, want: "rate limit exceeded"},
		{name: "unknown pair", code: ErrorUnknownPair, want: "unknown market pair"},
		{name: "decode", code: ErrorFailedToDecode, want: "failed to decode message"},
		{name: "api general", code: ErrorAPIGeneral, want: "general api error"},
		{name: "websocket general", code: ErrorWebSocketGeneral, want: "general websocket error"},
		{name: "invalid websocket topic", code: ErrorInvalidWebSocketTopic, want: "invalid websocket topic received"},
		{name: "parse price", code: ErrorFailedToParsePrice, want: "failed to parse price"},
		{name: "invalid chain ID", code: ErrorInvalidChainID, want: "invalid chain ID in response"},
		{name: "invalid response", code: ErrorInvalidResponse, want: "invalid response"},
		{name: "no response", code: ErrorNoResponse, want: "got no response"},
		{name: "invalid API chains", code: ErrorInvalidAPIChains, want: "invalid chains for api handler"},
		{name: "create URL", code: ErrorUnableToCreateURL, want: "failed to create URL for request"},
		{name: "websocket start", code: ErrorWebsocketStartFail, want: "failed to start websocket connection"},
		{name: "grpc general", code: ErrorGRPCGeneral, want: "general grpc error"},
		{name: "no existing price", code: ErrorNoExistingPrice, want: "no existing price"},
		{name: "ticker metadata missing", code: ErrorTickerMetadataNotFound, want: "ticker metadata not found"},
		{name: "unknown", code: ErrorUnknown, want: "unknown error"},
		{name: "unrecognised code", code: ErrorCode(999), want: "unknown error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.code.Error()
			if tt.want == "" {
				require.NoError(t, err)
				return
			}

			require.EqualError(t, err, tt.want)
		})
	}
}

func TestErrorCodeString(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{name: "ok", code: OK, want: "ok"},
		{name: "rate limit", code: ErrorRateLimitExceeded, want: "rate_limit_exceeded"},
		{name: "unknown", code: ErrorUnknown, want: "unknown"},
		{name: "unknown pair", code: ErrorUnknownPair, want: "unknown_pair"},
		{name: "create URL", code: ErrorUnableToCreateURL, want: "unable_to_create_url"},
		{name: "websocket start", code: ErrorWebsocketStartFail, want: "websocket_start_fail"},
		{name: "invalid API chains", code: ErrorInvalidAPIChains, want: "invalid_api_chains"},
		{name: "no response", code: ErrorNoResponse, want: "no_response"},
		{name: "invalid response", code: ErrorInvalidResponse, want: "invalid_response"},
		{name: "invalid chain ID", code: ErrorInvalidChainID, want: "invalid_chain_id"},
		{name: "parse price", code: ErrorFailedToParsePrice, want: "failed_to_parse_price"},
		{name: "invalid websocket topic", code: ErrorInvalidWebSocketTopic, want: "invalid_websocket_topic"},
		{name: "decode", code: ErrorFailedToDecode, want: "failed_to_decode"},
		{name: "api general", code: ErrorAPIGeneral, want: "api_general"},
		{name: "websocket general", code: ErrorWebSocketGeneral, want: "websocket_general"},
		{name: "grpc general", code: ErrorGRPCGeneral, want: "grpc_general"},
		{name: "no existing price", code: ErrorNoExistingPrice, want: "no_existing_price"},
		{name: "ticker metadata missing", code: ErrorTickerMetadataNotFound, want: "ticker_metadata_not_found"},
		// The API fetcher casts HTTP statuses through the type.
		{name: "HTTP status passes through as a number", code: ErrorCode(429), want: "429"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.code.String())
		})
	}
}

func TestErrorWithCode(t *testing.T) {
	cause := errors.New("provider failed")

	err := NewErrorWithCode(cause, ErrorAPIGeneral)

	require.Equal(t, ErrorAPIGeneral, err.Code())
	require.Equal(t, cause.Error(), err.Error())
}
