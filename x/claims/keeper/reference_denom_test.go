package keeper_test

import (
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// threeQuarters is a cross of three quarters: XDR at one and a half NOAH, USD at two.
func threeQuarters() oracletypes.RateSet {
	return oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
		chain.XDRBaseDenom: math.LegacyNewDecWithPrec(15, 1),
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})
}

// claimsRebases returns every claims rebase event the block has published.
func (s *KeeperTestSuite) claimsRebases() []*types.EventClaimsRebased {
	var out []*types.EventClaimsRebased
	for _, e := range sdk.UnwrapSDKContext(s.ctx).EventManager().ABCIEvents() {
		if e.Type != proto.MessageName(&types.EventClaimsRebased{}) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(e)
		s.Require().NoError(err)
		out = append(out, parsed.(*types.EventClaimsRebased))
	}
	return out
}

func (s *KeeperTestSuite) claimLimit() math.Int {
	mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	return mandate.CommitteeClaimLimit
}

// TestRebaseReference moves the limit, every committee claim's charge, a cancelled one and an earlier
// term's included, and the live term's usage at a three-quarter cross, each rounded up; a governance
// claim's zero charge stays zero.
func (s *KeeperTestSuite) TestRebaseReference() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	s.fundInsurance(5_000)
	live, err := s.submitCommittee(noahCoin(301), 0)
	s.Require().NoError(err)
	cancelled, err := s.submitCommittee(noahCoin(200), 0)
	s.Require().NoError(err)
	_, err = s.msgServer.CommitteeCancelClaim(s.ctx, &types.MsgCommitteeCancelClaim{Committee: testAddress(1), ExpectedTerm: 1, ClaimId: cancelled})
	s.Require().NoError(err)
	governance, err := s.submitGovernance(noahCoin(100), 0)
	s.Require().NoError(err)
	// An earlier term's claim is rebased but charges nothing to the live term.
	earlier := s.claim(live)
	earlier.ClaimId = 4
	mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	mandate.Term = 2
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, mandate))
	for _, id := range []uint64{live, cancelled} {
		claim := s.claim(id)
		claim.MandateTerm = 2
		s.Require().NoError(s.keeper.Claims.Set(s.ctx, id, claim))
	}
	s.Require().NoError(s.keeper.Claims.Set(s.ctx, earlier.ClaimId, earlier))

	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, threeQuarters()))

	s.requireInt(s.claim(live).Charge, 226)
	s.requireInt(s.claim(cancelled).Charge, 150)
	s.requireInt(s.claim(earlier.ClaimId).Charge, 226)
	s.Require().True(s.claim(governance).Charge.IsZero())
	used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(used, 376)
	s.requireInt(s.claimLimit(), 750)

	events := s.claimsRebases()
	s.Require().Len(events, 1)
	s.Require().Equal(sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000).String(), events[0].OldLimit.String())
	s.Require().Equal(sdk.NewInt64Coin(chain.USDBaseDenom, 750).String(), events[0].NewLimit.String())
	s.Require().Equal(sdk.NewInt64Coin(chain.XDRBaseDenom, 501).String(), events[0].OldUsed.String())
	s.Require().Equal(sdk.NewInt64Coin(chain.USDBaseDenom, 376).String(), events[0].NewUsed.String())
	s.Require().EqualValues(3, events[0].Claims)
}

// TestRebaseReferenceRefusesAChargeRoundingToZero pins that a pending committee claim's charge never
// rebases to zero: the record would fail its own validation at settlement, failing the claim.
func (s *KeeperTestSuite) TestRebaseReferenceRefusesAChargeRoundingToZero() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	s.fundInsurance(5_000)
	id, err := s.submitCommittee(noahCoin(1), 0)
	s.Require().NoError(err)
	rates := oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
		chain.XDRBaseDenom: math.LegacyNewDecWithPrec(1, 18),
		chain.USDBaseDenom: math.LegacyNewDecFromInt(math.NewIntWithDecimal(1, 30)),
	})

	err = s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates)
	s.Require().ErrorContains(err, "rebasing the charge of claim 1: amount must be positive")
	s.requireInt(s.claim(id).Charge, 1)
	s.Require().Empty(s.claimsRebases())
}

func (s *KeeperTestSuite) TestRebaseReferenceToTheSameDenomIsANoOp() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.XDRBaseDenom, nil))
	s.requireInt(s.claimLimit(), 1_000)
	s.Require().Empty(s.claimsRebases())
}

// TestRebaseReferenceRaisesTheLimitToTheUsage pins the clamp: three claims of two each exhaust a limit
// of six, and at a three-quarter cross each rounds up to two while the limit alone would round to
// five, so it is raised to the six the term has charged.
func (s *KeeperTestSuite) TestRebaseReferenceRaisesTheLimitToTheUsage() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
	s.Require().NoError(err)
	mandate.CommitteeClaimLimit = math.NewInt(6)
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, mandate))
	s.fundInsurance(5_000)
	for range 3 {
		_, err := s.submitCommittee(noahCoin(2), 0)
		s.Require().NoError(err)
	}

	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, threeQuarters()))

	used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(used, 6)
	s.requireInt(s.claimLimit(), 6)
}

// TestRebaseReferenceFailuresPreserveState checks an unpriced leg and a figure past the amount bound
// both fail before any write.
func (s *KeeperTestSuite) TestRebaseReferenceFailuresPreserveState() {
	for _, tc := range []struct {
		name  string
		rates oracletypes.RateSet
		want  string
	}{
		{
			name:  "unpriced incoming unit",
			rates: oracletypes.NewRateSetFrom(map[string]math.LegacyDec{chain.XDRBaseDenom: math.LegacyOneDec()}),
			want:  chain.USDBaseDenom,
		},
		{
			// A tiny outgoing unit and a vast incoming one take the limit below a base unit.
			name: "limit rounding to zero",
			rates: oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
				chain.XDRBaseDenom: math.LegacyNewDecWithPrec(1, 18),
				chain.USDBaseDenom: math.LegacyNewDecFromInt(math.NewIntWithDecimal(1, 30)),
			}),
			want: "rebasing the committee claim limit: amount must be positive",
		},
		{
			name: "limit past the amount bound",
			rates: oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
				chain.XDRBaseDenom: math.LegacyNewDecFromInt(math.NewIntWithDecimal(1, 18)),
				chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
			}),
			want: "128 bits",
		},
	} {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.atSubmission()
			s.appointCommittee(testAddress(1))
			mandate, err := s.keeper.ClaimsMandate.Get(s.ctx)
			s.Require().NoError(err)
			mandate.CommitteeClaimLimit = math.NewIntWithDecimal(1, 20)
			s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, mandate))

			err = s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, tc.rates)
			s.Require().ErrorContains(err, tc.want)
			s.Require().True(s.claimLimit().Equal(math.NewIntWithDecimal(1, 20)))
			s.Require().Empty(s.claimsRebases())
		})
	}
}
