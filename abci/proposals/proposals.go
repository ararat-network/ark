package proposals

import (
	"errors"
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	arkmetrics "github.com/ararat-network/ark/abci/metrics"
	arkabci "github.com/ararat-network/ark/abci/types"
	"github.com/ararat-network/ark/abci/voteextension"
)

// Handler is responsible primarily for:
//  1. Filling a proposal with transactions.
//  2. Injecting vote extensions into the proposal (if vote extensions are enabled).
//  3. Verifying that the injected extended commit is complete and authenticated.
//
// The proposal handler validates that the injected commit matches consensus and
// contains a super-majority of valid vote-extension signatures for the current block.
type Handler struct {
	// prepareProposalHandler fills a proposal with transactions.
	prepareProposalHandler sdk.PrepareProposalHandler

	// processProposalHandler processes transactions in a proposal.
	processProposalHandler sdk.ProcessProposalHandler

	// validatorStore resolves consensus public keys used to authenticate vote extensions.
	validatorStore baseapp.ValidatorStore
}

// NewHandler returns a new Handler.
func NewHandler(
	prepareProposalHandler sdk.PrepareProposalHandler,
	processProposalHandler sdk.ProcessProposalHandler,
	validatorStore baseapp.ValidatorStore,
) *Handler {
	return &Handler{
		prepareProposalHandler: prepareProposalHandler,
		processProposalHandler: processProposalHandler,
		validatorStore:         validatorStore,
	}
}

// PrepareProposalHandler returns a PrepareProposalHandler that will be called
// by base app when a new block proposal is requested. The PrepareProposalHandler
// will first fill the proposal with transactions. Then, if vote extensions are
// enabled, the handler will inject the extended commit info into the proposal.
// If the protobuf-encoded extended commit exceeds the request's MaxTxBytes
// budget, this handler will fail.
func (h *Handler) PrepareProposalHandler() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *cometabci.RequestPrepareProposal) (resp *cometabci.ResponsePrepareProposal, err error) {
		start := time.Now()
		var (
			extInfoBz                     []byte
			statusErr                     error
			wrappedPrepareProposalLatency time.Duration
		)

		// Report the PrepareProposal latency excluding the wrapped handler.
		defer func() {
			totalLatency := time.Since(start)
			if statusErr == nil {
				statusErr = err
			}
			arkmetrics.RecordLatencyAndStatus(
				totalLatency-wrappedPrepareProposalLatency,
				proposalStatus(statusErr),
				arkmetrics.PrepareProposal,
			)
		}()

		if req == nil {
			err = fmt.Errorf("%w for %s", arkabci.ErrNilRequest, arkmetrics.PrepareProposal)
			return nil, err
		}

		wrappedReq := req
		// If vote extensions are enabled, the proposer must inject the previous
		// block's extended commit info into the proposal.
		voteExtensionsEnabled := voteextension.VoteExtensionsAvailable(ctx)
		if voteExtensionsEnabled {
			// Preserve and validate the consensus-provided extended commit. Payload
			// classification happens later in preblock; the proposer must not erase
			// or rewrite authenticated reports.
			extInfo := req.LocalLastCommit
			if err = voteextension.ValidateExtendedCommit(ctx, h.validatorStore, extInfo); err != nil {
				err = fmt.Errorf("%w: %w", ErrExtendedCommitValidation, err)

				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}

			// Encode the extended commit info that will be injected into the
			// proposal and applied in PreBlock.
			extInfoBz, err = codec.EncodeExtendedCommit(extInfo)
			if err != nil {
				err = fmt.Errorf("%w: %w", arkabci.ErrCodec, err)

				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}
			// Give the wrapped handler only the remaining protobuf transaction
			// budget. Use a request copy so this wrapper does not mutate its input.
			extInfoBzSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{extInfoBz})
			if extInfoBzSize > req.MaxTxBytes {
				err = fmt.Errorf(
					"extended commit transaction size %d exceeds MaxTxBytes %d",
					extInfoBzSize,
					req.MaxTxBytes,
				)
				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}

			wrappedReqCopy := *req
			wrappedReqCopy.MaxTxBytes -= extInfoBzSize
			wrappedReq = &wrappedReqCopy
		}

		// Build the proposal. Get the duration that the wrapped prepare proposal handler executed for.
		wrappedPrepareProposalStartTime := time.Now()
		resp, err = h.prepareProposalHandler(ctx, wrappedReq)
		wrappedPrepareProposalLatency = time.Since(wrappedPrepareProposalStartTime)
		if err != nil {
			err = fmt.Errorf("%w for %s: %w", arkabci.ErrWrappedHandler, arkmetrics.PrepareProposal, err)
			if !voteExtensionsEnabled {
				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}

			statusErr = err
			if logger := ctx.Logger(); logger != nil {
				logger.Error(
					"wrapped prepare proposal failed; using commit-only proposal",
					"height", req.Height,
					"err", err,
				)
			}

			return &cometabci.ResponsePrepareProposal{Txs: [][]byte{extInfoBz}}, nil
		}

		// The wrapped handler was budgeted without the extended commit, so prepend
		// it exactly once after the handler returns.
		if voteExtensionsEnabled {
			resp.Txs = append([][]byte{extInfoBz}, resp.Txs...)
		}

		return resp, nil
	}
}

