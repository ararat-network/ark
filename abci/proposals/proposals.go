package proposals

import (
	"bytes"
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

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
//  3. Verifying that the vote extensions injected are valid.
//
// To verify the validity of the vote extensions, the proposal handler will
// call the validateVoteExtensionsFn. This function is responsible for verifying
// that the vote extensions included in the proposal are valid and compose a
// super-majority of signatures and vote extensions for the current block.
// The given VoteExtensionCodec must be the same used by the vote extension handler,
// the extended commit is decoded in accordance with the given ExtendedCommitCodec.
type Handler struct {
	logger log.Logger

	// prepareProposalHandler fills a proposal with transactions.
	prepareProposalHandler sdk.PrepareProposalHandler

	// processProposalHandler processes transactions in a proposal.
	processProposalHandler sdk.ProcessProposalHandler

	// validateVoteExtensionsFn validates the vote extensions included in a proposal.
	validateVoteExtensionsFn ve.ValidateVoteExtensionsFn

	// voteExtensionCodec is used to decode vote extensions.
	voteExtensionCodec codec.VoteExtensionCodec

	// extendedCommitCodec is used to decode extended commit info.
	extendedCommitCodec codec.ExtendedCommitCodec

	// retainOracleDataInWrappedHandler is a flag that determines whether the
	// proposal handler should pass the injected extended commit info to the
	// wrapped proposal handler.
	retainOracleDataInWrappedHandler bool
}

// NewHandler returns a new Handler.
func NewHandler(
	logger log.Logger,
	prepareProposalHandler sdk.PrepareProposalHandler,
	processProposalHandler sdk.ProcessProposalHandler,
	validateVoteExtensionsFn ve.ValidateVoteExtensionsFn,
	voteExtensionCodec codec.VoteExtensionCodec,
	extendedCommitInfoCodec codec.ExtendedCommitCodec,
	opts ...Option,
) *Handler {
	handler := &Handler{
		logger:                   logger,
		prepareProposalHandler:   prepareProposalHandler,
		processProposalHandler:   processProposalHandler,
		validateVoteExtensionsFn: validateVoteExtensionsFn,
		voteExtensionCodec:       voteExtensionCodec,
		extendedCommitCodec:      extendedCommitInfoCodec,
	}

	// apply options
	for _, opt := range opts {
		opt(handler)
	}

	return handler
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

		// If vote extensions are enabled, the proposer must inject the previous
		// block's extended commit info into the proposal.
		voteExtensionsEnabled := ve.VoteExtensionsEnabled(ctx)
		if voteExtensionsEnabled {
			h.logger.Debug(
				"injecting oracle data into proposal",
				"height", req.Height,
				"vote_extensions_enabled", voteExtensionsEnabled,
			)

			// Get pruned ExtendedCommitInfo from LocalLastCommit.
			extInfo, err := h.PruneAndValidateExtendedCommitInfo(ctx, req.LocalLastCommit)
			if err != nil {
				h.logger.Error(
					"failed to prune extended commit info",
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
			// Adjust req.MaxTxBytes so the wrapped proposal handler does not
			// reap too many txs from the mempool.
			extInfoBzSize := int64(len(extInfoBz))
			if extInfoBzSize <= req.MaxTxBytes {
				// Reserve bytes for the vote-extension transaction.
				req.MaxTxBytes -= extInfoBzSize
			} else {
				h.logger.Error("VE size consumes greater than entire block",
					"extInfoBzSize", extInfoBzSize,
					"MaxTxBytes", req.MaxTxBytes)
				err := fmt.Errorf("VE size consumes greater than entire block: extInfoBzSize = %d: MaxTxBytes = %d", extInfoBzSize, req.MaxTxBytes)
				return &cometabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
			}

			// Determine whether the wrapped prepare proposal handler should retain the extended commit info.
			if h.retainOracleDataInWrappedHandler {
				req.Txs = append([][]byte{extInfoBz}, req.Txs...) // prepend the VE Tx
			}
		}

		// Build the proposal. Get the duration that the wrapped prepare proposal handler executed for.
		wrappedPrepareProposalStartTime := time.Now()
		resp, err = h.prepareProposalHandler(ctx, req)
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

		// Inject the vote-extension transaction, if present, and resize the response txs to respect req.MaxTxBytes.
		resp.Txs = h.injectAndResize(resp.Txs, extInfoBz, req.MaxTxBytes+int64(len(extInfoBz)))

		h.logger.Debug(
			"prepared proposal",
			"txs", len(resp.Txs),
			"vote_extensions_enabled", voteExtensionsEnabled,
		)

		return resp, nil
	}
}

// injectAndResize returns a tx array containing the injectTx at the beginning followed by appTxs.
// The returned transaction array is bounded by maxSizeBytes, and the function is idempotent meaning the
// injectTx will only appear once regardless of how many times you attempt to inject it.
// If injectTx is large enough, all originalTxs may end up being excluded from the returned tx array.
func (h *Handler) injectAndResize(appTxs [][]byte, injectTx []byte, maxSizeBytes int64) [][]byte {
	var (
		returnedTxs   [][]byte
		consumedBytes int64
	)

	// If vote extensions are enabled and the injected tx is not already first, inject it here.
	if len(injectTx) != 0 && (len(appTxs) < 1 || !bytes.Equal(appTxs[0], injectTx)) {
		injectBytes := int64(len(injectTx))
		// Ensure the injected tx is in the response if there is room. The vote
		// extension payload should be relatively stable, so MaxTxBytes should be
		// configured with enough headroom.
		if injectBytes <= maxSizeBytes {
			consumedBytes += injectBytes
			returnedTxs = append(returnedTxs, injectTx)
		}
	}
	// Add as many app txs as possible within maxSizeBytes.
	for _, tx := range appTxs {
		consumedBytes += int64(len(tx))
		if consumedBytes > maxSizeBytes {
			return returnedTxs
		}
		returnedTxs = append(returnedTxs, tx)
	}
	return returnedTxs
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

		// Save the injected tx so it can be restored if it is removed before the
		// wrapped proposal handler runs.
		var injectedTx []byte

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

			// Remove the extended commit info from the proposal if required.
			if !h.retainOracleDataInWrappedHandler {
				injectedTx = req.Txs[arkabci.OracleInfoIndex]
				req.Txs = req.Txs[arkabci.NumInjectedTxs:]
			}
		}

		// Call the wrapped process proposal handler.
		wrappedProcessProposalStartTime := time.Now()
		resp, err = h.processProposalHandler(ctx, req)
		wrappedProcessProposalLatency = time.Since(wrappedProcessProposalStartTime)
		if err != nil {
			err = arkabci.WrappedHandlerError{
				Handler: arkmetrics.ProcessProposal,
				Err:     err,
			}
		}

		if !h.retainOracleDataInWrappedHandler && injectedTx != nil {
			// Re-inject the extended commit info if it was removed before calling the wrapped handler.
			req.Txs = append([][]byte{injectedTx}, req.Txs...)
		}

		return resp, err
	}
}
