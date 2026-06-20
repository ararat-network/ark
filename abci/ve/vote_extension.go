package ve

import (
	"context"
	"errors"
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	compression "noah/abci/codec"
	noahmetrics "noah/abci/metrics"
	abcioracle "noah/abci/oracle"
	noahabci "noah/abci/types"
	"noah/abci/ve/types"
	transporttypes "noah/oracle/transport/types"
)

// VoteExtensionHandler extends local votes with oracle price reports. If
// oracle data cannot be fetched, validated, or encoded, the handler returns an
// empty vote extension to preserve liveness.
type VoteExtensionHandler struct {
	logger log.Logger

	// oracleClient is the remote oracle client that is responsible for fetching prices
	oracleClient noahabci.OracleClient

	// timeout is the maximum amount of time to wait for the oracle to respond
	// to a price request.
	timeout time.Duration

	// voteExtensionCodec encodes and decodes oracle vote-extension payloads.
	voteExtensionCodec compression.VoteExtensionCodec

	// priceApplier decodes vote extensions, aggregates price reports, and writes prices to state.
	priceApplier abcioracle.PriceApplier
}

// NewVoteExtensionHandler returns a new VoteExtensionHandler.
func NewVoteExtensionHandler(
	logger log.Logger,
	oracleClient noahabci.OracleClient,
	timeout time.Duration,
	codec compression.VoteExtensionCodec,
	priceApplier abcioracle.PriceApplier,
) *VoteExtensionHandler {
	return &VoteExtensionHandler{
		logger:             logger,
		oracleClient:       oracleClient,
		timeout:            timeout,
		voteExtensionCodec: codec,
		priceApplier:       priceApplier,
	}
}

