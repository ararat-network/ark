package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	policyBefore, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	params := types.DefaultParams()
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
	funding := rewardFunding(2, 0, 0, 0, true)
	s.setRewardFunding(funding)
	params := types.DefaultParams()
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

func (s *KeeperTestSuite) TestMsgUpdateParamsRebuildsCapsOnReferenceChange() {
	s.setBlockHeight(42)
	params := types.DefaultParams()
	params.ReferenceTaxCap = sdk.NewInt64Coin(chain.MicroUSDDenom, 100)
	configured := oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(), chain.MicroSDRDenom, chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	for _, denom := range []string{chain.MicroSDRDenom, chain.MicroUSDDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), cap)
	}
	s.requireExactEvent(sdk.NewEvent(
		types.EventTypeTaxCapsUpdated,
		sdk.NewAttribute(
			types.AttributeKeyTaxCaps,
			sdk.NewCoins(
				sdk.NewInt64Coin(chain.MicroSDRDenom, 100),
				sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
			).String(),
		),
	))
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsZeroReferenceCap() {
	params := types.DefaultParams()
	params.ReferenceTaxCap = sdk.NewInt64Coin(chain.MicroUSDDenom, 0)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().ErrorContains(err, "ReferenceTaxCap must be positive")
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultParams(), stored)
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

	policy := committeePolicyCandidate()
	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer:       committee,
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

	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer:       sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String(),
		ExpectedTerm: mandate.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "neither Treasury authority")

	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer:       committee,
		ExpectedTerm: mandate.Term + 1,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "term mismatch")

	outOfBounds := policy
	outOfBounds.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer:       committee,
		ExpectedTerm: mandate.Term,
		Policy:       outOfBounds,
	})
	s.Require().ErrorContains(err, "outside mandate range")

	governanceOverride := policy
	governanceOverride.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer: s.authority,
		Policy: governanceOverride,
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

	_, err = s.msgServer.UpdateMonetaryPolicy(s.ctx, &types.MsgUpdateMonetaryPolicy{
		Signer:       committee,
		ExpectedTerm: disabled.Term,
		Policy:       policy,
	})
	s.Require().ErrorContains(err, "neither Treasury authority")
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
	_, err = s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
		Authority:                s.authority,
		Committee:                committee,
		ExpiryHeight:             100,
		CancellationPeriodBlocks: 1,
		CommitteeClaimLimit:      math.NewInt(100),
	})
	s.Require().ErrorContains(err, "distinct from the monetary-policy committee")
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
	params.ReferenceTaxCap = sdk.NewInt64Coin(chain.MicroUSDDenom, 100)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(), chain.MicroSDRDenom, chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)
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
	}
}

type insuranceAccountKeeper struct {
	addresses map[string]sdk.AccAddress
}

func (a insuranceAccountKeeper) GetModuleAddress(name string) sdk.AccAddress {
	return a.addresses[name]
}

func (insuranceAccountKeeper) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}

type insuranceBankKeeper struct {
	balance       math.Int
	sendErr       error
	sends         int
	lastRecipient sdk.AccAddress
	lastAmount    sdk.Coins
	blocked       map[string]bool
}

func (b *insuranceBankKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return b.blocked[addr.String()]
}

func (b *insuranceBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.ZeroInt())
}

func (b *insuranceBankKeeper) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balance)
}

func (b *insuranceBankKeeper) GetAllBalances(_ context.Context, _ sdk.AccAddress) sdk.Coins {
	if b.balance.IsPositive() {
		return sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, b.balance))
	}
	return sdk.Coins{}
}

func (*insuranceBankKeeper) SendCoinsFromModuleToModule(context.Context, string, string, sdk.Coins) error {
	return errors.New("unexpected module-to-module send")
}

func (b *insuranceBankKeeper) SendCoinsFromModuleToAccount(
	_ context.Context,
	_ string,
	recipient sdk.AccAddress,
	amount sdk.Coins,
) error {
	if b.sendErr != nil {
		return b.sendErr
	}
	next, err := b.balance.SafeSub(amount.AmountOf(chain.MicroNoahDenom))
	if err != nil || next.IsNegative() {
		return errors.New("insufficient fake bank balance")
	}
	b.balance = next
	b.sends++
	b.lastRecipient = append(sdk.AccAddress(nil), recipient...)
	b.lastAmount = append(sdk.Coins(nil), amount...)
	return nil
}

