package mempool

import (
	"errors"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
)

// CheckTx and InsertTx share admission. App-mode CometBFT deliberately does not
// lock these calls. Small per-candidate queues bound work waiting for state;
// the candidate is untrusted and never grants reserved transaction storage.
func (h *Handler) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	if req == nil {
		return rejected(sdkerrors.ErrInvalidRequest.Wrap("nil CheckTx"), sdk.Context{}), nil
	}
	switch req.Type {
	case abci.CheckTxType_New:
		// Continue admission; only Commit owns pending-state revalidation.
	case abci.CheckTxType_Recheck:
		return rejected(sdkerrors.ErrNotSupported.Wrap("external recheck is unsupported; pending transactions are rechecked on commit"), sdk.Context{}), nil
	default:
		return rejected(sdkerrors.ErrInvalidRequest.Wrap("unknown CheckTx type"), sdk.Context{}), nil
	}
	if len(req.Tx) == 0 || len(req.Tx) > h.pool.config.MaxTxBytes {
		return rejected(sdkerrors.ErrTxTooLarge.Wrap("invalid transaction size"), sdk.Context{}), nil
	}

	select {
	case h.decodeSlots <- struct{}{}:
	default:
		return busy("decoder queue is full"), nil
	}
	tx, lane, err := h.decodeCandidate(req.Tx)
	if err != nil {
		return rejected(err, sdk.Context{}), nil
	}

	if !h.pool.MayFit(lane, len(req.Tx)) {
		return busy("candidate lane is full"), nil
	}
	select {
	case h.admissionSlots[lane] <- struct{}{}:
	default:
		return busy("verification queue is full"), nil
	}
	defer func() { <-h.admissionSlots[lane] }()

	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	if h.pool.Has(req.Tx) {
		return busy("transaction already pending"), nil
	}
	return h.admit(tx, req.Tx), nil
}

func (h *Handler) InsertTx(req *abci.RequestInsertTx) (*abci.ResponseInsertTx, error) {
	if req == nil {
		return &abci.ResponseInsertTx{Code: 1}, nil
	}
	res, err := h.CheckTx(&abci.RequestCheckTx{Tx: req.Tx})
	if err != nil {
		return &abci.ResponseInsertTx{Code: abci.CodeTypeRetry}, nil
	}
	// A peer may send a successor before its predecessor, for example after
	// joining between gossip batches. Non-retryable errors stay in CometBFT's
	// seen cache until eviction, preventing rebroadcast from repairing the gap.
	// Retry sequence mismatches on the peer path; RPC keeps the detailed error.
	if res.Codespace == sdkerrors.ErrWrongSequence.Codespace() && res.Code == sdkerrors.ErrWrongSequence.ABCICode() {
		return &abci.ResponseInsertTx{Code: abci.CodeTypeRetry}, nil
	}
	return &abci.ResponseInsertTx{Code: res.Code}, nil
}

// admit runs while admissionMu is held and after duplicate rejection. RunTx
// inserts into this handler's pool; its ante writes remain in the outer cache
// until the complete SDK path succeeds, including any post-handler.
func (h *Handler) admit(tx sdk.Tx, bz []byte) *abci.ResponseCheckTx {
	base := h.app.GetContextForCheckTx(bz)
	if err := pendingLimits(base, tx, bz); err != nil {
		return rejected(err, sdk.Context{})
	}
	branch := base.MultiStore().CacheMultiStore()
	committed := false
	defer func() {
		// A late SDK error can occur after Pool.Insert. Discard both the entry and
		// the state branch. The duplicate guard and lifecycle lock make this entry
		// exclusively ours; existing pending transactions cannot be removed here.
		if !committed {
			h.pool.RemoveBytes(bz)
		}
	}()
	gas, result, events, err := h.app.RunTx(sdk.ExecModeCheck, bz, tx, -1, branch, nil)
	if err != nil {
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrDuplicate) {
			return busy(err.Error())
		}
		abcimetrics.RecordAdmission("invalid")
		return sdkerrors.ResponseCheckTxWithEvents(err, gas.GasWanted, gas.GasUsed, events, false)
	}
	branch.Write()
	committed = true
	abcimetrics.RecordPool(h.pool.Usage())
	abcimetrics.RecordAdmission("accepted")
	return &abci.ResponseCheckTx{
		GasWanted: int64(gas.GasWanted),
		GasUsed:   int64(gas.GasUsed),
		Log:       result.Log,
		Data:      result.Data,
		Events:    sdk.MarkEventsToIndex(result.Events, nil),
	}
}

func (h *Handler) recheck(e Entry) *abci.ResponseCheckTx {
	base := h.app.GetContextForCheckTx(e.Bytes).WithIsReCheckTx(true)
	ctx, write, err := h.validatePending(base, e.Tx, e.Bytes)
	if err == nil {
		e.Lane, e.Fee = FromContext(ctx), ctx.Priority()
		err = h.pool.insert(e)
	}
	if err != nil {
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrDuplicate) {
			return busy(err.Error())
		}
		return rejected(err, ctx)
	}
	write()
	abcimetrics.RecordAdmission("accepted")
	return &abci.ResponseCheckTx{
		GasWanted: int64(ctx.GasMeter().Limit()),
		GasUsed:   int64(ctx.GasMeter().GasConsumed()),
	}
}

