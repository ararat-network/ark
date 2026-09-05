package voteextension

import (
	"context"
	"errors"
	"fmt"
	"time"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	arkmetrics "github.com/ararat-network/ark/abci/metrics"
	abcioracle "github.com/ararat-network/ark/abci/oracle"
	arkabci "github.com/ararat-network/ark/abci/types"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pricefeed/api"
)

// Handler extends local votes with oracle price reports. If
// oracle data cannot be fetched, validated, or encoded, the handler returns an
// empty vote extension to preserve liveness.
type Handler struct {
	logger log.Logger

	// oracleClient is the remote oracle client that is responsible for fetching prices
	oracleClient arkabci.PriceFeedClient

	// oracleKeeper resolves the consensus target epoch for each vote height.
	oracleKeeper arkabci.OracleKeeper

	// timeout is the maximum amount of time to wait for the oracle to respond
	// to a price request.
	timeout time.Duration
}

// NewHandler returns a new Handler.
func NewHandler(
	logger log.Logger,
	oracleClient arkabci.PriceFeedClient,
	oracleKeeper arkabci.OracleKeeper,
	timeout time.Duration,
) *Handler {
	return &Handler{
		logger:       logger,
		oracleClient: oracleClient,
		oracleKeeper: oracleKeeper,
		timeout:      timeout,
	}
}