// ProcessProposalHandler returns a ProcessProposalHandler that will be called
// by base app when a new block proposal needs to be verified. The ProcessProposalHandler
// will verify that the vote extensions included in the proposal are valid and compose
// a super-majority of signatures and vote extensions for the current block.
func (h *Handler) ProcessProposalHandler() sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *cometabci.RequestProcessProposal) (resp *cometabci.ResponseProcessProposal, err error) {
		start := time.Now()
		var wrappedProcessProposalLatency time.Duration

		// Defer a function to record the total time it took to process the proposal.
		defer func() {
			totalLatency := time.Since(start)
			arkmetrics.RecordLatencyAndStatus(
				totalLatency-wrappedProcessProposalLatency,
				proposalStatus(err),
				arkmetrics.ProcessProposal,
			)
		}()

		// this should never happen, but just in case
		if req == nil {
			err = fmt.Errorf("%w for %s", arkabci.ErrNilRequest, arkmetrics.ProcessProposal)
			return nil, err
		}

		wrappedReq := req
		voteExtensionsEnabled := voteextension.VoteExtensionsAvailable(ctx)
		if voteExtensionsEnabled {
			// Ensure that the commit info was correctly injected into the proposal.
			if len(req.Txs) < arkabci.NumInjectedTxs {
				err = arkabci.ErrMissingCommitInfo
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT},
					err
			}

			extCommitBz := req.Txs[arkabci.OracleInfoIndex]

			// Validate the vote extensions included in the proposal.
			var extInfo cometabci.ExtendedCommitInfo
			expectedVotes := ctx.CometInfo().GetLastCommit().Votes().Len()
			extInfo, err = codec.DecodeExtendedCommit(extCommitBz, expectedVotes)
			if err != nil {
				err = fmt.Errorf("%w: %w", arkabci.ErrCodec, err)
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT},
					err
			}

			if err = voteextension.ValidateExtendedCommit(ctx, h.validatorStore, extInfo); err != nil {
				err = fmt.Errorf("%w: %w", ErrExtendedCommitValidation, err)
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT},
					err
			}

			// Observe the size of the extended commit info.
			arkmetrics.ObserveMessageSize(arkmetrics.ExtendedCommit, len(extCommitBz))

			// The injected commit is protocol metadata, not an SDK transaction.
			// Remove it from a request copy before invoking the wrapped handler.
			wrappedReqCopy := *req
			wrappedReqCopy.Txs = req.Txs[arkabci.NumInjectedTxs:]
			wrappedReq = &wrappedReqCopy
		}

		// Call the wrapped process proposal handler.
		wrappedProcessProposalStartTime := time.Now()
		resp, err = h.processProposalHandler(ctx, wrappedReq)
		wrappedProcessProposalLatency = time.Since(wrappedProcessProposalStartTime)
		if err != nil {
			err = fmt.Errorf("%w for %s: %w", arkabci.ErrWrappedHandler, arkmetrics.ProcessProposal, err)
		}

		return resp, err
	}
}

func proposalStatus(err error) arkmetrics.Status {
	switch {
	case err == nil:
		return arkmetrics.StatusSuccess
	case errors.Is(err, arkabci.ErrNilRequest):
		return arkmetrics.StatusNilRequest
	case errors.Is(err, arkabci.ErrWrappedHandler):
		return arkmetrics.StatusWrappedHandler
	case errors.Is(err, ErrExtendedCommitValidation):
		return arkmetrics.StatusExtendedCommitValidation
	case errors.Is(err, arkabci.ErrCodec):
		return arkmetrics.StatusCodec
	case errors.Is(err, arkabci.ErrMissingCommitInfo):
		return arkmetrics.StatusMissingCommitInfo
	default:
		return arkmetrics.StatusFailure
	}
}
