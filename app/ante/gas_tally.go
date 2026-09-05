package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// GasTallyDecorator records each transaction's declared gas for the base-fee
// controller. Block execution and simulation only; a transaction failing
// later in the ante reverts the write with everything else, so the tally
// reads as the block's paid-for gas.
type GasTallyDecorator struct {
	treasury *treasurykeeper.Keeper
}

func NewGasTallyDecorator(treasury *treasurykeeper.Keeper) GasTallyDecorator {
	return GasTallyDecorator{treasury: treasury}
}

func (d GasTallyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Genesis transactions are not block traffic and must not seed the first
	// block's tally.
	if (ctx.ExecMode() == sdk.ExecModeFinalize || simulate) && ctx.BlockHeight() != 0 {
		if feeTx, ok := tx.(sdk.FeeTx); ok {
			if err := d.treasury.TallyBlockGas(ctx, feeTx.GetGas()); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}
