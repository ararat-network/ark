package keeper_test

import (
	"bytes"

	"go.uber.org/mock/gomock"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
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
	policyBefore, err := s.keeper.EconomicPolicy.Get(s.ctx)
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
	policyAfter, err := s.keeper.EconomicPolicy.Get(s.ctx)
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

// TestMsgUpdateParamsRefusals checks every refusal leaves the stored params as
// they were; the strict oracle mock also proves each precedes any rate capture.
func (s *KeeperTestSuite) TestMsgUpdateParamsRefusals() {
	tests := []struct {
		name    string
		msg     func(types.Params) *types.MsgUpdateParams
		errIs   error
		wantErr string
	}{
		{
			name:    "nil message",
			msg:     func(types.Params) *types.MsgUpdateParams { return nil },
			wantErr: "nil update params message",
		},
		{
			name: "signer is not the authority",
			msg: func(params types.Params) *types.MsgUpdateParams {
				params.RewardFundingWindow++
				return &types.MsgUpdateParams{
					Authority: sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String(),
					Params:    params,
				}
			},
			errIs: errortypes.ErrUnauthorized,
		},
		{
			// The window cap combines with the reward-target cap to bound accrual arithmetic.
			name: "funding window past its cap",
			msg: func(params types.Params) *types.MsgUpdateParams {
				params.RewardFundingWindow = types.MaxRewardFundingWindow + 1
				return &types.MsgUpdateParams{Authority: s.authority, Params: params}
			},
			wantErr: "RewardFundingWindow must be between one and",
		},
		{
			// The cap's unit is the protocol reference, so only MsgSetReferenceDenom, which
			// rebases every reference-unit consumer in one transaction, may move it.
			name: "reference denomination change",
			msg: func(params types.Params) *types.MsgUpdateParams {
				params.ReferenceDenom = chain.USDBaseDenom
				return &types.MsgUpdateParams{Authority: s.authority, Params: params}
			},
			errIs:   errortypes.ErrInvalidRequest,
			wantErr: "re-point it with MsgSetReferenceDenom, not ausd",
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			current, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)

			_, err = s.msgServer.UpdateParams(s.ctx, tc.msg(current))
			if tc.errIs != nil {
				s.Require().ErrorIs(err, tc.errIs)
			}
			if tc.wantErr != "" {
				s.Require().ErrorContains(err, tc.wantErr)
			}
			stored, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, stored)
		})
	}
}

// TestMsgUpdateParamsAcceptsAWindowUnderAMaximalPolicy checks independent domain caps admit the
// largest supported window and reward targets together.
func (s *KeeperTestSuite) TestMsgUpdateParamsAcceptsAWindowUnderAMaximalPolicy() {
	policy := types.DefaultEconomicPolicy()
	policy.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget
	policy.OracleBlockRewardTarget = types.MaxBlockRewardTarget
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
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

// TestMsgUpdateParamsRepricesCapsWithoutRebuild checks a reference-cap amount change updates
// derived reads immediately without rate capture, factor writes, or refresh events.
func (s *KeeperTestSuite) TestMsgUpdateParamsRepricesCapsWithoutRebuild() {
	s.setBlockHeight(42)
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
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

func (s *KeeperTestSuite) TestMsgUpdateParamsDoesNotRebuildCapsWhenActivatingTax() {
	s.setConversionFactors(types.ConversionFactor{
		Denom:  chain.XDRBaseDenom,
		Factor: math.LegacyOneDec(),
	})

	candidate, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	candidate.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    candidate,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(candidate.Equal(stored))
	cap, err := s.keeper.GetTaxCap(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(cap.IsZero())
}

// TestMsgUpdatePolicyBoundsTheBlockRewardTargets covers the per-block ceiling.
// It is a fact about the field, checked in the policy's own validation, so a
// candidate is refused without reading params or the open funding window.
func (s *KeeperTestSuite) TestMsgUpdatePolicyBoundsTheBlockRewardTargets() {
	candidate := types.DefaultEconomicPolicy()
	candidate.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget.Add(math.OneInt())

	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    candidate,
	})
	s.Require().ErrorContains(err, "ValidatorBlockRewardTarget must be between zero and")
	stored, getErr := s.keeper.EconomicPolicy.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(types.DefaultEconomicPolicy().Equal(stored))
}

// TestMsgUpdatePolicyIgnoresTheOpenFundingWindow checks policy validity depends on domain bounds,
// not accrued live-window state.
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
	candidate := types.DefaultEconomicPolicy()
	candidate.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget

	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: s.authority,
		Policy:    candidate,
	})
	s.Require().NoError(err)
	stored, getErr := s.keeper.EconomicPolicy.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(candidate.Equal(stored))
}

