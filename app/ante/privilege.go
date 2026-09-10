package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app/mempool"
)

// PrivilegeDecorator assigns lanes after signature verification. Ineligible governance messages
// stay normal; committee candidates require mandate authorisation. Mixed and authz transactions use
// the normal lane.
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