type insuranceOracleKeeper struct{}

func (insuranceOracleKeeper) GetRateSnapshot(context.Context, ...string) (oracletypes.RateSnapshot, error) {
	return oracletypes.RateSnapshot{}, nil
}

func (insuranceOracleKeeper) GetTobinTaxes(context.Context) (oracletypes.TobinTaxes, error) {
	return nil, nil
}

type ClaimsKeeperTestSuite struct {
	suite.Suite

	ctx       context.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	bank      *insuranceBankKeeper
	authority string
	committee string
	recipient string
	caller    string
}

func TestClaimsKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(ClaimsKeeperTestSuite))
}

func (s *ClaimsKeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("insurance_transient")
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, transientKey)
	s.ctx = testCtx.Ctx

	addresses := make(map[string]sdk.AccAddress)
	for _, moduleName := range types.FundAccountNames() {
		addresses[moduleName] = authtypes.NewModuleAddress(moduleName)
	}
	addresses[types.StabilityTaxCollectorName] = authtypes.NewModuleAddress(types.StabilityTaxCollectorName)
	accountKeeper := insuranceAccountKeeper{addresses: addresses}
	s.bank = &insuranceBankKeeper{
		balance: math.NewInt(1_000),
		blocked: make(map[string]bool),
	}
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	s.committee = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	s.recipient = sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	s.caller = sdk.AccAddress(bytes.Repeat([]byte{4}, 20)).String()

	s.keeper = keeper.NewKeeper(
		cdc,
		storeService,
		transientStoreService,
		s.authority,
		accountKeeper,
		s.bank,
		insuranceOracleKeeper{},
	)
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, types.DefaultClaimsMandate()))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.ZeroInt()))
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.ZeroInt()))
	s.Require().NoError(s.keeper.NextClaimID.Set(s.ctx, 1))
	s.Require().NoError(s.keeper.MonetaryMandate.Set(s.ctx, types.DefaultMonetaryMandate()))
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *ClaimsKeeperTestSuite) setHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

func (s *ClaimsKeeperTestSuite) requireExactEvent(expected sdk.Event) {
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type == expected.Type {
			s.Require().Equal(expected, event)
			return
		}
	}
	s.Fail("event not found", expected.Type)
}

func (s *ClaimsKeeperTestSuite) mandateMessage() *types.MsgSetClaimsMandate {
	return &types.MsgSetClaimsMandate{
		Authority:                s.authority,
		Committee:                s.committee,
		ActivationHeight:         0,
		ExpiryHeight:             1_000,
		CancellationPeriodBlocks: 2,
		CommitteeClaimLimit:      math.NewInt(100),
	}
}

func (s *ClaimsKeeperTestSuite) createMandate() types.ClaimsMandate {
	_, err := s.msgServer.SetClaimsMandate(s.ctx, s.mandateMessage())
	s.Require().NoError(err)
	mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	return mandate
}

func (s *ClaimsKeeperTestSuite) submission(amount int64) *types.MsgSubmitClaim {
	return &types.MsgSubmitClaim{
		Submitter:         s.committee,
		ExpectedTerm:      1,
		IncidentReference: "incident-1",
		Recipient:         s.recipient,
		Amount:            sdk.NewInt64Coin(chain.MicroNoahDenom, amount),
		EvidenceReference: "evidence-1",
	}
}

func (s *ClaimsKeeperTestSuite) submit(amount int64) types.Claim {
	response, err := s.msgServer.SubmitClaim(s.ctx, s.submission(amount))
	s.Require().NoError(err)
	claim, err := s.keeper.Claims.Get(s.ctx, response.ClaimId)
	s.Require().NoError(err)
	return claim
}

