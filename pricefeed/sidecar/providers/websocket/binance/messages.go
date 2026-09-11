// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/binance/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package binance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type (
	// MethodType identifies a Binance websocket request method.
	MethodType string
	// StreamType identifies the Binance stream suffix used to route incoming messages.
	StreamType string
)

const (
	// SubscribeMethod represents a subscribe method. This must be sent as the first message
	// when connecting to the websocket feed.
	//
	// ref: https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#subscribe-to-a-stream
	SubscribeMethod MethodType = "SUBSCRIBE"

	// AggregateTradeStream represents the aggregate trade stream. This stream provides
	// trade information that is aggregated for a single taker order.
	//
	// ref: https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#aggregate-trade-streams
	AggregateTradeStream StreamType = "aggTrade"

	// TickerStream represents the ticker stream. This provides a 24hr rolling window ticker statistics for a single
	// symbol. These are NOT the statistics of the UTC day, but a 24hr rolling window for the previous 24hrs.
	//
	// ref: https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams#individual-symbol-ticker-streams
	TickerStream StreamType = "ticker"

	// Separator is the separator used to separate the symbol and the stream type.
	Separator = "@"
)

// SubscribeMessageRequest names streams to subscribe to. Sequential per-handler IDs correlate
// acknowledgements; see README.md for wire examples and upstream references.
type SubscribeMessageRequest struct {
	// Method is the method type for the message.
	Method string `json:"method"`
	// Params is the list of streams to subscribe to.
	Params []string `json:"params"`
	// ID is the unique identifier for the message.
	ID int64 `json:"id"`
}

// SubscribeMessageResponse matches its request by ID. A null result indicates successful
// subscription; see README.md for the wire contract.
type SubscribeMessageResponse struct {
	// Result is the result of the subscription.
	Result any `json:"result"`
	// ID is the unique identifier for the message.
	ID int64 `json:"id"`
}

// IsEmpty returns true if no data has been set for the message.
func (m *SubscribeMessageResponse) IsEmpty() bool {
	return m.ID == 0 && m.Result == nil
}

// StreamMessageResponse wraps combined-stream payloads with stream and data fields. See README.md
// for supported stream shapes.
type StreamMessageResponse struct {
	// Stream is the stream type.
	Stream string `json:"stream"`
}

// GetStreamType returns the stream type from the stream message response.
func (m *StreamMessageResponse) GetStreamType() StreamType {
	stream := strings.Split(m.Stream, Separator)
	if len(stream) != 2 {
		return ""
	}
	return StreamType(stream[1])
}

// AggregatedTradeMessageResponse carries an aggregate trade; Price is the observation used by this
// adapter. See README.md for the upstream message reference.
type AggregatedTradeMessageResponse struct {
	Data struct {
		// Ticker is the symbol.
		Ticker string `json:"s"`
		// Price is the price.
		Price string `json:"p"`
	} `json:"data"`
}

// TickerMessageResponse carries a rolling ticker observation. See README.md for the upstream field
// reference.
type TickerMessageResponse struct {
	Data struct {
		// Ticker is the symbol.
		Ticker string `json:"s"`
		// LastPrice is the last price.
		LastPrice string `json:"c"`
		// StatisticsCloseTime is the statistics close time.
		//
		// Note: This is unused but is included since json.Unmarshal requires all fields with same character but different casing
		// to be present.
		StatisticsCloseTime int64 `json:"C"`
	} `json:"data"`
}

// NewSubscribeRequestMessage builds subscription request messages for Binance symbols.
// Each symbol is subscribed to both aggregate trade and ticker streams.
func (h *Handler) NewSubscribeRequestMessage(instruments []string) ([][]byte, error) {
	numInstruments := len(instruments)
	if numInstruments == 0 {
		return nil, errors.New("no instruments to subscribe to")
	}

	maxSubsPerBatch := h.config.MaxSubscriptionsPerBatch
	numBatches := (numInstruments + maxSubsPerBatch - 1) / maxSubsPerBatch
	msgs := make([][]byte, numBatches)
	for i := range numBatches {
		// Get the symbols for the batch.
		start := i * maxSubsPerBatch
		end := min((i+1)*maxSubsPerBatch, numInstruments)
		batch := instruments[start:end]

		// Create the stream subscriptions for the symbols.
		params := make([]string, 0)
		for _, instrument := range batch {
			params = append(params, fmt.Sprintf("%s%s%s", strings.ToLower(instrument), Separator, string(AggregateTradeStream)))
			params = append(params, fmt.Sprintf("%s%s%s", strings.ToLower(instrument), Separator, string(TickerStream)))
		}

		// Generate an ID for correlating the subscription response.
		id := h.GenerateID()
		msg, err := json.Marshal(SubscribeMessageRequest{
			Method: string(SubscribeMethod),
			Params: params,
			ID:     id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}

		// Track the symbols for this subscription request ID.
		h.messageIDs[id] = batch
		msgs[i] = msg
	}

	return msgs, nil
}

// GenerateID returns the next sequential subscription request ID.
func (h *Handler) GenerateID() int64 {
	id := h.nextID
	h.nextID++
	return id
}