// ExtendVoteHandler returns a handler that extends votes with oracle price
// reports. Individual sidecar rates that fail to decode are dropped from the
// report one denom at a time; if oracle data cannot be fetched, validated, or
// encoded at all, the handler returns an empty vote extension to preserve
// liveness.
func (h *Handler) ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *cmtabci.RequestExtendVote) (resp *cmtabci.ResponseExtendVote, err error) {
		start := time.Now()
		returnError := false

		// Recover from panics and record status before returning. Operational
		// failures that already produced an empty vote extension are swallowed to
		// preserve liveness; structural failures remain visible to the caller.
		defer func() {
			if r := recover(); r != nil {
				panicCause, ok := r.(error)
				if !ok {
					panicCause = fmt.Errorf("%v", r)
				}
				resp, err = &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, fmt.Errorf("%w: %w", errPanic, panicCause)
				returnError = true
			}

			latency := time.Since(start)
			arkmetrics.RecordLatencyAndStatus(latency, voteExtensionStatus(err), arkmetrics.ExtendVote)
			if err != nil {
				if req == nil {
					h.logger.Error("extend vote handler failed", "err", err)
				} else {
					h.logger.Error("extend vote handler failed", "height", req.Height, "err", err)
				}
			}

			if !returnError {
				err = nil
			}
		}()

		if req == nil {
			err = fmt.Errorf("%w for %s", arkabci.ErrNilRequest, arkmetrics.ExtendVote)
			returnError = true
			return nil, err
		}

		feeds, err := h.oracleKeeper.GetFeeds(ctx, req.Height)
		if err != nil {
			err = fmt.Errorf("%w: get feeds for height %d: %w", arkabci.ErrOracleKeeper, req.Height, err)
			return &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		// Create a context with a timeout to ensure we do not wait forever for the oracle to respond.
		reqCtx, cancel := context.WithTimeout(ctx.Context(), h.timeout)
		defer cancel()

		// To preserve liveness, return an empty vote extension if the oracle is
		// unavailable or returns an invalid response.
		oracleResp, err := h.oracleClient.Prices(reqCtx, &api.PricesRequest{})
		if err != nil {
			err = fmt.Errorf("%w: %w", errPriceFeedClient, err)
			return &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		// If we get no response, we return an empty vote extension.
		if oracleResp == nil {
			err = fmt.Errorf("%w: oracle returned nil prices", errPriceFeedClient)
			return &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		rates := make(map[string][]byte, len(feeds.Denoms))
		var droppedTargets []string
		for _, denom := range feeds.Denoms {
			rawRate, ok := oracleResp.Prices[denom]
			if !ok {
				continue
			}
			// The failure domain of a sidecar rate is one denom, so one
			// undecodable or oversized rate must not abort the whole report: it
			// is dropped exactly like an omitted target and the remaining
			// reports still submit. The sidecar omits unpriced targets and the
			// compact encoding admits only positive rates, so omission is the
			// only abstention.
			if _, rateErr := abcioracle.DecodeVoteRate(rawRate); rateErr != nil {
				droppedTargets = append(droppedTargets, denom)
				continue
			}
			rates[denom] = rawRate
		}
		if len(droppedTargets) > 0 {
			h.logger.Error(
				"dropping undecodable oracle rates from vote extension",
				"height", req.Height,
				"targets", droppedTargets,
			)
		}
		voteExt := vetypes.OracleVoteExtension{
			Rates:         rates,
			TargetVersion: feeds.Version,
		}
		if _, validationErr := abcioracle.ValidateVoteExtension(voteExt, feeds); validationErr != nil {
			err = fmt.Errorf("%w: %w", errInvalidPrices, validationErr)
			return &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}
		bz, err := codec.EncodeVoteExtension(voteExt)
		if err != nil {
			err = fmt.Errorf("%w: %w", arkabci.ErrCodec, err)
			return &cmtabci.ResponseExtendVote{VoteExtension: []byte{}}, err
		}

		return &cmtabci.ResponseExtendVote{VoteExtension: bz}, nil
	}
}

// VerifyVoteExtensionHandler returns a handler that verifies the vote extension provided by
// a validator is valid. In the case when the vote extension is empty, we return ACCEPT. This means
// that the validator may have been unable to fetch prices from the oracle and is voting an empty vote extension.
// We reject any non-empty vote extensions that fail to decode or contain invalid prices.
func (h *Handler) VerifyVoteExtensionHandler() sdk.VerifyVoteExtensionHandler {
	return func(ctx sdk.Context, req *cmtabci.RequestVerifyVoteExtension) (_ *cmtabci.ResponseVerifyVoteExtension, err error) {
		start := time.Now()

		// Measure latency from invocation to return.
		defer func() {
			latency := time.Since(start)
			arkmetrics.RecordLatencyAndStatus(latency, voteExtensionStatus(err), arkmetrics.VerifyVoteExtension)
		}()

		if req == nil {
			err = fmt.Errorf("%w for %s", arkabci.ErrNilRequest, arkmetrics.VerifyVoteExtension)
			return nil, err
		}

		// By default, we accept empty vote extensions.
		if len(req.VoteExtension) == 0 {
			return &cmtabci.ResponseVerifyVoteExtension{Status: cmtabci.ResponseVerifyVoteExtension_ACCEPT}, nil
		}

		// Decode the vote-extension bytes.
		voteExtension, err := codec.DecodeVoteExtension(req.VoteExtension)
		if err != nil {
			err = fmt.Errorf("%w: %w", arkabci.ErrCodec, err)

			return &cmtabci.ResponseVerifyVoteExtension{Status: cmtabci.ResponseVerifyVoteExtension_REJECT}, err
		}

		feeds, err := h.oracleKeeper.GetFeeds(ctx, req.Height)
		if err != nil {
			err = fmt.Errorf("%w: get feeds for height %d: %w", arkabci.ErrOracleKeeper, req.Height, err)
			return &cmtabci.ResponseVerifyVoteExtension{Status: cmtabci.ResponseVerifyVoteExtension_REJECT}, err
		}
		if _, validationErr := abcioracle.ValidateVoteExtension(voteExtension, feeds); validationErr != nil {
			err = fmt.Errorf("%w: %w", errVoteExtensionValidation, validationErr)
			return &cmtabci.ResponseVerifyVoteExtension{Status: cmtabci.ResponseVerifyVoteExtension_REJECT}, err
		}

		// Observe message size.
		arkmetrics.ObserveMessageSize(arkmetrics.VoteExtension, len(req.VoteExtension))

		return &cmtabci.ResponseVerifyVoteExtension{Status: cmtabci.ResponseVerifyVoteExtension_ACCEPT}, nil
	}
}

func voteExtensionStatus(err error) arkmetrics.Status {
	switch {
	case err == nil:
		return arkmetrics.StatusSuccess
	case errors.Is(err, arkabci.ErrNilRequest):
		return arkmetrics.StatusNilRequest
	case errors.Is(err, errPanic):
		return arkmetrics.StatusPanic
	case errors.Is(err, errPriceFeedClient):
		return arkmetrics.StatusPriceFeedClient
	case errors.Is(err, errInvalidPrices):
		return arkmetrics.StatusInvalidPrices
	case errors.Is(err, errVoteExtensionValidation):
		return arkmetrics.StatusVoteExtensionValidation
	case errors.Is(err, arkabci.ErrOracleKeeper):
		return arkmetrics.StatusOracleKeeper
	case errors.Is(err, arkabci.ErrCodec):
		return arkmetrics.StatusCodec
	default:
		return arkmetrics.StatusFailure
	}
}
