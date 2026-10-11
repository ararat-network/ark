package mempool

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// WithReservations checks authenticated storage eligibility at the end of ante,
// while SDK RunTx still owns the disposable ante branch, and marks the entries
// CometBFT's recheck still reaches. No other mode applies node-local capacity
// as transaction validity.
func (p *Pool) WithReservations(ante sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		ctx, err := ante(ctx, tx, simulate)
		if err != nil {
			return ctx, err
		}
		switch ctx.ExecMode() {
		case sdk.ExecModeCheck:
			key, err := identity(tx)
			if err != nil {
				return ctx, err
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			_, err = p.allocation(key, Lane(ctx), len(ctx.TxBytes()))
			return ctx, err
		case sdk.ExecModeReCheck:
			p.touch(ctx.TxBytes())
		}
		return ctx, nil
	}
}
