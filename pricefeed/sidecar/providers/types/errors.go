// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/types/errors.go.
// Modified for Ark: provider error classification and reporting.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"errors"
	"strconv"
)

// ErrorCode classifies provider failures for response handling and metrics.
type ErrorCode int

const (
	OK                          ErrorCode = 0
	ErrorRateLimitExceeded      ErrorCode = 1
	ErrorUnknown                ErrorCode = 2
	ErrorUnknownPair            ErrorCode = 3
	ErrorUnableToCreateURL      ErrorCode = 4
	ErrorWebsocketStartFail     ErrorCode = 5
	ErrorInvalidAPIChains       ErrorCode = 6
	ErrorNoResponse             ErrorCode = 7
	ErrorInvalidResponse        ErrorCode = 8
	ErrorInvalidChainID         ErrorCode = 9
	ErrorFailedToParsePrice     ErrorCode = 10
	ErrorInvalidWebSocketTopic  ErrorCode = 11
	ErrorFailedToDecode         ErrorCode = 12
	ErrorAPIGeneral             ErrorCode = 13
	ErrorWebSocketGeneral       ErrorCode = 14
	ErrorGRPCGeneral            ErrorCode = 15
	ErrorNoExistingPrice        ErrorCode = 16
	ErrorTickerMetadataNotFound ErrorCode = 17
)

// String is the code's metric label: a stable snake_case name for a declared
// code, the number itself for any other, such as an HTTP status the API
// fetcher casts through.
func (e ErrorCode) String() string {
	if name, ok := errorCodeNames[e]; ok {
		return name
	}
	return strconv.Itoa(int(e))
}

var errorCodeNames = map[ErrorCode]string{
	OK:                          "ok",
	ErrorRateLimitExceeded:      "rate_limit_exceeded",
	ErrorUnknown:                "unknown",
	ErrorUnknownPair:            "unknown_pair",
	ErrorUnableToCreateURL:      "unable_to_create_url",
	ErrorWebsocketStartFail:     "websocket_start_fail",
	ErrorInvalidAPIChains:       "invalid_api_chains",
	ErrorNoResponse:             "no_response",
	ErrorInvalidResponse:        "invalid_response",
	ErrorInvalidChainID:         "invalid_chain_id",
	ErrorFailedToParsePrice:     "failed_to_parse_price",
	ErrorInvalidWebSocketTopic:  "invalid_websocket_topic",
	ErrorFailedToDecode:         "failed_to_decode",
	ErrorAPIGeneral:             "api_general",
	ErrorWebSocketGeneral:       "websocket_general",
	ErrorGRPCGeneral:            "grpc_general",
	ErrorNoExistingPrice:        "no_existing_price",
	ErrorTickerMetadataNotFound: "ticker_metadata_not_found",
}

// Error returns the error representation of the ErrorCode.
func (e ErrorCode) Error() error {
	switch e {
	case OK:
		return nil
	case ErrorRateLimitExceeded:
		return errors.New("rate limit exceeded")
	case ErrorUnknownPair:
		return errors.New("unknown market pair")
	case ErrorFailedToDecode:
		return errors.New("failed to decode message")
	case ErrorAPIGeneral:
		return errors.New("general api error")
	case ErrorWebSocketGeneral:
		return errors.New("general websocket error")
	case ErrorInvalidWebSocketTopic:
		return errors.New("invalid websocket topic received")
	case ErrorFailedToParsePrice:
		return errors.New("failed to parse price")
	case ErrorInvalidChainID:
		return errors.New("invalid chain ID in response")
	case ErrorInvalidResponse:
		return errors.New("invalid response")
	case ErrorNoResponse:
		return errors.New("got no response")
	case ErrorInvalidAPIChains:
		return errors.New("invalid chains for api handler")
	case ErrorUnableToCreateURL:
		return errors.New("failed to create URL for request")
	case ErrorWebsocketStartFail:
		return errors.New("failed to start websocket connection")
	case ErrorGRPCGeneral:
		return errors.New("general grpc error")
	case ErrorNoExistingPrice:
		return errors.New("no existing price")
	case ErrorTickerMetadataNotFound:
		return errors.New("ticker metadata not found")
	case ErrorUnknown:
		fallthrough
	default:
		return errors.New("unknown error")
	}
}

// ErrorWithCode couples a provider-facing error with its stable classification.
type ErrorWithCode struct {
	code        ErrorCode
	internalErr error
}

// Error returns an error string wrapping the internalErr and the error code.
func (ec ErrorWithCode) Error() string {
	return ec.internalErr.Error()
}

// Code returns the internal ErrorCode.
func (ec ErrorWithCode) Code() ErrorCode {
	return ec.code
}

// NewErrorWithCode returns err classified by ec.
func NewErrorWithCode(err error, ec ErrorCode) ErrorWithCode {
	return ErrorWithCode{
		code:        ec,
		internalErr: err,
	}
}
