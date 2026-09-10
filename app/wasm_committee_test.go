package app_test

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/app/mempool"
	"github.com/ararat-network/ark/pkg/mandate"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// A contract can hold a deliberative mandate with no chain change: it
// dispatches the committee message itself, Wasmd refuses the dispatch unless
// the message names the contract, and the module then authorises term and
// window as it would for a signed transaction. These tests pin that path, the
// shape appointment records for it, and the lane it does not get.

// economicCorridor is a mandate corridor wide enough to hold committeePolicy.
func economicCorridor() (treasurytypes.EconomicPolicy, treasurytypes.EconomicPolicy) {
	return treasurytypes.DefaultEconomicPolicy(), treasurytypes.EconomicPolicy{
		ValidatorBlockRewardTarget:  math.NewInt(10),
		OracleBlockRewardTarget:     math.NewInt(10),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
		LiabilityRatioWeight:        math.LegacyOneDec(),
		VolatilityWeight:            math.LegacyOneDec(),
		FlowWeight:                  math.LegacyOneDec(),
	}
}

func committeePolicy() treasurytypes.EconomicPolicy {
	return treasurytypes.EconomicPolicy{
		ValidatorBlockRewardTarget:  math.NewInt(5),
		OracleBlockRewardTarget:     math.NewInt(5),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.25"),
		LiabilityRatioWeight:        math.LegacyMustNewDecFromStr("0.5"),
		VolatilityWeight:            math.LegacyZeroDec(),
		FlowWeight:                  math.LegacyZeroDec(),
	}
}

// committeeExpiry is the first height at which the appointed contract may no
// longer act.
const committeeExpiry = 100

// appointEconomicCommittee appoints committee to the economic mandate as
// governance, active from height one until committeeExpiry, and returns the
// term it received.
func appointEconomicCommittee(t *testing.T, f reflectFixture, committee sdk.AccAddress) uint64 {
	t.Helper()
	minimum, maximum := economicCorridor()
	_, err := treasurykeeper.NewMsgServerImpl(f.app.TreasuryKeeper).SetEconomicMandate(f.ctx, &treasurytypes.MsgSetEconomicMandate{
		Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Committee:        committee.String(),
		ActivationHeight: 1,
		ExpiryHeight:     committeeExpiry,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	})
	require.NoError(t, err)
	stored, err := f.app.TreasuryKeeper.EconomicMandate.Get(f.ctx)
	require.NoError(t, err)
	return stored.Term
}

// committeeUpdate is the committee message as a contract dispatches it: an
// Any carrying the same proto a signing committee would send.
func committeeUpdate(t *testing.T, f reflectFixture, committee sdk.AccAddress, term uint64) wasmvmtypes.CosmosMsg {
	t.Helper()
	msg := &treasurytypes.MsgCommitteeUpdatePolicy{
		Committee:    committee.String(),
		ExpectedTerm: term,
		Policy:       committeePolicy(),
	}
	return wasmvmtypes.CosmosMsg{Any: &wasmvmtypes.AnyMsg{
		TypeURL: sdk.MsgTypeURL(msg),
		Value:   f.app.AppCodec().MustMarshal(msg),
	}}
}

func storedPolicy(t *testing.T, f reflectFixture) treasurytypes.EconomicPolicy {
	t.Helper()
	policy, err := f.app.TreasuryKeeper.EconomicPolicy.Get(f.ctx)
	require.NoError(t, err)
	return policy
}

// TestContractCommitteeIsRecordedAsOne: appointment reads the contract store,
// so a contract is recorded as a contract rather than as the keyless account
// an unused key pair would produce.
func TestContractCommitteeIsRecordedAsOne(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "committee")
	appointEconomicCommittee(t, f, contract)

	stored, err := f.app.TreasuryKeeper.EconomicMandate.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, mandate.CommitteeShape{
		AccountType: "cosmos.auth.v1beta1.BaseAccount",
		KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_CONTRACT,
	}, stored.CommitteeShape)
}

// TestContractCommitteeDispatchesItsAction: the contract's own dispatch clears
// Wasmd's signer check and the module's term and window, and the policy lands.
func TestContractCommitteeDispatchesItsAction(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "committee")
	term := appointEconomicCommittee(t, f, contract)

	require.NoError(t, f.execute(t, contract, f.owner, committeeUpdate(t, f, contract, term)))
	require.True(t, committeePolicy().Equal(storedPolicy(t, f)))
}

// TestContractCommitteeDispatchIsRefused: naming another committee is refused
// by Wasmd before the module sees the message; a stale term or an expired
// window is refused by the module. None of them moves the policy.
func TestContractCommitteeDispatchIsRefused(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "committee")
	term := appointEconomicCommittee(t, f, contract)
	before := storedPolicy(t, f)

	tests := []struct {
		name      string
		committee sdk.AccAddress
		term      uint64
		height    int64
		want      string
	}{
		{name: "another committee in the message", committee: f.owner, term: term, height: 1, want: "contract doesn't have permission"},
		{name: "stale term", committee: contract, term: term + 1, height: 1, want: "term mismatch"},
		{name: "expired window", committee: contract, term: term, height: committeeExpiry, want: "mandate is not active"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			at := f
			at.ctx = f.ctx.WithBlockHeight(test.height)
			err := at.execute(t, contract, f.owner, committeeUpdate(t, f, test.committee, test.term))
			require.ErrorContains(t, err, test.want)
			require.True(t, before.Equal(storedPolicy(t, f)))
		})
	}
}

// TestContractCommitteeActionFailsWithTheCall: an action dispatched beside a
// message that fails fails with it, as any contract dispatch does.
func TestContractCommitteeActionFailsWithTheCall(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "committee")
	term := appointEconomicCommittee(t, f, contract)
	before := storedPolicy(t, f)

	err := f.execute(t, contract, f.owner, committeeUpdate(t, f, contract, term), bankSend(f.recipient, 1_000_000))
	require.Error(t, err)
	require.True(t, before.Equal(storedPolicy(t, f)))
}

// TestContractCommitteeActionRidesTheNormalLane: the transaction a proposer
// signs carries MsgExecuteContract, not the committee message, so the lane
// classifier sees no committee action and grants no priority.
func TestContractCommitteeActionRidesTheNormalLane(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "committee")
	term := appointEconomicCommittee(t, f, contract)

	builder := f.app.TxConfig().NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   f.owner.String(),
		Contract: contract.String(),
		Msg:      reflectBody(t, committeeUpdate(t, f, contract, term)),
	}))
	lane, err := f.app.Privileges().Classify(f.ctx, builder.GetTx())
	require.NoError(t, err)
	require.Equal(t, mempool.LaneNormal, lane)
}
