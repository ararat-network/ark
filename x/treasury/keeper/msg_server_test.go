package keeper_test

import (
	"bytes"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/treasury/types"
)

// absentCommitteeShape is the observation an appointment records for a
// committee address holding no account, which is every committee here: these
// suites drive the keeper directly rather than through a signed transaction,
// so no committee account is ever created.
var absentCommitteeShape = mandate.CommitteeShape{
	KeyKind: mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_ABSENT,
}

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	policyBefore, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	// The reference cap matches the suite baseline so no rebuild fires: the
	// window change is the whole update.
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.ZeroInt()
	params.RewardFundingWindow++

	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params, stored)
	policyAfter, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(policyBefore.Equal(policyAfter))
}

func (s *KeeperTestSuite) TestMsgUpdateParamsDefersRewardFundingWindowChange() {
	funding := rewardFunding(2, 0, 0, 0)
	s.setRewardFunding(funding)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.ZeroInt()
	params.RewardFundingWindow = 5

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	storedParams, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(5), storedParams.RewardFundingWindow)
	storedFunding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), storedFunding.BlocksRemaining)
}

// TestMsgUpdateParamsBoundsTheFundingWindow covers the window's own ceiling,
// which with the per-block target cap is what keeps a window's accrual
// representable. The check is local to Params.Validate, so it does not consult
// the live policy or the open window — a window length is admissible or not on
// its own terms.
func (s *KeeperTestSuite) TestMsgUpdateParamsBoundsTheFundingWindow() {
	currentParams := types.DefaultParams()
	currentParams.RewardFundingWindow = 1
	s.Require().NoError(s.keeper.Params.Set(s.ctx, currentParams))

	candidate := currentParams
	candidate.RewardFundingWindow = types.MaxRewardFundingWindow + 1
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    candidate,
	})
	s.Require().ErrorContains(err, "RewardFundingWindow must be between one and")
	stored, getErr := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Equal(currentParams, stored)
}

// TestMsgUpdateParamsAcceptsAWindowUnderAMaximalPolicy pins what the domain
// caps bought. A window this long under targets this large was refused before,
// by a validation that multiplied the two out at every write; the ceilings on
// each field make the product safe without the projection, so the pair is now
// simply admissible.
func (s *KeeperTestSuite) TestMsgUpdateParamsAcceptsAWindowUnderAMaximalPolicy() {
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget
	policy.OracleBlockRewardTarget = types.MaxBlockRewardTarget
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

	candidate := types.DefaultParams()
	candidate.RewardFundingWindow = types.MaxRewardFundingWindow
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    candidate,
	})
	s.Require().NoError(err)
	stored, getErr := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Equal(candidate, stored)
}

// TestMsgUpdateParamsRepricesCapsWithoutRebuild pins the derive-at-read
// contract at the governance call site: a reference amount change stores the
// params and nothing else — no rate capture, no factor writes, no event —
// because every derived cap re-prices the moment the params land, the zero
// sentinel included.
func (s *KeeperTestSuite) TestMsgUpdateParamsRepricesCapsWithoutRebuild() {
	s.setBlockHeight(42)
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))

	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(100)
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(200), usdCap)
	s.Require().Empty(s.rateCaptures)

	// The uncapped sentinel arrives the same way.
	params.ReferenceTaxCap = math.ZeroInt()
	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	usdCap, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(usdCap.IsZero())
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyDoesNotRebuildCapsWhenActivatingTax() {
	s.setConversionFactors(types.ConversionFactor{
		Denom:  chain.SDRBaseDenom,
		Factor: math.LegacyOneDec(),
	})

	candidate := types.DefaultMonetaryPolicy()
	candidate.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    candidate,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(candidate.Equal(stored))
	cap, err := s.keeper.GetTaxCap(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(cap.IsZero())
}

// TestMsgUpdatePolicyBoundsTheBlockRewardTargets covers the per-block ceiling.
// It is a fact about the field, checked in the policy's own validation, so a
// candidate is refused without reading params or the open funding window.
func (s *KeeperTestSuite) TestMsgUpdatePolicyBoundsTheBlockRewardTargets() {
	candidate := types.DefaultMonetaryPolicy()
	candidate.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget.Add(math.OneInt())

	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    candidate,
	})
	s.Require().ErrorContains(err, "ValidatorBlockRewardTarget must be between zero and")
	stored, getErr := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(types.DefaultMonetaryPolicy().Equal(stored))
}

