package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app/mempool"
)

// PrivilegeDecorator assigns priority after signatures have been verified.
// Governance ineligibility keeps a transaction in the normal lane; its messages
// may establish each other's prerequisites when they execute in order.
// Committee candidates must pass mandate authorisation. Mixed and authz
// transactions use the normal lane without priority checks.
type PrivilegeDecorator struct{ set mempool.Set }

func NewPrivilegeDecorator(set mempool.Set) PrivilegeDecorator {
	return PrivilegeDecorator{set: set}
}

// AnteHandle also evaluates in simulation and execution, so eligibility gas
// is accounted for consistently. Committee authorisation failures reject the
// transaction; governance priority refusals do not decide execution validity.
func (d PrivilegeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	lane, err := d.set.Classify(ctx, tx)
	if err != nil {
		return ctx, err
	}
	return next(mempool.WithLane(ctx, lane), tx, simulate)
}