// ExtendVoteHandler returns a handler that extends votes with oracle price
// reports. If oracle data cannot be fetched, validated, or encoded, the handler
// returns an empty vote extension to preserve liveness.
func (h *VoteExtensionHandler) ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *cometabci.RequestExtendVote) (resp *cometabci.ResponseExtendVote, err error) {
		start := time.Now()

		// Recover from panics and record latency/status before returning. Non-panic
		// failures are logged and reported to metrics, then swallowed so the validator
		// can still return an empty vote extension.
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error(
					"recovered from panic in ExtendVoteHandler",
					"err", r,
				)

				resp, err = &cometabci.ResponseExtendVote{VoteExtension: []byte{}}, ErrPanic{fmt.Errorf("%v", r)}
			}

			latency := time.Since(start)
			h.logger.Debug(
				"extend vote handler",
				"duration (seconds)", latency.Seconds(),
				"err", err,
			)
			noahmetrics.RecordLatencyAndStatus(latency, err, noahmetrics.ExtendVote)

			// Non-panic errors have already been converted into empty vote extensions.
			var p ErrPanic
			if !errors.As(err, &p) {
				err = nil
			}
		}()

		if req == nil {
			h.logger.Error("extend vote handler received a nil request")
			err = noahabci.NilRequestError{
				Handler: noahmetrics.ExtendVote,
			}
			return nil, err
		}

		// Create a context with a timeout to ensure we do not wait forever for the oracle to respond.
		reqCtx, cancel := context.WithTimeout(ctx.Context(), h.timeout)
		defer cancel()

		// To preserve liveness, return an empty vote extension if the oracle is
		// unavailable or returns an invalid response.
		oracleResp, err := h.oracleClient.Prices(ctx.WithContext(reqCtx), &transporttypes.OraclePricesRequest{})
		if err != nil {
			h.logger.Error(
				"failed to retrieve oracle prices for vote extension; returning empty vote extension",
				"height", req.Height,
				"ctx_err", reqCtx.Err(),
				"err", err,
			)

			err = OracleClientError{
				Err: err,
			}

			return &cometabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		// If we get no response, we return an empty vote extension.
		if oracleResp == nil {
			h.logger.Error(
				"oracle returned nil prices for vote extension; returning empty vote extension",
				"height", req.Height,
			)

			err = OracleClientError{fmt.Errorf("oracle returned nil prices")}

			return &cometabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		voteExt := types.OracleVoteExtension{Rates: oracleResp.Prices}
		if err := ValidateOracleVoteExtension(ctx, voteExt); err != nil {
			h.logger.Error(
				"oracle returned invalid prices for vote extension; returning empty vote extension",
				"height", req.Height,
				"err", err,
			)

			err = InvalidOraclePricesError{
				Err: err,
			}

			return &cometabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}
		bz, err := h.voteExtensionCodec.Encode(voteExt)
		if err != nil {
			h.logger.Error(
				"failed to marshal vote extension; returning empty vote extension",
				"height", req.Height,
				"err", err,
			)

			err = noahabci.CodecError{
				Err: err,
			}

			return &cometabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		h.logger.Debug(
			"extending vote with oracle prices",
			"req_height", req.Height,
		)

		return &cometabci.ResponseExtendVote{VoteExtension: bz}, nil
	}
}

// VerifyVoteExtensionHandler returns a handler that verifies the vote extension provided by
// a validator is valid. In the case when the vote extension is empty, we return ACCEPT. This means
// that the validator may have been unable to fetch prices from the oracle and is voting an empty vote extension.
// We reject any non-empty vote extensions that fail to decode or contain invalid prices.
func (h *VoteExtensionHandler) VerifyVoteExtensionHandler() sdk.VerifyVoteExtensionHandler {
	return func(ctx sdk.Context, req *cometabci.RequestVerifyVoteExtension) (_ *cometabci.ResponseVerifyVoteExtension, err error) {
		start := time.Now()

		// Measure latency from invocation to return.
		defer func() {
			latency := time.Since(start)
			h.logger.Debug(
				"verify vote extension handler",
				"duration (seconds)", latency.Seconds(),
			)

			noahmetrics.RecordLatencyAndStatus(latency, err, noahmetrics.VerifyVoteExtension)
		}()

		if req == nil {
			err = noahabci.NilRequestError{
				Handler: noahmetrics.VerifyVoteExtension,
			}
			h.logger.Error("VerifyVoteExtensionHandler received a nil request")
			return nil, err
		}

		// By default, we accept empty vote extensions.
		if len(req.VoteExtension) == 0 {
			h.logger.Info(
				"empty vote extension",
				"height", req.Height,
			)

			return &cometabci.ResponseVerifyVoteExtension{Status: cometabci.ResponseVerifyVoteExtension_ACCEPT}, nil
		}

		// Decode the vote-extension bytes.
		voteExtension, err := h.voteExtensionCodec.Decode(req.VoteExtension)
		if err != nil {
			h.logger.Error(
				"failed to decode vote extension",
				"height", req.Height,
				"err", err,
			)
			err = noahabci.CodecError{
				Err: err,
			}

			return &cometabci.ResponseVerifyVoteExtension{Status: cometabci.ResponseVerifyVoteExtension_REJECT}, err
		}

		if err := ValidateOracleVoteExtension(ctx, voteExtension); err != nil {
			h.logger.Error(
				"failed to validate vote extension",
				"height", req.Height,
				"err", err,
			)
			err = ValidateVoteExtensionError{
				Err: err,
			}

			return &cometabci.ResponseVerifyVoteExtension{Status: cometabci.ResponseVerifyVoteExtension_REJECT}, err
		}

		h.logger.Debug(
			"validated vote extension",
			"height", req.Height,
			"size (bytes)", len(req.VoteExtension),
		)

		// Observe message size.
		noahmetrics.ObserveMessageSize(noahmetrics.VoteExtension, len(req.VoteExtension))

		return &cometabci.ResponseVerifyVoteExtension{Status: cometabci.ResponseVerifyVoteExtension_ACCEPT}, nil
	}
}