// TestMsgUpdatePolicyIgnoresTheOpenFundingWindow pins the property the swap to
// domain caps was for: a policy's admissibility no longer depends on how far
// the current window has already accrued. The same candidate that is accepted
// here would have been refused mid-window before, which made a governance value
// valid or not according to when it was proposed.
func (s *KeeperTestSuite) TestMsgUpdatePolicyIgnoresTheOpenFundingWindow() {
	params := types.DefaultParams()
	params.RewardFundingWindow = types.MaxRewardFundingWindow
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setRewardFunding(types.RewardFundingState{
		BlocksRemaining:   types.MaxRewardFundingWindow,
		ValidatorTarget:   types.MaxBlockRewardTarget,
		OracleTarget:      types.MaxBlockRewardTarget,
		ValidatorFeeValue: math.ZeroInt(),
	})
	candidate := types.DefaultMonetaryPolicy()
	candidate.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget

	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    candidate,
	})
	s.Require().NoError(err)
	stored, getErr := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(candidate.Equal(stored))
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsInvalidAuthority() {
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: "not-authority",
		Params:    types.DefaultParams(),
	})
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
}

func (s *KeeperTestSuite) TestMonetaryMandateAndCommitteeUpdate() {
	s.setBlockHeight(10)
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := monetaryPolicyBounds()

	_, err := s.msgServer.SetMonetaryMandate(s.ctx, &types.MsgSetMonetaryMandate{
		Authority:        s.authority,
		Committee:        committee,
		ActivationHeight: 10,
		ExpiryHeight:     20,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	})
	s.Require().NoError(err)
	mandate, err := s.keeper.MonetaryMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(1), mandate.Term)
	s.Equal(committee, mandate.Committee)
	s.requireTypedEvent(&types.EventMonetaryMandateSet{
		Term:             1,
		Committee:        committee,
		ActivationHeight: 10,
		ExpiryHeight:     20,
		CommitteeShape:   absentCommitteeShape,
	})

	policy := committeePolicyCandidate()
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: mandate.Term,
		Policy:       policy,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.True(policy.Equal(stored))
	storedParams, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(types.DefaultRewardFundingWindow, storedParams.RewardFundingWindow)

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String(),
		ExpectedTerm: mandate.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "not the exact appointed committee")

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: mandate.Term + 1,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "term mismatch")

	outOfBounds := policy
	outOfBounds.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: mandate.Term,
		Policy:       outOfBounds,
	})
	s.Require().ErrorContains(err, "outside mandate range")

	governanceOverride := policy
	governanceOverride.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	_, err = s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    governanceOverride,
	})
	s.Require().NoError(err)
	stored, err = s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(governanceOverride.InsuranceTargetRatio, stored.InsuranceTargetRatio)

	_, err = s.msgServer.SetMonetaryMandate(s.ctx, &types.MsgSetMonetaryMandate{
		Authority: s.authority,
	})
	s.Require().NoError(err)
	disabled, err := s.keeper.MonetaryMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(2), disabled.Term)
	s.Empty(disabled.Committee)
	// A disabling is an appointment event too, carrying the empty committee and
	// the advanced term.
	s.requireTypedEvent(&types.EventMonetaryMandateSet{Term: 2})

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: disabled.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "not the exact appointed committee")
}

// TestMonetaryPolicyMessagesAreRoleDisjoint proves the split messages cannot be
// crossed: the governance message rejects the committee, and the committee
// message rejects the governance authority even while its mandate is live.
func (s *KeeperTestSuite) TestMonetaryPolicyMessagesAreRoleDisjoint() {
	s.setBlockHeight(10)
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := monetaryPolicyBounds()
	_, err := s.msgServer.SetMonetaryMandate(s.ctx, &types.MsgSetMonetaryMandate{
		Authority:        s.authority,
		Committee:        committee,
		ActivationHeight: 10,
		ExpiryHeight:     20,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	})
	s.Require().NoError(err)
	mandate, err := s.keeper.MonetaryMandate.Get(s.ctx)
	s.Require().NoError(err)
	policy := committeePolicyCandidate()

	_, err = s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: committee,
		Policy:    policy,
	})
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    s.authority,
		ExpectedTerm: mandate.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "not the exact appointed committee")

	stored, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(types.DefaultMonetaryPolicy().Equal(stored))
}

