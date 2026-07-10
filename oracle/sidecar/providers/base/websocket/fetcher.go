package websocket

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	sidecarinternal "ark/oracle/sidecar/internal"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/providers/base/websocket/metrics"
	"ark/oracle/sidecar/providers/types"
)

// Fetcher maintains websocket subscriptions for provider ticker prices.
type Fetcher struct {
	logger log.Logger
	config Config

	httpClient       *http.Client
	headers          http.Header
	dial             DialFunc
	endpointSelector types.EndpointSelector

	dataHandler DataHandler
}

// NewFetcher returns a websocket fetcher using dataHandler for provider-specific messages.
func NewFetcher(cfg Config, dataHandler DataHandler, opts ...Option) (*Fetcher, error) {
	f := &Fetcher{
		logger:           log.NewNopLogger(),
		config:           cfg,
		dial:             websocket.Dial,
		endpointSelector: types.FirstEndpoint,
		dataHandler:      dataHandler,
	}

	for _, opt := range opts {
		opt(f)
	}

	if f.logger == nil {
		return nil, errors.New("logger is nil")
	}
	if f.dataHandler == nil {
		return nil, errors.New("data handler is nil")
	}
	if f.dial == nil {
		return nil, errors.New("dial function is nil")
	}
	if f.endpointSelector == nil {
		return nil, errors.New("endpoint selector is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	f.logger = f.logger.With("websocket_fetcher", f.config.Name)

	return f, nil
}

// Run starts websocket connections for tickers and publishes provider responses until ctx is cancelled.
func (f *Fetcher) Run(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	if responseCh == nil {
		f.logger.Debug("response channel is nil")
		return errors.New("response channel is nil")
	}
	if len(tickers) == 0 {
		f.logger.Debug("no tickers to query; exiting")
		return nil
	}

	maxTickersPerConn := f.config.MaxTickersPerConnection
	if maxTickersPerConn == 0 {
		maxTickersPerConn = len(tickers)
	}
	numConnections := (len(tickers) + maxTickersPerConn - 1) / maxTickersPerConn

	f.logger.Debug(
		"starting websocket fetcher",
		"tickers", len(tickers),
		"max_tickers_per_connection", maxTickersPerConn,
		"connections", numConnections,
		"response_buffer_size", f.ResponseBufferSize(tickers),
	)

	group, groupCtx := errgroup.WithContext(ctx)
	for subTickers := range slices.Chunk(tickers, maxTickersPerConn) {
		group.Go(func() error {
			return sidecarinternal.RunRecovering("websocket connection", func() error {
				return f.runConnection(groupCtx, subTickers, responseCh)
			})
		})
	}

	return group.Wait()
}

// Type returns the fetcher type.
func (f *Fetcher) Type() base.TransportType {
	return base.WebSocket
}

// Name returns the provider name configured on the fetcher.
func (f *Fetcher) Name() string {
	return f.config.Name
}

// ResponseBufferSize returns the configured response buffer capacity.
func (f *Fetcher) ResponseBufferSize([]types.Ticker) int {
	return f.config.MaxBufferSize
}

// runConnection supervises one ticker shard, restarting its websocket session
// after reconnect signals until ctx is cancelled.
func (f *Fetcher) runConnection(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	// Restart the connection while the parent context remains active.
	restarts := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if restarts > 0 {
			metrics.RecordReconnect(ctx, f.config.Name)
			f.logger.Debug(
				"restarting websocket connection",
				"num_restarts", restarts,
			)
			if err := sleep(ctx, f.config.ReconnectionTimeout); err != nil {
				return err
			}
		}

		handler := f.dataHandler.Copy()
		if err := f.runOnce(ctx, tickers, handler, responseCh); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if errors.Is(err, errReconnect) {
				f.logger.Debug(
					"websocket connection session returned reconnect signal",
					"error", err,
				)
				restarts++
				continue
			}

			f.logger.Error(
				"websocket connection session returned error",
				"error", err,
			)

			return err
		}
		restarts++
	}
}

// runOnce runs one websocket connection session: select an endpoint, dial,
// subscribe, then receive messages until the session fails or ctx is cancelled.
func (f *Fetcher) runOnce(
	ctx context.Context,
	tickers []types.Ticker,
	handler DataHandler,
	responseCh chan<- types.Response,
) error {
	dialCtx, cancel := context.WithTimeout(ctx, f.config.HandshakeTimeout)
	defer cancel()
	endpoint, err := f.endpointSelector(f.config.Endpoints)
	if err != nil {
		return ErrSelectEndpointWithErr(err)
	}

	conn, _, err := f.dial(dialCtx, endpoint.URL, f.dialOptions(endpoint))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dialErr := ErrDialWithErr(err)
		response := types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(dialErr, types.ErrorWebsocketStartFail),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case responseCh <- response:
			metrics.RecordConnectionEvent(ctx, f.config.Name, metrics.ConnectionEventDialError)
			return reconnectWithErr(dialErr)
		}
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			f.logger.Debug("failed to close websocket connection", "error", err)
		}
	}()

	// Subscribe before starting the receive loop.
	if err := f.subscribe(ctx, conn, handler, tickers, responseCh); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return err
	}

	metrics.RecordConnectionEvent(ctx, f.config.Name, metrics.ConnectionEventHealthy)

	if f.config.PingInterval == 0 {
		return f.recv(ctx, conn, handler, tickers, responseCh)
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return sidecarinternal.RunRecovering("websocket heartbeat", func() error {
			return f.heartBeat(groupCtx, conn, handler, tickers, responseCh)
		})
	})
	group.Go(func() error {
		return sidecarinternal.RunRecovering("websocket receive", func() error {
			return f.recv(groupCtx, conn, handler, tickers, responseCh)
		})
	})

	return group.Wait()
}

