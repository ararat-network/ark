package ante

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/lanes"
)

// PrivilegeDecorator vouches for every privileged message in a signed
// transaction, authz exec walked, through the same lane set the mempool
// classifies with. A transaction earns the priority lane only if its modules
// vouch for it, and one they refuse is kept out here rather than failing at
// execution after riding the lane.
type PrivilegeDecorator struct {
	cdc codec.Codec
	set lanes.Set
}

func NewPrivilegeDecorator(cdc codec.Codec, set lanes.Set) PrivilegeDecorator {
	return PrivilegeDecorator{cdc: cdc, set: set}
}

// AnteHandle vouches in every mode, simulation included: each vouch is reads
// only, and an estimate that skipped it would fall short of block execution
// by exactly its gas.
func (d PrivilegeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := walkAuthzExec(d.cdc, msg, 0, func(msg sdk.Msg) error {
			return d.set.Vouch(ctx, msg)
		}); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}