func (s *ClaimsKeeperTestSuite) TestDefaultSentinelAndMandateUpdate() {
	mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	expectedMandate := types.DefaultClaimsMandate()
	s.True(proto.Equal(&mandate, &expectedMandate))

	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.True(insuranceReserved.IsZero())
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())

	mandate = s.createMandate()
	s.Equal(s.committee, mandate.Committee)
	s.Equal(uint64(1), mandate.Term)
	s.Equal(uint64(0), mandate.ActivationHeight)
	s.Equal(uint64(1_000), mandate.ExpiryHeight)
	s.Equal(uint64(2), mandate.CancellationPeriodBlocks)
	s.Equal(math.NewInt(100), mandate.CommitteeClaimLimit)

	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(30)))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.NewInt(20)))
	update := s.mandateMessage()
	update.CancellationPeriodBlocks = 3
	_, err = s.msgServer.SetClaimsMandate(s.ctx, update)
	s.Require().NoError(err)
	mandate, err = s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(2), mandate.Term)
	s.Equal(uint64(3), mandate.CancellationPeriodBlocks)
	allowanceUsed, err = s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())
	insuranceReserved, err = s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(30), insuranceReserved)

	_, err = s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
		Authority: s.authority,
	})
	s.Require().NoError(err)
	mandate, err = s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(types.NewDisabledClaimsMandate(3), mandate)
	allowanceUsed, err = s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())
	insuranceReserved, err = s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(30), insuranceReserved)
}

func (s *ClaimsKeeperTestSuite) TestMandateUpdateRequiresCurrentTermState() {
	s.Require().NoError(s.keeper.ClaimsMandate.Remove(s.ctx))

	_, err := s.msgServer.SetClaimsMandate(s.ctx, s.mandateMessage())
	s.Require().ErrorContains(err, "getting Claims mandate")
}

func (s *ClaimsKeeperTestSuite) TestMandateTermCannotOverflow() {
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, types.NewDisabledClaimsMandate(^uint64(0))))

	_, err := s.msgServer.SetClaimsMandate(s.ctx, s.mandateMessage())
	s.Require().ErrorContains(err, "term cannot advance")
}

func (s *ClaimsKeeperTestSuite) TestMandateValidation() {
	tests := []struct {
		name   string
		mutate func(*types.MsgSetClaimsMandate)
	}{
		{name: "committee is authority", mutate: func(msg *types.MsgSetClaimsMandate) { msg.Committee = s.authority }},
		{name: "invalid active heights", mutate: func(msg *types.MsgSetClaimsMandate) { msg.ActivationHeight = msg.ExpiryHeight }},
		{name: "zero cancellation period", mutate: func(msg *types.MsgSetClaimsMandate) { msg.CancellationPeriodBlocks = 0 }},
		{name: "cancellation period exceeds active span", mutate: func(msg *types.MsgSetClaimsMandate) {
			msg.ExpiryHeight = msg.ActivationHeight + msg.CancellationPeriodBlocks - 1
		}},
		{name: "unset committee claim limit", mutate: func(msg *types.MsgSetClaimsMandate) { msg.CommitteeClaimLimit = math.Int{} }},
		{name: "zero committee claim limit", mutate: func(msg *types.MsgSetClaimsMandate) { msg.CommitteeClaimLimit = math.ZeroInt() }},
		{name: "negative committee claim limit", mutate: func(msg *types.MsgSetClaimsMandate) { msg.CommitteeClaimLimit = math.NewInt(-1) }},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			msg := s.mandateMessage()
			tc.mutate(msg)
			_, err := s.msgServer.SetClaimsMandate(s.ctx, msg)
			s.Require().Error(err)
		})
	}
}

func (s *ClaimsKeeperTestSuite) TestConsensusAuthorityCannotBecomeClaimsRole() {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Authority: &cmtproto.AuthorityParams{Authority: s.caller},
	})
	msg := s.mandateMessage()
	msg.Authority = s.caller
	msg.Committee = s.caller
	_, err := s.msgServer.SetClaimsMandate(s.ctx, msg)
	s.Require().ErrorContains(err, "distinct from Treasury authority")
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimReservesFunds() {
	s.createMandate()
	s.setHeight(25)
	claim := s.submit(80)

	s.Equal(uint64(1), claim.ClaimId)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
	s.Equal(s.committee, claim.Submitter)
	s.Equal(types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE, claim.Origin)
	s.Equal(uint64(1), claim.MandateTerm)
	s.Equal(uint64(25), claim.SubmittedHeight)
	s.Equal(uint64(27), claim.ExecutableHeight)
	s.Zero(claim.FinalizedHeight)
	s.Zero(s.bank.sends)
	s.Equal(math.NewInt(1_000), s.bank.balance)

	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(80), insuranceReserved)
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(80), allowanceUsed)
	s.requireExactEvent(sdk.NewEvent(
		types.EventTypeClaimSubmitted,
		sdk.NewAttribute(types.AttributeKeyClaimID, "1"),
	))

	second := s.submit(1)
	s.Equal(uint64(2), second.ClaimId)
	nextClaimID, err := s.keeper.NextClaimID.Peek(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(3), nextClaimID)
}