// subscribe sends the initial provider subscription messages for tickers.
// Message-construction errors are returned as local handler errors. Subscription
// write errors publish unresolved ticker responses because the provider session
// failed after dialling.
func (f *Fetcher) subscribe(
	ctx context.Context,
	conn *websocket.Conn,
	handler DataHandler,
	tickers []types.Ticker,
	responseCh chan<- types.Response,
) error {
	// Some providers require a delay after dial before subscription messages.
	if err := sleep(ctx, f.config.PostConnectionTimeout); err != nil {
		return err
	}

	messages, err := handler.CreateMessages(tickers)
	if err != nil {
		f.logger.Debug("failed to create subscription messages", "error", err)
		return ErrCreateMessageWithErr(err)
	}

	for index, message := range messages {
		if err := f.write(ctx, conn, message); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			writeErr := ErrWriteWithErr(err)
			f.logger.Debug("failed to write subscription message", "error", writeErr)
			metrics.RecordWriteError(ctx, f.config.Name, metrics.WriteOperationSubscribe)
			response := types.NewErrorResponse(
				tickers,
				types.NewErrorWithCode(
					writeErr,
					types.ErrorWebsocketStartFail,
				),
			)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case responseCh <- response:
				metrics.RecordConnectionEvent(ctx, f.config.Name, metrics.ConnectionEventSubscribeError)
				return reconnectWithErr(writeErr)
			}
		}

		if index != len(messages)-1 {
			if err := sleep(ctx, f.config.WriteInterval); err != nil {
				return err
			}
		}
	}

	return nil
}

// recv reads provider messages, converts them to provider responses, and writes
// optional follow-up messages returned by the handler.
func (f *Fetcher) recv(
	ctx context.Context,
	conn *websocket.Conn,
	handler DataHandler,
	tickers []types.Ticker,
	responseCh chan<- types.Response,
) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		message, err := f.read(ctx, conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			f.logger.Error(
				"failed to read message from websocket handler",
				"error", err,
			)
			metrics.RecordConnectionEvent(ctx, f.config.Name, metrics.ConnectionEventReadError)

			readErr := ErrReadWithErr(err)
			response := types.NewErrorResponse(
				tickers,
				types.NewErrorWithCode(
					readErr,
					types.ErrorWebSocketGeneral,
				),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case responseCh <- response:
				return reconnectWithErr(readErr)
			}
		}

		response, updateMessage, err := handler.HandleMessage(message)
		if err != nil {
			f.logger.Debug("failed to handle websocket message", "error", err)
			metrics.RecordParseError(ctx, f.config.Name)
			continue
		}

		if !response.Empty() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case responseCh <- response:
			}
		}

		for _, msg := range updateMessage {
			if err := f.write(ctx, conn, msg); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				writeErr := ErrWriteWithErr(err)
				response := types.NewErrorResponse(
					tickers,
					types.NewErrorWithCode(writeErr, types.ErrorWebSocketGeneral),
				)

				select {
				case <-ctx.Done():
					return ctx.Err()
				case responseCh <- response:
					metrics.RecordWriteError(ctx, f.config.Name, metrics.WriteOperationUpdate)
					return reconnectWithErr(writeErr)
				}
			}
		}
	}
}

// heartBeat periodically writes provider heartbeat messages while the connection
// session is active.
func (f *Fetcher) heartBeat(
	ctx context.Context,
	conn *websocket.Conn,
	handler DataHandler,
	tickers []types.Ticker,
	responseCh chan<- types.Response,
) error {
	ticker := time.NewTicker(f.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			msgs, err := handler.HeartBeatMessages()
			if err != nil {
				f.logger.Debug("failed to create heartbeat messages", "error", err)
				continue
			}

			for _, msg := range msgs {
				if err := f.write(ctx, conn, msg); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}

					writeErr := ErrWriteWithErr(err)
					response := types.NewErrorResponse(
						tickers,
						types.NewErrorWithCode(writeErr, types.ErrorWebSocketGeneral),
					)

					select {
					case <-ctx.Done():
						return ctx.Err()
					case responseCh <- response:
						metrics.RecordWriteError(ctx, f.config.Name, metrics.WriteOperationHeartbeat)
						return reconnectWithErr(writeErr)
					}
				}
			}
		}
	}
}

// sleep waits for d or returns early when ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