func (s *KeeperTestSuite) TestEconomicMandateAndCommitteeUpdate() {
	s.setBlockHeight(10)
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := economicPolicyBounds()

	_, err := s.msgServer.SetEconomicMandate(s.ctx, &types.MsgSetEconomicMandate{
		Authority:      s.authority,
		Committee:      committee,
		ActivationTime: 10,
		ExpiryTime:     20,
		MinimumPolicy:  minimum,
		MaximumPolicy:  maximum,
	})
	s.Require().NoError(err)
	mandate, err := s.keeper.EconomicMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(1), mandate.Term)
	s.Equal(committee, mandate.Committee)
	s.requireTypedEvent(&types.EventEconomicMandateSet{
		Term:           1,
		Committee:      committee,
		ActivationTime: 10,
		ExpiryTime:     20,
		CommitteeShape: absentCommitteeShape,
	})

	policy := committeePolicyCandidate()
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: mandate.Term,
		Policy:       policy,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.EconomicPolicy.Get(s.ctx)
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
	s.Require().ErrorContains(err, "not the exact appointed account")

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
	stored, err = s.keeper.EconomicPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(governanceOverride.InsuranceTargetRatio, stored.InsuranceTargetRatio)

	_, err = s.msgServer.SetEconomicMandate(s.ctx, &types.MsgSetEconomicMandate{
		Authority: s.authority,
	})
	s.Require().NoError(err)
	disabled, err := s.keeper.EconomicMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(2), disabled.Term)
	s.Empty(disabled.Committee)
	// A disabling is an appointment event too, carrying the empty committee and
	// the advanced term.
	s.requireTypedEvent(&types.EventEconomicMandateSet{Term: 2})

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    committee,
		ExpectedTerm: disabled.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "not the exact appointed account")
}

// TestEconomicPolicyMessagesAreRoleDisjoint proves the split messages cannot be
// crossed: the governance message rejects the committee, and the committee
// message rejects the governance authority even while its mandate is live.
func (s *KeeperTestSuite) TestEconomicPolicyMessagesAreRoleDisjoint() {
	s.setBlockHeight(10)
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := economicPolicyBounds()
	_, err := s.msgServer.SetEconomicMandate(s.ctx, &types.MsgSetEconomicMandate{
		Authority:      s.authority,
		Committee:      committee,
		ActivationTime: 10,
		ExpiryTime:     20,
		MinimumPolicy:  minimum,
		MaximumPolicy:  maximum,
	})
	s.Require().NoError(err)
	mandate, err := s.keeper.EconomicMandate.Get(s.ctx)
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
	s.Require().ErrorContains(err, "not the exact appointed account")

	stored, err := s.keeper.EconomicPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(types.DefaultEconomicPolicy().Equal(stored))
}

// TestSetEconomicMandateRefusals checks every refusal leaves the incumbent
// mandate in place and announces no appointment.
func (s *KeeperTestSuite) TestSetEconomicMandateRefusals() {
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	consensusAuthority := sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String()
	minimum, maximum := economicPolicyBounds()
	tests := []struct {
		name string
		// consensus overrides the fallback authority when set.
		consensus string
		height    int64
		msg       func(types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate
		errIs     error
		wantErr   string
	}{
		{
			name:    "nil message",
			msg:     func(types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate { return nil },
			wantErr: "nil set economic mandate message",
		},
		{
			name: "signer is not the authority",
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.Authority = "not-authority"
				return &msg
			},
			errIs: errortypes.ErrUnauthorized,
		},
		{
			// Only the Treasury authority is refused: an address may also hold the Claims
			// mandate, which app/claims_test.go checks.
			name: "committee is the authority",
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.Committee = s.authority
				return &msg
			},
			wantErr: "distinct from the Treasury authority",
		},
		{
			name:      "committee is the consensus authority",
			consensus: consensusAuthority,
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.Authority = consensusAuthority
				msg.Committee = consensusAuthority
				return &msg
			},
			wantErr: "distinct from the Treasury authority",
		},
		{
			// The fallback stays refused as a committee while consensus params override it.
			name:      "committee is the overridden fallback authority",
			consensus: consensusAuthority,
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.Authority = consensusAuthority
				msg.Committee = s.authority
				return &msg
			},
			wantErr: "distinct from the Treasury authority",
		},
		{
			name: "malformed committee",
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.Committee = "not-an-address"
				return &msg
			},
			wantErr: "committee is invalid",
		},
		{
			name: "inverted window",
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.ActivationTime, msg.ExpiryTime = msg.ExpiryTime, msg.ActivationTime
				return &msg
			},
			wantErr: "activation time must precede expiry time",
		},
		{
			// The window is half-open, so its expiry time is already inactive.
			name:   "elapsed window",
			height: 100,
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				return &msg
			},
			wantErr: "not after the current block time",
		},
		{
			name: "minimum above maximum",
			msg: func(msg types.MsgSetEconomicMandate) *types.MsgSetEconomicMandate {
				msg.MinimumPolicy, msg.MaximumPolicy = msg.MaximumPolicy, msg.MinimumPolicy
				return &msg
			},
			wantErr: "invalid economic-policy bounds",
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setBlockHeight(tc.height)
			if tc.consensus != "" {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
					Authority: &cmtproto.AuthorityParams{Authority: tc.consensus},
				})
			}

			_, err := s.msgServer.SetEconomicMandate(s.ctx, tc.msg(types.MsgSetEconomicMandate{
				Authority:      s.authority,
				Committee:      committee,
				ActivationTime: 10,
				ExpiryTime:     100,
				MinimumPolicy:  minimum,
				MaximumPolicy:  maximum,
			}))
			if tc.errIs != nil {
				s.Require().ErrorIs(err, tc.errIs)
			}
			if tc.wantErr != "" {
				s.Require().ErrorContains(err, tc.wantErr)
			}
			stored, err := s.keeper.EconomicMandate.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(types.DefaultEconomicMandate(), stored)
			s.requireNoTypedEvent(&types.EventEconomicMandateSet{})
		})
	}
}

