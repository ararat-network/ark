package ante

import (
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// The MultiSend fan-out guard, the spam defence the Hub adopted after
// inscription-era wide-fanout sends cost the network far more than they cost
// the sender. Compiled in like minVoterStake: they decide what enters blocks,
// so a node-local knob would fragment mempools.
//
// The Hub's figures. The Hub derived the factor from its block: 75M max_gas
// over 500² is 300, so one max-fan-out send fills a Hub block. Against the
// genesis 100M max_gas the same send costs ~84M (75M surcharge plus Bank's
// ~18k-per-output linear cost), so here the cap, not gas, is the binding
// rule, and stays so down to a max_gas of ~85M. The Hub's derivation redone
// for 100M gives 400, which would have gas bind at ~477 outputs and leave
// the cap dead. The surcharge overtakes the linear cost near 60 outputs,
// leaving ordinary batch payouts unhurt; a max-fan-out send is paid for,
// and repetition ratchets the base fee.
const (
	maxMultiSendOutputs = 500
	multiSendGasFactor  = 300
)

// ValidateMultiSendMsg refuses a MultiSend fanning out to more than
// maxMultiSendOutputs recipients and charges a quadratic gas surcharge on one
// within it — fan-out costs the network superlinearly, so it must cost the
// sender superlinearly. It recurses through authz MsgExec so a wrapped send
// cannot slip past either seam that calls this: the ante for signed
// transactions, and the policy router for messages a contract, derived
// account, or interchain account dispatches. Sends a passed proposal executes
// are the one path around it, and need no guard: they already won a
// governance vote. Non-MultiSend messages pass untouched.
func ValidateMultiSendMsg(ctx sdk.Context, cdc codec.Codec, msg sdk.Msg, depth int) error {
	return walkAuthzExec(cdc, msg, depth, func(msg sdk.Msg) error {
		send, ok := msg.(*banktypes.MsgMultiSend)
		if !ok {
			return nil
		}
		n := uint64(len(send.Outputs))
		if n > maxMultiSendOutputs {
			return errorsmod.Wrapf(
				errortypes.ErrInvalidRequest,
				"too many MultiSend outputs: max %d, got %d",
				maxMultiSendOutputs, n,
			)
		}
		// Cap first, so n² cannot overflow.
		ctx.GasMeter().ConsumeGas(multiSendGasFactor*n*n, "MultiSend quadratic surcharge")
		return nil
	})
}

// MultiSendDecorator applies the fan-out guard to signed transactions; the
// policy router applies the same guard to execution-generated messages.
// Unlike the gov-vote decorator it runs in simulation too: the cap is pure
// message shape, and skipping the surcharge would understate gas estimates.
type MultiSendDecorator struct {
	cdc codec.Codec
}

func NewMultiSendDecorator(cdc codec.Codec) MultiSendDecorator {
	return MultiSendDecorator{cdc: cdc}
}

func (d MultiSendDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := ValidateMultiSendMsg(ctx, d.cdc, msg, 0); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}
