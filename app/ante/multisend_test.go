package ante_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// guardAddr derives a distinct valid address per output without generating a
// key per recipient.
func guardAddr(i int) sdk.AccAddress {
	a := make([]byte, 20)
	binary.BigEndian.PutUint32(a, uint32(i))
	return sdk.AccAddress(a)
}

// multiSend builds a well-formed MsgMultiSend fanning out one base unit to
// each of n recipients.
func multiSend(from sdk.AccAddress, n int) *banktypes.MsgMultiSend {
	outputs := make([]banktypes.Output, n)
	for i := range outputs {
		outputs[i] = banktypes.Output{
			Address: guardAddr(i).String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
		}
	}
	return &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: from.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(n))),
		}},
		Outputs: outputs,
	}
}

func setupMultiSendTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress) {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	return arkApp, ctx, guardAddr(1 << 16)
}

func TestMultiSendGuard(t *testing.T) {
	arkApp, ctx, from := setupMultiSendTest(t)

	wrap := func(msg sdk.Msg, depth int) sdk.Msg {
		for range depth {
			exec := authz.NewMsgExec(from, []sdk.Msg{msg})
			msg = &exec
		}
		return msg
	}

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantGas uint64
		wantErr string
	}{
		{
			name:    "single-recipient send passes",
			msg:     multiSend(from, 1),
			wantGas: ante.MultiSendGasFactor,
		},
		{
			name:    "at the cap passes and pays the full surcharge",
			msg:     multiSend(from, ante.MaxMultiSendOutputs),
			wantGas: ante.MultiSendGasFactor * ante.MaxMultiSendOutputs * ante.MaxMultiSendOutputs,
		},
		{
			name:    "over the cap is refused",
			msg:     multiSend(from, ante.MaxMultiSendOutputs+1),
			wantErr: "too many MultiSend outputs",
		},
		{
			name:    "authz-wrapped over-cap send is refused",
			msg:     wrap(multiSend(from, ante.MaxMultiSendOutputs+1), 1),
			wantErr: "too many MultiSend outputs",
		},
		{
			name:    "authz-wrapped send pays the surcharge",
			msg:     wrap(multiSend(from, 10), 2),
			wantGas: ante.MultiSendGasFactor * 10 * 10,
		},
		{
			name:    "nesting past the depth bound is refused",
			msg:     wrap(multiSend(from, 1), codectypes.MaxUnpackAnyRecursionDepth),
			wantErr: "too many nested",
		},
		{
			name: "non-MultiSend message passes untouched",
			msg:  &banktypes.MsgSend{FromAddress: from.String()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metered := ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000_000))
			err := ante.ValidateMultiSendMsg(metered, arkApp.AppCodec(), tc.msg, 0)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantGas, metered.GasMeter().GasConsumed())
		})
	}
}

// TestMultiSendDecoratorRunsInSimulation pins the deviation from the gov-vote
// decorator: simulation must both enforce the cap and consume the surcharge,
// or gas estimates understate real delivery.
func TestMultiSendDecoratorRunsInSimulation(t *testing.T) {
	arkApp, ctx, from := setupMultiSendTest(t)
	decorator := ante.NewMultiSendDecorator(arkApp.AppCodec())
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	over := treasuryFeeTx{msgs: []sdk.Msg{multiSend(from, ante.MaxMultiSendOutputs+1)}}
	_, err := decorator.AnteHandle(ctx, over, true, next)
	require.ErrorContains(t, err, "too many MultiSend outputs")

	metered := ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000_000))
	within := treasuryFeeTx{msgs: []sdk.Msg{multiSend(from, 10)}}
	_, err = decorator.AnteHandle(metered, within, true, next)
	require.NoError(t, err)
	require.Equal(t, uint64(ante.MultiSendGasFactor*10*10), metered.GasMeter().GasConsumed())
}
