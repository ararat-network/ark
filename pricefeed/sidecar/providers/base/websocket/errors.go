// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/base/websocket/errors/ws_query_handler.go.
// Modified for Ark: websocket session and handler errors.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package websocket

import "errors"

var (
	// errReconnect is an internal signal that a websocket session should be
	// abandoned and retried without stopping the fetcher.
	errReconnect = errors.New("websocket reconnect")

	// ErrHandleMessage is returned when a DataHandler cannot parse or handle a
	// provider message.
	ErrHandleMessage = errors.New("websocket data handler failed to handle message")

	// ErrCreateMessages is returned when a DataHandler cannot create subscription
	// messages for the requested tickers.
	ErrCreateMessages = errors.New("websocket data handler failed to create messages")

	// ErrRead is returned when the fetcher cannot read a websocket message.
	ErrRead = errors.New("websocket fetcher failed to read message")

	// ErrWrite is returned when the fetcher cannot write a websocket message.
	ErrWrite = errors.New("websocket fetcher failed to write message")

	// ErrDial is returned when the fetcher cannot create a websocket connection.
	ErrDial = errors.New("websocket fetcher failed to create connection")

	// ErrSelectEndpoint is returned when the fetcher cannot choose a websocket endpoint.
	ErrSelectEndpoint = errors.New("websocket fetcher failed to select endpoint")
)

// ErrHandleMessageWithErr wraps an underlying message handling error.
// DataHandler implementations should use this function when message parsing fails.
func ErrHandleMessageWithErr(err error) error {
	return errors.Join(ErrHandleMessage, err)
}

// ErrCreateMessageWithErr wraps an underlying subscription message creation error.
// DataHandler implementations should use this function when subscription construction fails.
func ErrCreateMessageWithErr(err error) error {
	return errors.Join(ErrCreateMessages, err)
}

// ErrReadWithErr wraps an underlying websocket read error.
func ErrReadWithErr(err error) error {
	return errors.Join(ErrRead, err)
}

// ErrWriteWithErr wraps an underlying websocket write error.
func ErrWriteWithErr(err error) error {
	return errors.Join(ErrWrite, err)
}

// ErrDialWithErr wraps an underlying websocket dial error.
func ErrDialWithErr(err error) error {
	return errors.Join(ErrDial, err)
}

// ErrSelectEndpointWithErr wraps an underlying endpoint selection error.
func ErrSelectEndpointWithErr(err error) error {
	return errors.Join(ErrSelectEndpoint, err)
}

func reconnectWithErr(err error) error {
	return errors.Join(errReconnect, err)
}
