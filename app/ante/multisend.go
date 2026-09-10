package ante

import (
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// MultiSend consensus limits bound recipient count and charge quadratic gas for fan-out. They apply
// uniformly across nodes; see README.md for their sizing and execution boundaries.
const (
	maxMultiSendOutputs = 500
	multiSendGasFactor  = 300
)

// ValidateMultiSendMsg caps MultiSend recipients and charges quadratic gas, including authz leaves.
// Ante and the execution-policy router enforce it; governance proposal execution uses the
// underlying router. Other messages pass through.
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
// It runs in simulation too: the cap is pure message shape, and skipping
// the surcharge would understate gas estimates.
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
