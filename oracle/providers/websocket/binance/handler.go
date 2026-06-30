package binance

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers/base/websocket"
	"noah/oracle/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Binance websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Binance websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
	// messageIDs maps subscription request IDs to the Binance symbols in the request.
	messageIDs map[int64][]string
	// nextID is the next subscription request ID.
	nextID int64
}

// NewHandler returns a new Binance DataHandler.
func NewHandler(logger log.Logger, config websocket.Config) (websocket.DataHandler, error) {
	if logger == nil {
		return nil, errors.New("logger is nil")
	}
	if config.Name != Name {
		return nil, fmt.Errorf("expected websocket config name %s, got %s", Name, config.Name)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid websocket config for %s: %w", Name, err)
	}

	return &Handler{
		logger:     logger.With("websocket_data_handler", config.Name),
		config:     config,
		cache:      types.NewTickers(),
		messageIDs: make(map[int64][]string),
		nextID:     1,
	}, nil
}

// HandleMessage is used to handle a message received from the data provider. The Binance websocket
// API is expected to handle the following types of messages:
//  1. SubscribeMessageResponse: This is a response to a subscription request. If the subscription
//     was successful, the response will contain a nil result. If the subscription failed, a
//     re-subscription message will be returned.
//  2. StreamMessageResponse: This is a response to a stream message. The stream message contains
//     the latest price of a ticker - either received when a trade is made or an automated price
//     update is received.
func (h *Handler) HandleMessage(
	message []byte,
) (types.Response, [][]byte, error) {
	var (
		resp      types.Response
		msg       SubscribeMessageResponse
		streamMsg StreamMessageResponse
	)

	// Unmarshal the message. If the message fails to be unmarshaled or is empty, this means
	// that we likely received a price update message.
	if err := json.Unmarshal(message, &msg); err == nil && !msg.IsEmpty() {
		instruments, ok := h.messageIDs[msg.ID]
		if !ok {
			return resp, nil, fmt.Errorf("failed to find instruments for message ID %d", msg.ID)
		}

		if msg.Result != nil {
			// If the result is not nil, this means that the subscription failed to be made. Return
			// an update message with the same subscription.
			h.logger.Debug("failed to make subscription; attempting to re-subscribe", "instruments", msg)
			subscriptionMsgs, err := h.NewSubscribeRequestMessage(instruments)
			return resp, subscriptionMsgs, err
		}

		// If the result is nil, this means that the subscription was successful. Return an empty
		// response.
		h.logger.Debug("successfully subscribed to instruments", "instruments", instruments)
		return resp, nil, nil
	}

	// Unmarshal the message as a stream message. If the message fails to be unmarshaled, this means
	// that we received an unknown message type.
	if err := json.Unmarshal(message, &streamMsg); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message %w", err)
	}

	switch streamMsg.GetStreamType() {
	case TickerStream:
		// Ticker stream is sent every 1000ms and contains the latest price of a ticker.
		var tickerResp TickerMessageResponse
		if err := json.Unmarshal(message, &tickerResp); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker message %w", err)
		}

		h.logger.Debug("received ticker message", "ticker", tickerResp.Data.Ticker)
		resp, err := h.parsePriceUpdateMessage(tickerResp.Data.Ticker, tickerResp.Data.LastPrice)
		return resp, nil, err
	case AggregateTradeStream:
		// Aggregate trade stream is sent when a trade is executed on the Binance exchange.
		var aggTradeResp AggregatedTradeMessageResponse
		if err := json.Unmarshal(message, &aggTradeResp); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal aggregate trade message %w", err)
		}

		h.logger.Debug("received aggregate trade message", "ticker", aggTradeResp.Data.Ticker)
		resp, err := h.parsePriceUpdateMessage(aggTradeResp.Data.Ticker, aggTradeResp.Data.Price)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown stream type %s", streamMsg.Stream)
	}
}

// CreateMessages is used to create a message to send to Binance. This is used to subscribe to
// the given tickers. This is called when the connection to the data provider is first established.
// Notably, the tickers have a unique identifier that is used to identify the messages going back
// and forth. This unique identifier is the same one sent in the initial subscription.
func (h *Handler) CreateMessages(
	tickers []types.Ticker,
) ([][]byte, error) {
	instruments := make([]string, 0)

	for _, ticker := range tickers {
		instruments = append(instruments, string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessage(instruments)
}

// HeartBeatMessages returns nil because Binance does not require provider-level
// heartbeat messages. Websocket ping/pong control frames are handled by the
// connection read loop.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger:     h.logger,
		config:     h.config,
		cache:      types.NewTickers(),
		messageIDs: make(map[int64][]string),
		nextID:     1,
	}
}
