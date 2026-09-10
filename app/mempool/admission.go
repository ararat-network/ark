package mempool

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// WithReservations checks authenticated storage eligibility at the end of ante,
// while SDK RunTx still owns the disposable ante branch. Recheck, proposal,
// simulation and execution never apply node-local capacity as transaction validity.
func (p *Pool) WithReservations(ante sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		ctx, err := ante(ctx, tx, simulate)
		if err != nil || ctx.ExecMode() != sdk.ExecModeCheck {
			return ctx, err
		}
		key, err := identity(tx)
		if err != nil {
			return ctx, err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		_, err = p.allocation(key, Lane(ctx), len(ctx.TxBytes()))
		return ctx, err
	}
}