func (s *KeeperTestSuite) TestMonetaryMandateAuthorityAndRoleSeparation() {
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := monetaryPolicyBounds()
	message := &types.MsgSetMonetaryMandate{
		Authority:        "not-authority",
		Committee:        committee,
		ActivationHeight: 1,
		ExpiryHeight:     2,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	}
	_, err := s.msgServer.SetMonetaryMandate(s.ctx, message)
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)

	message.Authority = s.authority
	message.Committee = s.authority
	_, err = s.msgServer.SetMonetaryMandate(s.ctx, message)
	s.Require().ErrorContains(err, "distinct from Treasury authority")

	message.Committee = committee
	_, err = s.msgServer.SetMonetaryMandate(s.ctx, message)
	s.Require().NoError(err)
	// The other half of this property — that one address may hold both the
	// monetary and Claims mandates, because the roles are separated from the
	// Treasury authority rather than from each other — now spans two modules
	// and is asserted in app/claims_test.go.
}

func (s *KeeperTestSuite) TestConsensusAuthorityCannotBecomeMonetaryRole() {
	consensusAuthority := sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String()
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Authority: &cmtproto.AuthorityParams{Authority: consensusAuthority},
	})
	minimum, maximum := monetaryPolicyBounds()
	message := &types.MsgSetMonetaryMandate{
		Authority:        consensusAuthority,
		Committee:        consensusAuthority,
		ActivationHeight: 1,
		ExpiryHeight:     2,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	}
	_, err := s.msgServer.SetMonetaryMandate(s.ctx, message)
	s.Require().ErrorContains(err, "distinct from Treasury authority")

	// The fallback authority stays rejected as a committee even while a
	// consensus-params authority overrides it.
	message.Committee = s.authority
	_, err = s.msgServer.SetMonetaryMandate(s.ctx, message)
	s.Require().ErrorContains(err, "distinct from Treasury authority")
}

func (s *KeeperTestSuite) TestGovernanceReferenceCapChangePreservesCommittee() {
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := monetaryPolicyBounds()
	_, err := s.msgServer.SetMonetaryMandate(s.ctx, &types.MsgSetMonetaryMandate{
		Authority:        s.authority,
		Committee:        committee,
		ActivationHeight: 1,
		ExpiryHeight:     100,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	})
	s.Require().NoError(err)

	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(100)
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)

	mandate, err := s.keeper.MonetaryMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(1), mandate.Term)
	s.Equal(committee, mandate.Committee)
	s.True(minimum.Equal(mandate.MinimumPolicy))
	s.True(maximum.Equal(mandate.MaximumPolicy))
}

func monetaryPolicyBounds() (types.MonetaryPolicy, types.MonetaryPolicy) {
	minimum := types.DefaultMonetaryPolicy()
	maximum := types.MonetaryPolicy{
		StabilityTaxRate:            math.LegacyMustNewDecFromStr("0.1"),
		ValidatorBlockRewardTarget:  math.NewInt(10),
		OracleBlockRewardTarget:     math.NewInt(10),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
		LiabilityRatioWeight:        math.LegacyOneDec(),
		VolatilityWeight:            math.LegacyOneDec(),
		FlowWeight:                  math.LegacyOneDec(),
	}
	return minimum, maximum
}

func committeePolicyCandidate() types.MonetaryPolicy {
	return types.MonetaryPolicy{
		StabilityTaxRate:            math.LegacyZeroDec(),
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

// TestMsgUpdateParamsRejectsReferenceDenomChange pins the ownership split:
// the cap's unit is the protocol reference, so governance cannot re-anchor it
// through ordinary params — only MsgSetReferenceDenom, which rebases every
// reference-unit consumer in one transaction, may move it.
func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsReferenceDenomChange() {
	current, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params := types.DefaultParams()
	params.ReferenceDenom = chain.USDBaseDenom

	// The strict oracle mock also proves the rejection precedes any rate
	// capture or cap rebuild.
	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "re-point it with MsgSetReferenceDenom, not ausd")
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(current, stored)
}

func (s *KeeperTestSuite) setConversionFactors(factors ...types.ConversionFactor) {
	s.Require().NoError(s.keeper.ConversionFactors.Clear(s.ctx, nil))
	for _, factor := range factors {
		s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, factor.Denom, factor))
	}
}
