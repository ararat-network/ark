package ante

import (
	"errors"
	stdmath "math"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"

	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// useBaseFeeChecker gates the consensus base fee, and no genesis can stand in
// for it: Validate refuses a zero MinBaseGasPrice, the controller floors the
// live price at it every block, and the requirement ceils to at least one base
// unit, so the gate is unsatisfiable-free by construction.
var useBaseFeeChecker = true

// SetBaseFeeChecker switches the base-fee gate, and false disables it
// outright. It exists for the simulation harness, which draws every
// transaction's fee uniformly from the sender's spendable balance and cannot
// be told what a transaction owes: it spends senders down to an empty balance
// and then offers no fee at all, which no base fee can accept. The Hub
// switches its own fee market off for the same reason. Never call it outside
// a test.
func SetBaseFeeChecker(enabled bool) {
	useBaseFeeChecker = enabled
}

// baseFeeChecker is the gate the ante chain installs, or nil once a test has
// switched it off — which leaves the SDK's node-local check, inert outside
// CheckTx, so a fee is still deducted but none is required.
func baseFeeChecker(treasury *treasurykeeper.Keeper) sdkante.TxFeeChecker {
	if !useBaseFeeChecker {
		return nil
	}
	return NewBaseFeeChecker(treasury)
}

// NewBaseFeeChecker prices gas against Treasury's consensus base fee. A fee
// satisfies the gate when any single denomination of it covers its own
// requirement — ceil(BaseGasPrice × gas limit × factor) — over the accepted
// set: the reference denom, factor-table members, and NOAH once its cross
// exists. The SDK skips the checker under simulation; height zero is waived
// on the stability decorator's terms, because gentxs carry no fees.
//
// Priority is the whole fee converted to reference units per gas unit, so
// the auction ranks real value whatever denomination paid it. The
// requirement is deliberately not subtracted: nothing is burned, so what a
// transaction pays per gas is what the block earns from it. A rank measured
// above the floor would also date, because priority is assigned once at the
// first CheckTx and never revisited — a tip scored against a cheap block's
// base fee would outrank an identical payment made under congestion.
func NewBaseFeeChecker(treasury *treasurykeeper.Keeper) sdkante.TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return nil, 0, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
		}
		fee := feeTx.GetFee()
		if ctx.BlockHeight() == 0 {
			return fee, 0, nil
		}

		gas := feeTx.GetGas()
		params, err := treasury.Params.Get(ctx)
		if err != nil {
			return nil, 0, err
		}
		price, err := treasury.BaseGasPrice.Get(ctx)
		if err != nil {
			return nil, 0, err
		}
		// A zero requirement admits any fee. The reference requirement is
		// ceil(price × gas) — zero exactly when the price or the gas limit
		// is, and at least one base unit otherwise.
		if price.IsZero() || gas == 0 {
			return fee, 0, nil
		}

		for _, coin := range fee {
			required, factor, err := treasury.GetRequiredGasFee(ctx, params, price, gas, coin.Denom)
			if err != nil {
				// No gas factor means this denomination cannot satisfy the
				// gate; another coin in the fee still may.
				if errors.Is(err, collections.ErrNotFound) {
					continue
				}
				return nil, 0, err
			}
			if coin.Amount.LT(required.Amount) {
				continue
			}

			if !factor.IsPositive() {
				// Unreachable: every stored factor is written positive. Kept
				// so the division below can never be reached by zero.
				return nil, 0, errorsmod.Wrapf(
					sdkerrors.ErrInvalidCoins,
					"non-positive gas factor for %s",
					coin.Denom,
				)
			}
			value := math.LegacyNewDecFromInt(coin.Amount).
				Quo(factor).
				TruncateInt()
			return fee, gasPriority(value, gas), nil
		}

		// Only refusal derives the reference figure it quotes — identity
		// factor, so it cannot miss.
		reference, _, err := treasury.GetRequiredGasFee(ctx, params, price, gas, params.ReferenceDenom)
		if err != nil {
			return nil, 0, err
		}
		return nil, 0, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"base fee requires %s or its equivalent in an accepted fee denomination, got %s",
			reference,
			fee,
		)
	}
}

// gasPriority ranks a transaction by the reference value it pays per gas
// unit, saturating at the int64 ceiling rather than overflowing on an
// absurd fee.
func gasPriority(value math.Int, gas uint64) int64 {
	if gas == 0 {
		return 0
	}
	perGas := value.Quo(math.NewIntFromUint64(gas))
	if !perGas.IsInt64() {
		return stdmath.MaxInt64
	}
	return perGas.Int64()
}

// GasTallyDecorator records each transaction's declared gas for the base-fee
// controller. Block execution only: CheckTx sees mempool traffic rather
// than the block, and a transaction failing later in the ante reverts the
// write with everything else, so the tally reads as the block's paid-for
// gas. Simulation tallies too, onto its discarded cache, so the estimate
// carries the write.
type GasTallyDecorator struct {
	treasury *treasurykeeper.Keeper
}

func NewGasTallyDecorator(treasury *treasurykeeper.Keeper) GasTallyDecorator {
	return GasTallyDecorator{treasury: treasury}
}

func (d GasTallyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Genesis transactions are not block traffic and must not seed the first
	// block's tally. Baseapp happens not to stamp ExecModeFinalize on the
	// InitChain context, but the height gate is the rule.
	if (ctx.ExecMode() == sdk.ExecModeFinalize || simulate) && ctx.BlockHeight() != 0 {
		if feeTx, ok := tx.(sdk.FeeTx); ok {
			if err := d.treasury.TallyBlockGas(ctx, feeTx.GetGas()); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}