func (s *ClaimsKeeperTestSuite) TestGovernanceCanSubmitClaim() {
	s.createMandate()
	msg := s.submission(20)
	msg.Submitter = s.authority

	response, err := s.msgServer.SubmitClaim(s.ctx, msg)
	s.Require().NoError(err)
	claim, err := s.keeper.Claims.Get(s.ctx, response.ClaimId)
	s.Require().NoError(err)
	s.Equal(s.authority, claim.Submitter)
	s.Equal(types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE, claim.Origin)
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())

	msg = s.submission(20)
	msg.Submitter = s.authority
	msg.ExpectedTerm++
	_, err = s.msgServer.SubmitClaim(s.ctx, msg)
	s.Require().ErrorContains(err, "term mismatch")
}

func (s *ClaimsKeeperTestSuite) TestCommitteeClaimLimitIsGrossForTerm() {
	s.createMandate()
	s.setHeight(25)
	cancelledClaim := s.submit(60)

	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 1,
		ClaimId:      cancelledClaim.ClaimId,
		Reason:       "not covered",
	})
	s.Require().NoError(err)
	s.submit(40)

	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(100), allowanceUsed)
	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(40), insuranceReserved)

	_, err = s.msgServer.SubmitClaim(s.ctx, s.submission(1))
	s.Require().ErrorContains(err, "exceeds remaining Claims allowance 0")

	update := s.mandateMessage()
	_, err = s.msgServer.SetClaimsMandate(s.ctx, update)
	s.Require().NoError(err)
	allowanceUsed, err = s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())
	insuranceReserved, err = s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(40), insuranceReserved)

	newTermSubmission := s.submission(100)
	newTermSubmission.ExpectedTerm = 2
	_, err = s.msgServer.SubmitClaim(s.ctx, newTermSubmission)
	s.Require().NoError(err)
	allowanceUsed, err = s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(100), allowanceUsed)
	insuranceReserved, err = s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(140), insuranceReserved)
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimValidation() {
	_, err := s.msgServer.SubmitClaim(s.ctx, s.submission(1))
	s.Require().ErrorContains(err, "not active")

	s.createMandate()
	tests := []struct {
		name      string
		mutate    func(*types.MsgSubmitClaim)
		expectErr string
	}{
		{name: "unrelated submitter", mutate: func(msg *types.MsgSubmitClaim) { msg.Submitter = s.caller }, expectErr: "neither Treasury authority"},
		{name: "wrong denom", mutate: func(msg *types.MsgSubmitClaim) { msg.Amount = sdk.NewInt64Coin("uusd", 1) }, expectErr: "unoah coin"},
		{name: "self payment", mutate: func(msg *types.MsgSubmitClaim) {
			msg.Recipient = authtypes.NewModuleAddress(types.InsuranceName).String()
		}, expectErr: "cannot be the Insurance module"},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			msg := s.submission(1)
			tc.mutate(msg)
			_, err := s.msgServer.SubmitClaim(s.ctx, msg)
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}

	s.bank.balance = math.NewInt(50)
	_, err = s.msgServer.SubmitClaim(s.ctx, s.submission(51))
	s.Require().ErrorContains(err, "cannot cover Insurance reservation")
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimRejectsBlockedRecipientBeforeReservation() {
	s.createMandate()
	blockedRecipient := sdk.AccAddress(bytes.Repeat([]byte{5}, 20))
	s.bank.blocked[blockedRecipient.String()] = true
	msg := s.submission(10)
	msg.Recipient = blockedRecipient.String()

	_, err := s.msgServer.SubmitClaim(s.ctx, msg)
	s.Require().ErrorContains(err, "blocked from receiving funds")

	exists, err := s.keeper.Claims.Has(s.ctx, 1)
	s.Require().NoError(err)
	s.False(exists)
	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.True(insuranceReserved.IsZero())
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())
	nextClaimID, err := s.keeper.NextClaimID.Peek(s.ctx)
	s.Require().NoError(err)
	s.Equal(uint64(1), nextClaimID)
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimRejectsExhaustedIDSequenceBeforeAccounting() {
	s.createMandate()
	s.Require().NoError(s.keeper.NextClaimID.Set(s.ctx, ^uint64(0)))

	_, err := s.msgServer.SubmitClaim(s.ctx, s.submission(10))
	s.Require().ErrorContains(err, "claim ID sequence is exhausted")

	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.True(insuranceReserved.IsZero())
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())
	nextClaimID, err := s.keeper.NextClaimID.Peek(s.ctx)
	s.Require().NoError(err)
	s.Equal(^uint64(0), nextClaimID)
}

