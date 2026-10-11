package ante

import (
	cmttypes "github.com/cometbft/cometbft/types"

	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// MinBlockDataBytes is the least transaction budget a block may keep after evidence. The proposer
// injects the previous block's extended commit as the first transaction, and a block that cannot
// carry it is rejected by every validator with no proposer able to build another, so the budget is
// capped where governance writes it rather than projected from the live validator set. Eight
// mebibytes holds the commit of some eight hundred validators at the vote-extension ceiling, far
// past any set the chain seats, with room for ordinary transactions beside it.
const MinBlockDataBytes = 8 << 20

// ValidateConsensusParamsMsg refuses a consensus-parameter update that would leave blocks unable to
// carry the injected extended commit, whether signed, authz-wrapped, or carried in a governance
// proposal. x/consensus requires the block and evidence parameters together, so the budget reads
// from the message alone; an unlimited block is bounded by CometBFT's own ceiling.
func ValidateConsensusParamsMsg(cdc codec.Codec, msg sdk.Msg, depth int) error {
	return walkAuthzExec(cdc, msg, depth, func(msg sdk.Msg) error {
		switch m := msg.(type) {
		case *govv1.MsgSubmitProposal:
			msgs, err := m.GetMsgs()
			if err != nil {
				return errorsmod.Wrap(errortypes.ErrInvalidRequest, "cannot unpack proposal messages")
			}
			for _, inner := range msgs {
				if err := ValidateConsensusParamsMsg(cdc, inner, depth+1); err != nil {
					return err
				}
			}
			return nil
		case *consensustypes.MsgUpdateParams:
			return validateBlockDataBudget(m)
		default:
			return nil
		}
	})
}

func validateBlockDataBudget(msg *consensustypes.MsgUpdateParams) error {
	if msg.Block == nil || msg.Evidence == nil {
		// x/consensus refuses the update itself; nothing to size.
		return nil
	}
	maxBytes := msg.Block.MaxBytes
	if maxBytes == -1 {
		maxBytes = cmttypes.MaxBlockSizeBytes
	}
	if maxBytes-msg.Evidence.MaxBytes < MinBlockDataBytes {
		return errorsmod.Wrapf(
			errortypes.ErrInvalidRequest,
			"block max_bytes %d less evidence max_bytes %d leaves under %d bytes for transactions",
			msg.Block.MaxBytes,
			msg.Evidence.MaxBytes,
			MinBlockDataBytes,
		)
	}
	return nil
}
