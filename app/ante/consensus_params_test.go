package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// TestConsensusParamsBudgetFloor pins the one rule: block bytes less evidence bytes must leave the
// floor for transactions, read the same way signed, authz-wrapped, and inside a proposal.
func TestConsensusParamsBudgetFloor(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	cdc := arkApp.AppCodec()
	update := func(block, evidence int64) *consensustypes.MsgUpdateParams {
		return &consensustypes.MsgUpdateParams{
			Authority: "authority",
			Block:     &cmtproto.BlockParams{MaxBytes: block, MaxGas: -1},
			Evidence:  &cmtproto.EvidenceParams{MaxBytes: evidence, MaxAgeNumBlocks: 1, MaxAgeDuration: 1},
			Validator: &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		}
	}

	tests := []struct {
		name   string
		msg    *consensustypes.MsgUpdateParams
		refuse bool
	}{
		{name: "launch budget", msg: update(22_020_096, 1_048_576)},
		{name: "exactly the floor", msg: update(ante.MinBlockDataBytes+1_000, 1_000)},
		{name: "one byte under the floor", msg: update(ante.MinBlockDataBytes+999, 1_000), refuse: true},
		{name: "block too small on its own", msg: update(1_048_576, 0), refuse: true},
		{name: "evidence eats the budget", msg: update(22_020_096, 22_000_000), refuse: true},
		{name: "unlimited block", msg: update(-1, cmttypes.MaxBlockSizeBytes-ante.MinBlockDataBytes)},
		{name: "unlimited block with the budget eaten", msg: update(-1, cmttypes.MaxBlockSizeBytes-ante.MinBlockDataBytes+1), refuse: true},
		{name: "incomplete update is x/consensus's to refuse", msg: &consensustypes.MsgUpdateParams{Block: &cmtproto.BlockParams{MaxBytes: 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := []struct {
				name string
				msg  sdk.Msg
			}{
				{name: "signed", msg: tc.msg},
				{name: "authz", msg: mustExec(t, tc.msg)},
				{name: "proposal", msg: mustProposal(t, tc.msg)},
				{name: "proposal under authz", msg: mustExec(t, mustProposal(t, tc.msg))},
			}
			for _, w := range wrapped {
				err := ante.ValidateConsensusParamsMsg(cdc, w.msg, 0)
				if tc.refuse {
					require.ErrorContains(t, err, "leaves under", w.name)
				} else {
					require.NoError(t, err, w.name)
				}
			}
		})
	}
}

func mustExec(t *testing.T, msgs ...sdk.Msg) *authz.MsgExec {
	t.Helper()
	exec := authz.NewMsgExec(guardAddr(1), msgs)
	return &exec
}

func mustProposal(t *testing.T, msgs ...sdk.Msg) *govv1.MsgSubmitProposal {
	t.Helper()
	proposal, err := govv1.NewMsgSubmitProposal(msgs, nil, guardAddr(2).String(), "", "title", "summary", false)
	require.NoError(t, err)
	return proposal
}