func (s *ClaimsKeeperTestSuite) TestClaimActionsRejectZeroID() {
	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{})
	s.Require().ErrorContains(err, "claim ID must be positive")

	_, err = s.msgServer.ExecuteClaim(s.ctx, &types.MsgExecuteClaim{})
	s.Require().ErrorContains(err, "claim ID must be positive")
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimEnforcesMandateTermAndWindow() {
	s.createMandate()
	s.setHeight(25)
	stale := s.submission(1)
	stale.ExpectedTerm++
	_, err := s.msgServer.SubmitClaim(s.ctx, stale)
	s.Require().ErrorContains(err, "term mismatch")

	s.setHeight(1_000)
	_, err = s.msgServer.SubmitClaim(s.ctx, s.submission(1))
	s.Require().ErrorContains(err, "not active")

	s.setHeight(25)
	update := s.mandateMessage()
	update.ExpiryHeight = 26
	_, err = s.msgServer.SetClaimsMandate(s.ctx, update)
	s.Require().NoError(err)
	crossesExpiry := s.submission(1)
	crossesExpiry.ExpectedTerm = 2
	_, err = s.msgServer.SubmitClaim(s.ctx, crossesExpiry)
	s.Require().ErrorContains(err, "exceeds Claims mandate expiry height")
}

func (s *ClaimsKeeperTestSuite) TestSubmitClaimRejectsExecutableHeightOverflow() {
	update := s.mandateMessage()
	update.ExpiryHeight = ^uint64(0)
	update.CancellationPeriodBlocks = ^uint64(0)
	_, err := s.msgServer.SetClaimsMandate(s.ctx, update)
	s.Require().NoError(err)

	s.setHeight(1)
	_, err = s.msgServer.SubmitClaim(s.ctx, s.submission(1))
	s.Require().ErrorContains(err, "executable height overflows")
}

func (s *ClaimsKeeperTestSuite) TestCommitteeAndGovernanceCanCancelDuringPeriod() {
	s.createMandate()
	s.setHeight(25)
	committeeClaim := s.submit(40)
	governanceClaim := s.submit(30)

	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:  s.caller,
		ClaimId: committeeClaim.ClaimId,
		Reason:  "unauthorized",
	})
	s.Require().ErrorContains(err, "neither Treasury authority")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 2,
		ClaimId:      committeeClaim.ClaimId,
		Reason:       "stale committee transaction",
	})
	s.Require().ErrorContains(err, "term mismatch")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 1,
		ClaimId:      committeeClaim.ClaimId,
		Reason:       "not covered",
		Reference:    "case-1",
	})
	s.Require().NoError(err)
	committeeClaim, err = s.keeper.Claims.Get(s.ctx, committeeClaim.ClaimId)
	s.Require().NoError(err)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_CANCELLED, committeeClaim.Status)
	s.Equal(s.committee, committeeClaim.FinalizedBy)
	s.Equal("case-1", committeeClaim.CancellationReference)

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:  s.authority,
		ClaimId: governanceClaim.ClaimId,
		Reason:  "governance veto",
	})
	s.Require().NoError(err)
	governanceClaim, err = s.keeper.Claims.Get(s.ctx, governanceClaim.ClaimId)
	s.Require().NoError(err)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_CANCELLED, governanceClaim.Status)
	s.Equal(s.authority, governanceClaim.FinalizedBy)

	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.True(insuranceReserved.IsZero())
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(70), allowanceUsed)
	s.Zero(s.bank.sends)
}

