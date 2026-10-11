package proposals

import (
	"fmt"
	"time"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	abcitypes "github.com/ararat-network/ark/abci/types"
	"github.com/ararat-network/ark/abci/voteextension"
)

// Handler wraps transaction proposal selection and validation with extended
// commit injection, consensus matching, and signature-quorum authentication.
type Handler struct {
	// prepareProposalHandler fills a proposal with transactions.
	prepareProposalHandler sdk.PrepareProposalHandler

	// processProposalHandler processes transactions in a proposal.
	processProposalHandler sdk.ProcessProposalHandler

	// validatorStore resolves consensus public keys used to authenticate vote extensions.
	validatorStore baseapp.ValidatorStore

	// oracleParams supplies the freshness limit a proposal's prices are held to.
	oracleParams abcitypes.OracleParamsReader
}

// NewHandler returns a new Handler.
func NewHandler(
	prepareProposalHandler sdk.PrepareProposalHandler,
	processProposalHandler sdk.ProcessProposalHandler,
	validatorStore baseapp.ValidatorStore,
	oracleParams abcitypes.OracleParamsReader,
) *Handler {
	return &Handler{
		prepareProposalHandler: prepareProposalHandler,
		processProposalHandler: processProposalHandler,
		validatorStore:         validatorStore,
		oracleParams:           oracleParams,
	}
}

// PrepareProposalHandler reserves extended-commit bytes before transaction
// selection and prepends the commit to the result when extensions are enabled.
// It fails if the commit exceeds MaxTxBytes; selector errors, and prices older
// than the oracle's freshness limit, yield a commit-only proposal when
// extensions are enabled.
func (h *Handler) PrepareProposalHandler() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *cmtabci.RequestPrepareProposal) (resp *cmtabci.ResponsePrepareProposal, err error) {
		start := time.Now()
		completed := false
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
			status := abcimetrics.StatusOf(statusErr)
			if !completed {
				status = abcimetrics.StatusPanic
			}
			abcimetrics.RecordLatencyAndStatus(
				totalLatency-wrappedPrepareProposalLatency,
				status,
				abcimetrics.PrepareProposal,
			)
		}()

		resp, err = func() (resp *cmtabci.ResponsePrepareProposal, err error) {
			if req == nil {
				err = fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.PrepareProposal)
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

					return &cmtabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
				}

				// Encode the extended commit info that will be injected into the
				// proposal and applied in PreBlock.
				extInfoBz, err = codec.EncodeExtendedCommit(extInfo)
				if err != nil {
					err = fmt.Errorf("%w: %w", abcitypes.ErrCodec, err)

					return &cmtabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
				}
				// Give the wrapped handler only the remaining protobuf transaction
				// budget. Use a request copy so this wrapper does not mutate its input.
				// On any error here baseapp proposes CometBFT's raw transaction list
				// instead, which ProcessProposal rejects for want of the commit, so the
				// round is lost. The ante's block data budget floor keeps governance
				// from making this branch permanent.
				extInfoBzSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{extInfoBz})
				if extInfoBzSize > req.MaxTxBytes {
					err = fmt.Errorf(
						"extended commit transaction size %d exceeds MaxTxBytes %d",
						extInfoBzSize,
						req.MaxTxBytes,
					)
					return &cmtabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
				}

				wrappedReqCopy := *req
				wrappedReqCopy.MaxTxBytes -= extInfoBzSize
				wrappedReq = &wrappedReqCopy

				// The block applies prices observed as the previous block committed and carries
				// that commit's median time, so after a stall both predate it while its
				// transactions do not. Past the oracle's freshness limit by this proposer's clock,
				// the block carries prices only and transactions trade on the next block's.
				age, limit, ageErr := h.priceAge(ctx, req.Time)
				if ageErr != nil || age > limit {
					statusErr = errCommitOnly
					if ageErr != nil {
						statusErr = fmt.Errorf("%w: %w", errCommitOnly, ageErr)
					}
					if logger := ctx.Logger(); logger != nil {
						if ageErr != nil {
							logger.Error("cannot age proposal prices; using commit-only proposal",
								"height", req.Height, "err", ageErr)
						} else {
							logger.Warn("proposal prices older than the oracle's freshness limit; using commit-only proposal",
								"height", req.Height, "age_seconds", age, "limit_seconds", limit)
						}
					}

					return &cmtabci.ResponsePrepareProposal{Txs: [][]byte{extInfoBz}}, nil
				}
			}

			// Build the proposal. Get the duration that the wrapped prepare proposal handler executed for.
			wrappedPrepareProposalStartTime := time.Now()
			resp, err = h.prepareProposalHandler(ctx, wrappedReq)
			wrappedPrepareProposalLatency = time.Since(wrappedPrepareProposalStartTime)
			if err != nil {
				err = fmt.Errorf("%w for %s: %w", abcitypes.ErrWrappedHandler, abcimetrics.PrepareProposal, err)
				if !voteExtensionsEnabled {
					return &cmtabci.ResponsePrepareProposal{Txs: make([][]byte, 0)}, err
				}

				statusErr = err
				if logger := ctx.Logger(); logger != nil {
					logger.Error(
						"wrapped prepare proposal failed; using commit-only proposal",
						"height", req.Height,
						"err", err,
					)
				}

				// Preserve authenticated oracle reports when transaction selection fails.
				return &cmtabci.ResponsePrepareProposal{Txs: [][]byte{extInfoBz}}, nil
			}

			// The wrapped handler was budgeted without the extended commit, so prepend
			// it exactly once after the handler returns.
			if voteExtensionsEnabled {
				resp.Txs = append([][]byte{extInfoBz}, resp.Txs...)
			}

			return resp, nil
		}()
		completed = true
		return resp, err
	}
}

