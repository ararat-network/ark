package keeper_test

import (
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// rebaseRates prices the outgoing XDR and incoming USD units, NOAH per unit, as Oracle hands them.
func rebaseRates(from, to math.LegacyDec) oracletypes.RateSet {
	return oracletypes.NewRateSetFrom(map[string]math.LegacyDec{chain.XDRBaseDenom: from, chain.USDBaseDenom: to})
}

// threeQuarters is a cross of three quarters: XDR at one and a half NOAH, USD at two.
func threeQuarters() oracletypes.RateSet {
	return rebaseRates(math.LegacyNewDecWithPrec(15, 1), math.LegacyNewDec(2))
}

// compensationRebases returns every compensation rebase event the block has published.
func (s *KeeperTestSuite) compensationRebases() []*types.EventCompensationRebased {
	var out []*types.EventCompensationRebased
	for _, e := range s.sdkCtx().EventManager().ABCIEvents() {
		if e.Type != proto.MessageName(&types.EventCompensationRebased{}) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(e)
		s.Require().NoError(err)
		out = append(out, parsed.(*types.EventCompensationRebased))
	}
	return out
}

func (s *KeeperTestSuite) requireInt(got math.Int, want int64) {
	s.T().Helper()
	s.Require().True(got.Equal(math.NewInt(want)), "got %s, want %d", got, want)
}

func (s *KeeperTestSuite) requireCoin(got sdk.Coin, denom string, amount int64) {
	s.T().Helper()
	s.Require().Equal(denom, got.Denom)
	s.requireInt(got.Amount, amount)
}

func (s *KeeperTestSuite) compensationCap() math.Int {
	p, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	return p.CompensationCap
}

// TestRebaseReference moves the cap, every compensation award's value, a cancelled one included,
// the live term's usage, and its allowance at a three-quarter cross, each rounded up, and leaves a
// member's zero value alone; the export then reconciles in the new unit.
func (s *KeeperTestSuite) TestRebaseReference() {
	s.fund(types.ContributorsName, chain.NoahCoin(math.NewInt(1_000_000)))
	s.appointCommittee(1_000)
	s.appointRegistrar()
	s.openTranche(100_000)
	member := s.register(testAddress(5))
	governance, err := s.compensate(testAddress(3), chain.NoahCoin(math.NewInt(301)), s.now(), 1, math.ZeroInt())
	s.Require().NoError(err)
	committee, err := s.committeeCompensate(testAddress(4), chain.NoahCoin(math.NewInt(500)), 1)
	s.Require().NoError(err)
	s.Require().NoError(s.cancel(committee))
	oldCap := s.compensationCap()

	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, threeQuarters()))

	s.Require().True(s.compensationCap().Equal(oldCap.MulRaw(3).QuoRaw(4)))
	s.requireInt(s.grant(governance).AwardValue, 226)
	s.requireInt(s.grant(committee).AwardValue, 375)
	s.Require().True(s.grant(member).AwardValue.IsZero())
	used, err := s.keeper.CompensationUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(used, 375)
	mandate, err := s.keeper.GrantsMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(mandate.CompensationAllowance, 750)

	events := s.compensationRebases()
	s.Require().Len(events, 1)
	s.Require().True(events[0].OldCap.Amount.Equal(oldCap))
	s.Require().Equal(chain.XDRBaseDenom, events[0].OldCap.Denom)
	s.Require().True(events[0].NewCap.Amount.Equal(s.compensationCap()))
	s.Require().Equal(chain.USDBaseDenom, events[0].NewCap.Denom)
	s.requireCoin(events[0].OldAllowance, chain.XDRBaseDenom, 1_000)
	s.requireCoin(events[0].NewAllowance, chain.USDBaseDenom, 750)
	s.requireCoin(events[0].OldUsed, chain.XDRBaseDenom, 500)
	s.requireCoin(events[0].NewUsed, chain.USDBaseDenom, 375)
	s.Require().EqualValues(2, events[0].Grants)

	s.reference = chain.USDBaseDenom
	s.export()
}

func (s *KeeperTestSuite) TestRebaseReferenceToTheSameDenomIsANoOp() {
	oldCap := s.compensationCap()
	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.XDRBaseDenom, nil))
	s.Require().True(s.compensationCap().Equal(oldCap))
	s.Require().Empty(s.compensationRebases())
}

// TestRebaseReferenceRaisesTheAllowanceToTheUsage pins the clamp: three awards of two each exhaust
// an allowance of six, and at a three-quarter cross each rounds up to two while the allowance
// alone would round to five, so it is raised to the six the term has charged and the export
// still reconciles.
func (s *KeeperTestSuite) TestRebaseReferenceRaisesTheAllowanceToTheUsage() {
	s.fund(types.ContributorsName, chain.NoahCoin(math.NewInt(1_000_000)))
	s.appointCommittee(6)
	for i := byte(3); i <= 5; i++ {
		_, err := s.committeeCompensate(testAddress(i), chain.NoahCoin(math.NewInt(2)), 1)
		s.Require().NoError(err)
	}

	s.Require().NoError(s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, threeQuarters()))

	used, err := s.keeper.CompensationUsed.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(used, 6)
	mandate, err := s.keeper.GrantsMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.requireInt(mandate.CompensationAllowance, 6)
	events := s.compensationRebases()
	s.Require().Len(events, 1)
	s.requireCoin(events[0].NewAllowance, chain.USDBaseDenom, 6)

	s.reference = chain.USDBaseDenom
	s.export()
}

// TestRebaseReferenceFailuresPreserveState checks an unpriced leg and a figure the amount bound
// refuses both fail before any write.
func (s *KeeperTestSuite) TestRebaseReferenceFailuresPreserveState() {
	oldCap := s.compensationCap()
	cases := []struct {
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
			name:  "cap past the amount bound",
			rates: rebaseRates(math.LegacyNewDecFromInt(math.NewIntWithDecimal(1, 18)), math.LegacyNewDecWithPrec(1, 18)),
			want:  "128 bits",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			err := s.keeper.RebaseReference(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, tc.rates)
			s.Require().ErrorContains(err, tc.want)
			s.Require().True(s.compensationCap().Equal(oldCap))
			s.Require().Empty(s.compensationRebases())
		})
	}
}