func (s *ClaimsKeeperTestSuite) TestCommitteeCannotCancelGovernanceClaim() {
	s.createMandate()
	s.setHeight(25)
	msg := s.submission(20)
	msg.Submitter = s.authority
	response, err := s.msgServer.SubmitClaim(s.ctx, msg)
	s.Require().NoError(err)
	// Rotate the effective governance authority after submission. The stored
	// origin, not current address equality, continues to protect the claim.
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Authority: &cmtproto.AuthorityParams{Authority: s.caller},
	})

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 1,
		ClaimId:      response.ClaimId,
		Reason:       "committee veto",
	})
	s.Require().ErrorContains(err, "cannot cancel governance-submitted claim")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:  s.caller,
		ClaimId: response.ClaimId,
		Reason:  "governance cancellation",
	})
	s.Require().NoError(err)
}

func (s *ClaimsKeeperTestSuite) TestGovernanceCancellationDoesNotDependOnCurrentMandate() {
	s.createMandate()
	s.setHeight(25)
	claim := s.submit(20)

	_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
		Authority: s.authority,
	})
	s.Require().NoError(err)

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 2,
		ClaimId:      claim.ClaimId,
		Reason:       "disabled committee",
	})
	s.Require().ErrorContains(err, "neither Treasury authority")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:  s.authority,
		ClaimId: claim.ClaimId,
		Reason:  "governance cancellation",
	})
	s.Require().NoError(err)
}

func (s *ClaimsKeeperTestSuite) TestCancellationBoundaryIsExclusive() {
	s.createMandate()
	s.setHeight(25)
	claim := s.submit(20)
	s.setHeight(27)

	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 1,
		ClaimId:      claim.ClaimId,
		Reason:       "too late",
	})
	s.Require().ErrorContains(err, "ended at height 27")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:  s.authority,
		ClaimId: claim.ClaimId,
		Reason:  "governance cancellation after deadline",
	})
	s.Require().ErrorContains(err, "ended at height 27")
	claim, err = s.keeper.Claims.Get(s.ctx, claim.ClaimId)
	s.Require().NoError(err)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
}

func (s *ClaimsKeeperTestSuite) TestExecuteClaimAtAndAfterBoundary() {
	s.createMandate()
	s.setHeight(25)
	claim := s.submit(80)

	s.setHeight(26)
	_, err := s.msgServer.ExecuteClaim(s.ctx, &types.MsgExecuteClaim{
		Caller:  s.caller,
		ClaimId: claim.ClaimId,
	})
	s.Require().ErrorContains(err, "not executable before height 27")

	s.setHeight(100)
	s.bank.sendErr = errors.New("bank unavailable")
	_, err = s.msgServer.ExecuteClaim(s.ctx, &types.MsgExecuteClaim{
		Caller:  s.caller,
		ClaimId: claim.ClaimId,
	})
	s.Require().ErrorContains(err, "bank unavailable")
	stored, err := s.keeper.Claims.Get(s.ctx, claim.ClaimId)
	s.Require().NoError(err)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_PENDING, stored.Status)

	s.bank.sendErr = nil
	_, err = s.msgServer.ExecuteClaim(s.ctx, &types.MsgExecuteClaim{
		Caller:  s.caller,
		ClaimId: claim.ClaimId,
	})
	s.Require().NoError(err)
	paid, err := s.keeper.Claims.Get(s.ctx, claim.ClaimId)
	s.Require().NoError(err)
	s.Equal(types.ClaimStatus_CLAIM_STATUS_PAID, paid.Status)
	s.Equal(uint64(100), paid.FinalizedHeight)
	s.Equal(s.caller, paid.FinalizedBy)
	s.Equal(1, s.bank.sends)
	s.Equal(math.NewInt(920), s.bank.balance)
	s.Equal(s.recipient, s.bank.lastRecipient.String())
	s.requireExactEvent(sdk.NewEvent(
		types.EventTypeClaimPaid,
		sdk.NewAttribute(types.AttributeKeyClaimID, "1"),
		sdk.NewAttribute(types.AttributeKeyRecipient, s.recipient),
		sdk.NewAttribute(types.AttributeKeyAmount, "80unoah"),
	))

	insuranceReserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.True(insuranceReserved.IsZero())
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(math.NewInt(80), allowanceUsed)

	_, err = s.msgServer.ExecuteClaim(s.ctx, &types.MsgExecuteClaim{
		Caller:  s.caller,
		ClaimId: claim.ClaimId,
	})
	s.Require().ErrorContains(err, "not pending")
}