// priceAge is how old, by this node's clock in whole seconds, the prices a proposal applies are,
// dated by the proposal's time, against the oracle's freshness limit. A clock behind the proposal
// reads age zero.
func (h *Handler) priceAge(ctx sdk.Context, proposalTime time.Time) (age, limit uint64, err error) {
	params, err := h.oracleParams.GetParams(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("reading oracle params: %w", err)
	}
	age = uint64(max(time.Now().Unix()-proposalTime.Unix(), 0))
	return age, params.MaxExchangeRateAge, nil
}

// ProcessProposalHandler returns a ProcessProposalHandler that will be called
// by base app when a new block proposal needs to be verified. The ProcessProposalHandler
// will verify that the vote extensions included in the proposal are valid and compose
// a super-majority of signatures and vote extensions for the current block.
func (h *Handler) ProcessProposalHandler() sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *cmtabci.RequestProcessProposal) (resp *cmtabci.ResponseProcessProposal, err error) {
		start := time.Now()
		completed := false
		var wrappedProcessProposalLatency time.Duration

		// Defer a function to record the total time it took to process the proposal.
		defer func() {
			totalLatency := time.Since(start)
			status := abcimetrics.StatusOf(err)
			if err == nil && resp != nil && resp.Status == cmtabci.ResponseProcessProposal_REJECT {
				status = abcimetrics.StatusRejected
			}
			if !completed {
				status = abcimetrics.StatusPanic
			}
			abcimetrics.RecordLatencyAndStatus(
				totalLatency-wrappedProcessProposalLatency,
				status,
				abcimetrics.ProcessProposal,
			)
		}()

		resp, err = func() (resp *cmtabci.ResponseProcessProposal, err error) {
			if req == nil {
				err = fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.ProcessProposal)
				return nil, err
			}

			wrappedReq := req
			voteExtensionsEnabled := voteextension.VoteExtensionsAvailable(ctx)
			if voteExtensionsEnabled {
				// Ensure that the commit info was correctly injected into the proposal.
				if len(req.Txs) < abcitypes.NumInjectedTxs {
					err = abcitypes.ErrMissingCommitInfo
					return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_REJECT},
						err
				}

				extCommitBz := req.Txs[abcitypes.OracleInfoIndex]

				// Validate the vote extensions included in the proposal.
				var extInfo cmtabci.ExtendedCommitInfo
				expectedVotes := ctx.CometInfo().GetLastCommit().Votes().Len()
				extInfo, err = codec.DecodeExtendedCommit(extCommitBz, expectedVotes)
				if err != nil {
					err = fmt.Errorf("%w: %w", abcitypes.ErrCodec, err)
					return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_REJECT},
						err
				}

				if err = voteextension.ValidateExtendedCommit(ctx, h.validatorStore, extInfo); err != nil {
					err = fmt.Errorf("%w: %w", ErrExtendedCommitValidation, err)
					return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_REJECT},
						err
				}

				// The injected commit is protocol metadata, not an SDK transaction.
				// Remove it from a request copy before invoking the wrapped handler.
				wrappedReqCopy := *req
				wrappedReqCopy.Txs = req.Txs[abcitypes.NumInjectedTxs:]
				wrappedReq = &wrappedReqCopy
			}

			// Call the wrapped process proposal handler.
			wrappedProcessProposalStartTime := time.Now()
			resp, err = h.processProposalHandler(ctx, wrappedReq)
			wrappedProcessProposalLatency = time.Since(wrappedProcessProposalStartTime)
			if err != nil {
				err = fmt.Errorf("%w for %s: %w", abcitypes.ErrWrappedHandler, abcimetrics.ProcessProposal, err)
			}

			return resp, err
		}()
		completed = true
		return resp, err
	}
}
