package proposals

import (
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/codec"
	arkmetrics "ark/abci/metrics"
	arkabci "ark/abci/types"
	"ark/abci/ve"
)

// Handler is responsible primarily for:
//  1. Filling a proposal with transactions.
//  2. Injecting vote extensions into the proposal (if vote extensions are enabled).
//  3. Verifying that the injected extended commit is complete and authenticated.
//
// The proposal handler calls validateVoteExtensionsFn to verify that the
// injected commit matches consensus and contains a super-majority of valid
// vote-extension signatures for the current block.
// The extended commit is decoded in accordance with the given ExtendedCommitCodec.
type Handler struct {
	logger log.Logger

	// prepareProposalHandler fills a proposal with transactions.
	prepareProposalHandler sdk.PrepareProposalHandler

	// processProposalHandler processes transactions in a proposal.
	processProposalHandler sdk.ProcessProposalHandler

	// validateVoteExtensionsFn validates the vote extensions included in a proposal.
	validateVoteExtensionsFn ve.ValidateVoteExtensionsFn

	// extendedCommitCodec is used to decode extended commit info.
	extendedCommitCodec codec.ExtendedCommitCodec
}

// NewHandler returns a new Handler.
func NewHandler(
	logger log.Logger,
	prepareProposalHandler sdk.PrepareProposalHandler,
	processProposalHandler sdk.ProcessProposalHandler,
	validateVoteExtensionsFn ve.ValidateVoteExtensionsFn,
	extendedCommitInfoCodec codec.ExtendedCommitCodec,
) *Handler {
	return &Handler{
		logger:                   logger,
		prepareProposalHandler:   prepareProposalHandler,
		processProposalHandler:   processProposalHandler,
		validateVoteExtensionsFn: validateVoteExtensionsFn,
		extendedCommitCodec:      extendedCommitInfoCodec,
	}
}

// PrepareProposalHandler returns a PrepareProposalHandler that will be called
// by base app when a new block proposal is requested. The PrepareProposalHandler
// will first fill the proposal with transactions. Then, if vote extensions are
// enabled, the handler will inject the extended commit info into the proposal.
// If the size of the vote extensions exceeds the request's MaxTxBytes size, this
// handler will fail.
func (h *Handler) PrepareProposalHandler() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *cometabci.RequestPrepareProposal) (resp *cometabci.ResponsePrepareProposal, err error) {
		start := time.Now()
		var (
			extInfoBz                     []byte
			wrappedPrepareProposalLatency time.Duration
		)

		// Report the PrepareProposal latency excluding the wrapped handler.
		defer func() {
			totalLatency := time.Since(start)
			arkmetrics.RecordLatencyAndStatus(totalLatency-wrappedPrepareProposalLatency, err, arkmetrics.PrepareProposal)
		}()

		if req == nil {
			h.logger.Error("PrepareProposalHandler received a nil request")
			err = arkabci.NilRequestError{
				Handler: arkmetrics.PrepareProposal,
			}
			return nil, err
		}
		wrappedReq := req

		// If vote extensions are enabled, the proposer must inject the previous
		// block's extended commit info into the proposal.
		voteExtensionsEnabled := ve.VoteExtensionsEnabled(ctx)
		if voteExtensionsEnabled {
			h.logger.Debug(
				"injecting oracle data into proposal",
				"height", req.Height,
				"vote_extensions_enabled", voteExtensionsEnabled,
			)

			// Preserve and validate the consensus-provided extended commit. Payload
			// classification happens later in preblock; the proposer must not erase
			// or rewrite authenticated reports.
			extInfo := req.LocalLastCommit
			if err := h.ValidateExtendedCommitInfo(ctx, req.Height, extInfo); err != nil {
				h.logger.Error(
					"failed to validate extended commit info",
					"height", req.Height,
					"local_last_commit", req.LocalLastCommit,
					"err", err,
				)

				err = InvalidExtendedCommitInfoError{
					Err: err,
				}

				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}

			// Encode the extended commit info that will be injected into the
			// proposal and applied in PreBlock.
			extInfoBz, err = h.extendedCommitCodec.Encode(extInfo)
			if err != nil {
				h.logger.Error(
					"failed to extended commit info",
					"commit_info", extInfo,
					"err", err,
				)
				err = arkabci.CodecError{
					Err: err,
				}

				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}
			// Give the wrapped handler only the remaining protobuf transaction
			// budget. Use a request copy so this wrapper does not mutate its input.
			extInfoBzSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{extInfoBz})
			if extInfoBzSize > req.MaxTxBytes {
				h.logger.Error("VE size consumes greater than entire block",
					"extInfoBzSize", extInfoBzSize,
					"MaxTxBytes", req.MaxTxBytes)
				err := fmt.Errorf("VE size consumes greater than entire block: extInfoBzSize = %d: MaxTxBytes = %d", extInfoBzSize, req.MaxTxBytes)
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
			h.logger.Error("failed to prepare proposal", "err", err)
			err = arkabci.WrappedHandlerError{
				Handler: arkmetrics.PrepareProposal,
				Err:     err,
			}

			return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
		}
		h.logger.Debug("wrapped prepareProposalHandler produced response ", "txs", len(resp.Txs))

		// The wrapped handler was budgeted without the extended commit, so prepend
		// it exactly once after the handler returns.
		if voteExtensionsEnabled {
			resp.Txs = append([][]byte{extInfoBz}, resp.Txs...)
		}

		h.logger.Debug(
			"prepared proposal",
			"txs", len(resp.Txs),
			"vote_extensions_enabled", voteExtensionsEnabled,
		)

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
			arkmetrics.RecordLatencyAndStatus(totalLatency-wrappedProcessProposalLatency, err, arkmetrics.ProcessProposal)
		}()

		// this should never happen, but just in case
		if req == nil {
			h.logger.Error("ProcessProposalHandler received a nil request")
			err = arkabci.NilRequestError{
				Handler: arkmetrics.ProcessProposal,
			}
			return nil, err
		}

		voteExtensionsEnabled := ve.VoteExtensionsEnabled(ctx)

		h.logger.Debug(
			"processing proposal",
			"height", req.Height,
			"num_txs", len(req.Txs),
			"vote_extensions_enabled", voteExtensionsEnabled,
		)

		wrappedReq := req

		if voteExtensionsEnabled {
			// Ensure that the commit info was correctly injected into the proposal.
			if len(req.Txs) < arkabci.NumInjectedTxs {
				h.logger.Error("failed to process proposal: missing commit info", "num_txs", len(req.Txs))
				err = arkabci.MissingCommitInfoError{}
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT},
					err
			}

			extCommitBz := req.Txs[arkabci.OracleInfoIndex]

			// Validate the vote extensions included in the proposal.
			var extInfo cometabci.ExtendedCommitInfo
			extInfo, err = h.extendedCommitCodec.Decode(extCommitBz)
			if err != nil {
				h.logger.Error("failed to unmarshal commit info", "err", err)
				err = arkabci.CodecError{
					Err: err,
				}
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT},
					err
			}

			if err := h.ValidateExtendedCommitInfo(ctx, req.Height, extInfo); err != nil {
				h.logger.Error(
					"failed to validate vote extensions",
					"height", req.Height,
					"commit_info", extInfo,
					"err", err,
				)
				err = InvalidExtendedCommitInfoError{
					Err: err,
				}

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
			err = arkabci.WrappedHandlerError{
				Handler: arkmetrics.ProcessProposal,
				Err:     err,
			}
		}

		return resp, err
	}
}