// validatePending handles recheck and proposal validation on a disposable branch.
// These paths need the returned ante context to refresh authenticated lane and fee.
// Fresh admission delegates transaction execution to SDK RunTx instead.
// It mirrors BaseApp.RunTx (cosmos-sdk v0.54.3, baseapp/baseapp.go) up to
// message execution: getContextForTx's per-tx context, validateBasicTxMsgs
// skipped on recheck, a router handler per message, ante on a branch, then
// the post chain on a discarded branch with success set, as runMsgs reports
// outside finalisation. Re-diff against RunTx on an SDK bump.
// Only the caller may write successful ante changes, after reserving storage or
// block resources. Tax collection belongs to message execution, not admission.
func (h *Handler) validatePending(base sdk.Context, tx sdk.Tx, bz []byte) (ctx sdk.Context, write func(), err error) {
	// The signature verification setting rides on the supplied context.
	ctx = base.WithTxBytes(bz).WithTxIndex(-1).WithGasMeter(storetypes.NewInfiniteGasMeter())
	ctx, write = ctx.CacheContext()
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case storetypes.ErrorOutOfGas, storetypes.ErrorGasOverflow:
				err = sdkerrors.ErrOutOfGas.Wrap(fmt.Sprint(r))
			default:
				err = sdkerrors.ErrPanic.Wrap(fmt.Sprint(r))
			}
		}
	}()
	if len(tx.GetMsgs()) == 0 {
		return ctx, nil, sdkerrors.ErrInvalidRequest.Wrap("transaction has no messages")
	}
	for _, msg := range tx.GetMsgs() {
		// Stateless checks passed at admission; the SDK skips them on recheck.
		if basic, ok := msg.(sdk.HasValidateBasic); ok && !ctx.IsReCheckTx() {
			if err := basic.ValidateBasic(); err != nil {
				return ctx, nil, err
			}
		}
		if h.app.MsgServiceRouter().Handler(msg) == nil {
			return ctx, nil, sdkerrors.ErrUnknownRequest.Wrapf("no handler for %T", msg)
		}
	}
	if _, err := tx.GetMsgsV2(); err != nil {
		return ctx, nil, err
	}
	if err := pendingLimits(ctx, tx, bz); err != nil {
		return ctx, nil, err
	}

	ctx, err = h.app.AnteHandler()(ctx, tx, false)
	if err != nil {
		return ctx, nil, err
	}
	if h.post != nil {
		postCtx, _ := ctx.CacheContext()
		if _, err := h.post(postCtx.WithEventManager(sdk.NewEventManager()), tx, false, true); err != nil {
			return ctx, nil, err
		}
	}
	return ctx, write, nil
}

// decodeCandidate inspects untrusted input outside SDK RunTx's recovery boundary.
// The caller reserves a decoder slot; release it even if decoding or GetMsgs panics.
func (h *Handler) decodeCandidate(bz []byte) (tx sdk.Tx, lane int8, err error) {
	defer func() {
		<-h.decodeSlots
		if r := recover(); r != nil {
			err = sdkerrors.ErrPanic.Wrap(fmt.Sprint(r))
		}
	}()
	tx, err = h.decode(bz)
	if err != nil {
		return nil, LaneNormal, sdkerrors.ErrTxDecode.Wrap(err.Error())
	}
	return tx, h.privileges.CandidateLane(tx), nil
}

// pendingLimits rejects transactions that cannot fit the configured block.
// These local admission limits supplement the SDK's transaction validation.
func pendingLimits(ctx sdk.Context, tx sdk.Tx, bz []byte) (err error) {
	// Fresh admission inspects GetGas before entering SDK RunTx. Keep recovery
	// local to this inspection, without catching store or lifecycle failures.
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case storetypes.ErrorOutOfGas, storetypes.ErrorGasOverflow:
				err = sdkerrors.ErrOutOfGas.Wrap(fmt.Sprint(r))
			default:
				err = sdkerrors.ErrPanic.Wrap(fmt.Sprint(r))
			}
		}
	}()
	gasTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return sdkerrors.ErrTxDecode.Wrap("transaction has no gas limit")
	}
	if block := ctx.ConsensusParams().Block; block != nil {
		if block.MaxGas > 0 && gasTx.GetGas() > uint64(block.MaxGas) {
			return sdkerrors.ErrOutOfGas.Wrap("declared gas exceeds block limit")
		}
		if block.MaxBytes > 0 && int64(len(bz)) > block.MaxBytes {
			return sdkerrors.ErrTxTooLarge.Wrap("transaction exceeds block byte limit")
		}
	}
	return nil
}

func busy(message string) *abci.ResponseCheckTx {
	abcimetrics.RecordAdmission("retry")
	return &abci.ResponseCheckTx{
		Code:      abci.CodeTypeRetry,
		Codespace: "lanes",
		Log:       message,
	}
}

func rejected(err error, ctx sdk.Context) *abci.ResponseCheckTx {
	abcimetrics.RecordAdmission("invalid")
	var wanted, used uint64
	if !ctx.IsZero() && ctx.GasMeter() != nil {
		wanted = ctx.GasMeter().Limit()
		used = ctx.GasMeter().GasConsumed()
	}
	return sdkerrors.ResponseCheckTxWithEvents(err, wanted, used, nil, false)
}
