package mempool

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
)

func TestReservationReleaseFollowsCometLifecycle(t *testing.T) {
	for _, caller := range []sdkmempool.RemovalCaller{sdkmempool.CallerRunTxFinalize, sdkmempool.CallerPrepareProposalRemoveInvalid, sdkmempool.CallerRunTxRecheck} {
		t.Run(string(caller), func(t *testing.T) {
			p := newTestPool(20)
			tx := fixtureTx(1)
			require.NoError(t, admit(p, LaneNormal, 0, tx))
			require.NoError(t, p.RemoveWithReason(sdk.Context{}, tx, sdkmempool.RemoveReason{Caller: caller}))
			if caller == sdkmempool.CallerRunTxRecheck {
				require.Zero(t, p.CountTx())
				return
			}
			require.Equal(t, 1, p.CountTx())
			p.Finalised([][]byte{encodePoolTx(tx)})
			require.Equal(t, 1, p.CountTx())
			p.PrepareCheckState(sdk.Context{})
			require.Zero(t, p.CountTx())
		})
	}
}

func TestReservationsOnlyApplyToFreshAdmission(t *testing.T) {
	for _, mode := range []sdk.ExecMode{sdk.ExecModeCheck, sdk.ExecModeReCheck, sdk.ExecModePrepareProposal, sdk.ExecModeProcessProposal, sdk.ExecModeSimulate, sdk.ExecModeFinalize} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			p := newTestPool(20)
			for i := range 18 {
				require.NoError(t, admit(p, LaneNormal, 0, fixtureTx(byte(i+1))))
			}
			handler := p.WithReservations(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return WithLane(ctx, LaneNormal), nil })
			tx := fixtureTx(50)
			_, err := handler(admissionContext(tx, LaneNormal, 0).WithExecMode(mode), tx, mode == sdk.ExecModeSimulate)
			if mode == sdk.ExecModeCheck {
				require.ErrorIs(t, err, ErrCapacity)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