func (s *ClaimsKeeperTestSuite) TestMandateRotationDoesNotRewritePendingClaim() {
	s.createMandate()
	s.setHeight(25)
	claim := s.submit(20)

	newCommittee := sdk.AccAddress(bytes.Repeat([]byte{5}, 20)).String()
	update := s.mandateMessage()
	update.Committee = newCommittee
	_, err := s.msgServer.SetClaimsMandate(s.ctx, update)
	s.Require().NoError(err)

	stored, err := s.keeper.Claims.Get(s.ctx, claim.ClaimId)
	s.Require().NoError(err)
	s.Equal(s.committee, stored.Submitter)
	s.Equal(uint64(1), stored.MandateTerm)
	s.Equal(claim.ExecutableHeight, stored.ExecutableHeight)
	allowanceUsed, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.True(allowanceUsed.IsZero())

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       s.committee,
		ExpectedTerm: 1,
		ClaimId:      claim.ClaimId,
		Reason:       "old committee",
	})
	s.Require().ErrorContains(err, "neither Treasury authority")

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Signer:       newCommittee,
		ExpectedTerm: 2,
		ClaimId:      claim.ClaimId,
		Reason:       "current committee",
	})
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRejectsUnconfiguredReferenceDenom() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Denom = chain.MicroUSDDenom
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().ErrorContains(err, "not configured in oracle")
}

func (s *KeeperTestSuite) TestMsgUpdateParamsSwitchesReferenceAndCopiesItsCapDirectly() {
	current := types.DefaultParams()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ReplaceTaxCaps(s.ctx, []types.TaxCap{
		{Denom: chain.MicroKRWDenom, TaxCap: math.NewInt(9)},
		{Denom: chain.MicroSDRDenom, TaxCap: math.NewInt(8)},
		{Denom: chain.MicroUSDDenom, TaxCap: math.NewInt(7)},
	}))

	candidate := current
	candidate.ReferenceTaxCap = sdk.NewInt64Coin(chain.MicroUSDDenom, 101)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(), chain.MicroUSDDenom, chain.MicroKRWDenom, chain.MicroSDRDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroKRWDenom: math.LegacyNewDec(2),
		chain.MicroSDRDenom: math.LegacyNewDec(3),
		chain.MicroUSDDenom: math.LegacyNewDec(7),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    candidate,
	})
	s.Require().NoError(err)
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(candidate, stored)
	referenceCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(101), referenceCap)
	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(28), krwCap)
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroSDRDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(43), sdrCap)
}

func (s *KeeperTestSuite) TestMsgUpdateParamsRateFailurePreservesOldParamsAndCaps() {
	tests := []struct {
		name  string
		rates oracletypes.RateSnapshot
		err   error
	}{
		{
			name: "missing candidate rate",
			rates: oracletypes.RateSnapshot{
				chain.MicroSDRDenom: math.LegacyOneDec(),
			},
		},
		{
			name: "stale snapshot",
			err:  oracletypes.ErrStaleExchangeRate,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			current := types.DefaultParams()
			s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
			oldCaps := []types.TaxCap{
				{Denom: chain.MicroSDRDenom, TaxCap: math.NewInt(33)},
				{Denom: chain.MicroUSDDenom, TaxCap: math.NewInt(44)},
			}
			s.Require().NoError(s.keeper.ReplaceTaxCaps(s.ctx, oldCaps))
			candidate := current
			candidate.ReferenceTaxCap = sdk.NewInt64Coin(chain.MicroUSDDenom, 101)
			s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
				{Denom: chain.MicroSDRDenom},
				{Denom: chain.MicroUSDDenom},
			}, nil)
			s.oracleKeeper.EXPECT().GetRateSnapshot(
				gomock.Any(), chain.MicroSDRDenom, chain.MicroUSDDenom,
			).Return(test.rates, test.err)

			_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: s.authority,
				Params:    candidate,
			})
			s.Require().Error(err)
			stored, getErr := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(getErr)
			s.Require().Equal(current, stored)
			for _, oldCap := range oldCaps {
				storedCap, getErr := s.keeper.TaxCaps.Get(s.ctx, oldCap.Denom)
				s.Require().NoError(getErr)
				s.Require().Equal(oldCap.TaxCap, storedCap)
			}
			for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
				s.Require().NotEqual(types.EventTypeTaxCapsUpdated, event.Type)
			}
		})
	}
}