func (s *KeeperTestSuite) TestGovernanceReferenceCapChangePreservesCommittee() {
	committee := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	minimum, maximum := economicPolicyBounds()
	_, err := s.msgServer.SetEconomicMandate(s.ctx, &types.MsgSetEconomicMandate{
		Authority:      s.authority,
		Committee:      committee,
		ActivationTime: 1,
		ExpiryTime:     100,
		MinimumPolicy:  minimum,
		MaximumPolicy:  maximum,
	})
	s.Require().NoError(err)

	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(100)
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)

	mandate, err := s.keeper.EconomicMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(1), mandate.Term)
	s.Equal(committee, mandate.Committee)
	s.True(minimum.Equal(mandate.MinimumPolicy))
	s.True(maximum.Equal(mandate.MaximumPolicy))
}

func economicPolicyBounds() (types.EconomicPolicy, types.EconomicPolicy) {
	minimum := types.DefaultEconomicPolicy()
	maximum := types.EconomicPolicy{
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

func committeePolicyCandidate() types.EconomicPolicy {
	return types.EconomicPolicy{
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

func (s *KeeperTestSuite) setConversionFactors(factors ...types.ConversionFactor) {
	s.Require().NoError(s.keeper.ConversionFactors.Clear(s.ctx, nil))
	for _, factor := range factors {
		s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, factor.Denom, factor))
	}
}

// TestMsgReturnSubsidy pins the stale-state guard, the fixed endpoints, and
// the event: only governance moves the pool, only NOAH, only into the
// community pool, and never below the minimum the proposal stated.
func (s *KeeperTestSuite) TestMsgReturnSubsidy() {
	subsidy := authtypes.NewModuleAddress(types.SubsidyPoolName)
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), subsidy, chain.NoahBaseDenom).
		Return(chain.NoahCoin(math.NewInt(100))).
		AnyTimes()
	returnMsg := func(amount, minimum int64) *types.MsgReturnSubsidy {
		return &types.MsgReturnSubsidy{
			Authority:             s.authority,
			Amount:                chain.NoahCoin(math.NewInt(amount)),
			MinimumSubsidyBalance: chain.NoahCoin(math.NewInt(minimum)),
		}
	}

	_, err := s.msgServer.ReturnSubsidy(s.ctx, returnMsg(50, 60))
	s.Require().ErrorContains(err, "balance 100 cannot return 50anoah while retaining 60anoah")
	_, err = s.msgServer.ReturnSubsidy(s.ctx, returnMsg(101, 0))
	s.Require().ErrorContains(err, "cannot return 101anoah")
	_, err = s.msgServer.ReturnSubsidy(s.ctx, returnMsg(0, 0))
	s.Require().ErrorContains(err, "must be positive")
	wrongDenom := returnMsg(10, 0)
	wrongDenom.Amount = sdk.NewInt64Coin(chain.USDBaseDenom, 10)
	_, err = s.msgServer.ReturnSubsidy(s.ctx, wrongDenom)
	s.Require().ErrorContains(err, "return amount")
	unauthorised := returnMsg(10, 0)
	unauthorised.Authority = sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String()
	_, err = s.msgServer.ReturnSubsidy(s.ctx, unauthorised)
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
	s.requireNoTypedEvent(&types.EventSubsidyReturned{
		Denom:     chain.NoahBaseDenom,
		Amount:    math.ZeroInt(),
		Remaining: math.ZeroInt(),
	})

	s.distributionKeeper.EXPECT().
		FundCommunityPool(gomock.Any(), chain.NoahCoins(math.NewInt(40)), subsidy).
		Return(nil)
	_, err = s.msgServer.ReturnSubsidy(s.ctx, returnMsg(40, 60))
	s.Require().NoError(err)
	s.requireTypedEvent(&types.EventSubsidyReturned{
		Denom:     chain.NoahBaseDenom,
		Amount:    math.NewInt(40),
		Remaining: math.NewInt(60),
	})
}