func (s *KeeperTestSuite) TestMsgTransferReserveToBufferRejectsInvalidAuthority() {
	_, err := s.msgServer.TransferReserveToBuffer(
		s.ctx,
		&types.MsgTransferReserveToBuffer{
			Authority:             "not-authority",
			Amount:                sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
		},
	)
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
}

func (s *KeeperTestSuite) TestMsgTransferReserveToBufferHonoursFloor() {
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.MicroNoahDenom).
		Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 10))

	_, err := s.msgServer.TransferReserveToBuffer(
		s.ctx,
		&types.MsgTransferReserveToBuffer{
			Authority:             s.authority,
			Amount:                sdk.NewInt64Coin(chain.MicroNoahDenom, 7),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.MicroNoahDenom, 4),
		},
	)
	s.Require().ErrorContains(err, "cannot fund")
}

func (s *KeeperTestSuite) TestMsgTransferReserveToBufferRejectsInvalidCoins() {
	tests := []struct {
		name    string
		amount  sdk.Coin
		minimum sdk.Coin
		wantErr string
	}{
		{
			name:    "zero amount",
			amount:  sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			minimum: sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			wantErr: "invalid transfer amount",
		},
		{
			name:    "negative amount",
			amount:  sdk.Coin{Denom: chain.MicroNoahDenom, Amount: math.NewInt(-1)},
			minimum: sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			wantErr: "invalid transfer amount",
		},
		{
			name:    "wrong amount denom",
			amount:  sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
			minimum: sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			wantErr: "invalid transfer amount",
		},
		{
			name:    "malformed amount denom",
			amount:  sdk.Coin{Denom: "BAD DENOM", Amount: math.OneInt()},
			minimum: sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			wantErr: "invalid transfer amount",
		},
		{
			name:    "negative minimum",
			amount:  sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			minimum: sdk.Coin{Denom: chain.MicroNoahDenom, Amount: math.NewInt(-1)},
			wantErr: "invalid minimum reserve balance",
		},
		{
			name:    "wrong minimum denom",
			amount:  sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			minimum: sdk.NewInt64Coin(chain.MicroUSDDenom, 0),
			wantErr: "invalid minimum reserve balance",
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			_, err := s.msgServer.TransferReserveToBuffer(
				s.ctx,
				&types.MsgTransferReserveToBuffer{
					Authority:             s.authority,
					Amount:                test.amount,
					MinimumReserveBalance: test.minimum,
				},
			)
			s.Require().ErrorContains(err, test.wantErr)
		})
	}
}

func (s *KeeperTestSuite) TestMsgTransferReserveToBufferTransfersFundsWithoutCustomEvent() {
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(), authtypes.NewModuleAddress(types.StrategicReserveName), chain.MicroNoahDenom,
	).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 10))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 6)),
	).Return(nil)

	_, err := s.msgServer.TransferReserveToBuffer(
		s.ctx,
		&types.MsgTransferReserveToBuffer{
			Authority:             s.authority,
			Amount:                sdk.NewInt64Coin(chain.MicroNoahDenom, 6),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.MicroNoahDenom, 4),
		},
	)
	s.Require().NoError(err)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestMsgTransferReserveToBufferReturnsBankFailureWithoutEvent() {
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(), authtypes.NewModuleAddress(types.StrategicReserveName), chain.MicroNoahDenom,
	).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 10))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 6)),
	).Return(errors.New("injected bank failure"))

	_, err := s.msgServer.TransferReserveToBuffer(
		s.ctx,
		&types.MsgTransferReserveToBuffer{
			Authority:             s.authority,
			Amount:                sdk.NewInt64Coin(chain.MicroNoahDenom, 6),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.MicroNoahDenom, 4),
		},
	)
	s.Require().ErrorContains(err, "injected bank failure")
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}
