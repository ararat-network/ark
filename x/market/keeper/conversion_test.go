package keeper_test

import (
	"bytes"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// absentCommitteeShape is the observation an appointment records for a
// committee address holding no account, which is every committee here: these
// suites drive the keeper directly rather than through a signed transaction,
// so no committee account is ever created.
var absentCommitteeShape = mandate.CommitteeShape{
	KeyKind: mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_ABSENT,
}

const (
	capacityActivation = 10
	capacityExpiry     = 20
)

func capacityCommittee() string {
	return sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
}

// livePool is the depth every conversion-mandate test starts from, so the corridor
// below brackets it: half to double, with the recovery period allowed to shorten
// to a quarter. Deepening the pool while shortening recovery is the historical
// depeg response the fast path exists for.
func livePool() math.LegacyDec {
	return math.LegacyNewDec(100)
}

// capacityCorridor bounds depth and the recovery period in both directions, and
// pins the spread floor's own floor at launch: the committee may lift it to four
// times launch and walk it back, but never below the rate governance set.
func capacityCorridor() (types.ConversionPolicy, types.ConversionPolicy) {
	minimum := types.ConversionPolicy{
		BasePool:           xdrBasePool(livePool().QuoInt64(2)),
		PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod / 4,
		MinStabilitySpread: types.DefaultMinStabilitySpread,
	}
	maximum := types.ConversionPolicy{
		BasePool:           xdrBasePool(livePool().MulInt64(2)),
		PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod,
		MinStabilitySpread: types.DefaultMinStabilitySpread.MulInt64(4),
	}

	return minimum, maximum
}

// seedLiveConversionMandate puts the suite at a height inside an appointed
// committee's window, with the live pool at livePool() and a zero delta.
func (s *KeeperTestSuite) seedLiveConversionMandate() types.ConversionMandate {
	s.setCapacityHeight(capacityActivation)
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(livePool())
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

	minimum, maximum := capacityCorridor()
	_, err := s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
		Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Committee:        capacityCommittee(),
		ActivationHeight: capacityActivation,
		ExpiryHeight:     capacityExpiry,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
		MaxTobinTax:      math.LegacyZeroDec(),
	})
	s.Require().NoError(err)

	appointment, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)

	return appointment
}

func (s *KeeperTestSuite) setCapacityHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

// oracleRateSetForRebase is the pair x/asset hands in when it re-points the
// reference: one XDR buys two USD, so the pool's depth doubles in the new unit.
func oracleRateSetForRebase() oracletypes.RateSet {
	return oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDec(2),
	}
}

// candidateInsideCorridor changes depth, recovery period, and spread floor together within the
// seeded mandate bounds.
func candidateInsideCorridor() types.ConversionPolicy {
	return types.ConversionPolicy{
		BasePool:           xdrBasePool(livePool().MulInt64(2)),
		PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod / 4,
		MinStabilitySpread: types.DefaultMinStabilitySpread.MulInt64(2),
	}
}

func (s *KeeperTestSuite) TestSetMandate() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	appointment := s.seedLiveConversionMandate()

	s.Require().Equal(uint64(1), appointment.Term)
	s.Require().Equal(capacityCommittee(), appointment.Committee)
	live, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(appointment.IsActive(live, capacityActivation))
	s.requireTypedEvent(&types.EventConversionMandateSet{
		Term:             1,
		Committee:        capacityCommittee(),
		ActivationHeight: capacityActivation,
		ExpiryHeight:     capacityExpiry,
		CommitteeShape:   absentCommitteeShape,
	})

	// Replacement advances the term, so a transaction prepared under the old
	// appointment can never apply under the new one.
	_, err = s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
		Authority: authority,
	})
	s.Require().NoError(err)
	disabled, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(disabled.IsDisabled())
	s.Require().Equal(uint64(2), disabled.Term)
	s.Require().True(disabled.MinimumPolicy.IsZero())
	s.Require().True(disabled.MaximumPolicy.IsZero())

	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy:       candidateInsideCorridor(),
	})
	s.Require().ErrorContains(err, "not the exact appointed committee")
}

func (s *KeeperTestSuite) TestSetMandateRejections() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	minimum, maximum := capacityCorridor()

	tests := []struct {
		name      string
		mutate    func(*types.MsgSetConversionMandate)
		expectErr string
		errorIs   error
	}{
		{
			name:      "invalid authority",
			mutate:    func(msg *types.MsgSetConversionMandate) { msg.Authority = "invalid_authority" },
			expectErr: "invalid authority",
			errorIs:   errortypes.ErrUnauthorized,
		},
		{
			// Governance already holds the unbounded path, so appointing itself
			// is a delegation to nobody that still reads as a live fast path.
			name:      "committee is the authority",
			mutate:    func(msg *types.MsgSetConversionMandate) { msg.Committee = authority },
			expectErr: "must be distinct from Market authority",
		},
		{
			name:      "inverted window",
			mutate:    func(msg *types.MsgSetConversionMandate) { msg.ExpiryHeight = msg.ActivationHeight },
			expectErr: "conversion mandate: activation height must precede expiry height",
		},
		{
			name: "inverted corridor",
			mutate: func(msg *types.MsgSetConversionMandate) {
				msg.MinimumPolicy, msg.MaximumPolicy = msg.MaximumPolicy, msg.MinimumPolicy
			},
			expectErr: "minimum base pool",
		},
		{
			// A corridor in another unit could never authorize an action. It is
			// refused at appointment rather than discovered when the committee
			// musters, which is the worst possible moment.
			name: "bounds denominated off the live pool",
			mutate: func(msg *types.MsgSetConversionMandate) {
				msg.MinimumPolicy.BasePool = sdk.NewDecCoinFromDec(
					chain.USDBaseDenom,
					msg.MinimumPolicy.BasePool.Amount,
				)
				msg.MaximumPolicy.BasePool = sdk.NewDecCoinFromDec(
					chain.USDBaseDenom,
					msg.MaximumPolicy.BasePool.Amount,
				)
			},
			expectErr: "conversion bounds are denominated in ausd, not the live base pool axdr",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			current := types.DefaultConversionPolicy()
			current.BasePool = xdrBasePool(livePool())
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
			before, err := s.keeper.ConversionMandate.Get(s.ctx)
			s.Require().NoError(err)

			msg := &types.MsgSetConversionMandate{
				Authority:        authority,
				Committee:        capacityCommittee(),
				ActivationHeight: capacityActivation,
				ExpiryHeight:     capacityExpiry,
				MinimumPolicy:    minimum,
				MaximumPolicy:    maximum,
				MaxTobinTax:      math.LegacyZeroDec(),
			}
			tc.mutate(msg)

			_, err = s.msgServer.SetConversionMandate(s.ctx, msg)
			s.Require().ErrorContains(err, tc.expectErr)
			if tc.errorIs != nil {
				s.Require().ErrorIs(err, tc.errorIs)
			}

			// A refused appointment leaves the previous one untouched, term
			// included: a failed proposal must not burn a term.
			after, err := s.keeper.ConversionMandate.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(before, after)
		})
	}
}

// TestExportAfterRebaseRoundTrips checks import accepts a reachable stranded mandate: pool rebasing
// changes its unit while the approved corridor retains its own.
func (s *KeeperTestSuite) TestExportAfterRebaseRoundTrips() {
	appointment := s.seedLiveConversionMandate()
	s.Require().False(appointment.IsDisabled())
	s.accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName)).
		AnyTimes()
	s.oracleKeeper.EXPECT().
		GetReferenceDenom(gomock.Any()).
		Return(chain.USDBaseDenom, nil).
		AnyTimes()

	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracleRateSetForRebase(),
	))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	// The export is exactly the stranded shape: pool in the new unit, corridor
	// still in the old one.
	s.Require().Equal(chain.USDBaseDenom, exported.ConversionPolicy.BasePool.Denom)
	s.Require().Equal(chain.XDRBaseDenom, exported.ConversionMandate.MinimumPolicy.BasePool.Denom)

	s.Require().NoError(exported.Validate())
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
}

// TestStrandedMandateIsNotActive checks that policy authority requires matching live pool units as
// well as an open window. Envelope activity alone does not grant policy authority.
func (s *KeeperTestSuite) TestStrandedMandateIsNotActive() {
	appointment := s.seedLiveConversionMandate()
	live, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(appointment.IsActive(live, capacityActivation))

	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracleRateSetForRebase(),
	))

	stranded, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	rebased, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	// The window is still open — that is precisely why the window alone is not
	// the answer.
	s.Require().True(stranded.Envelope.IsActive(capacityActivation))
	s.Require().False(stranded.IsActive(rebased, capacityActivation))

	queryServer := keeper.NewQueryServerImpl(s.keeper)
	res, err := queryServer.ConversionMandate(s.ctx, &types.QueryConversionMandateRequest{})
	s.Require().NoError(err)
	s.Require().False(res.Active, "a stranded committee must not be reported as a live fast path")

	// Re-appointing in the new unit — what governance does in the same proposal
	// as the re-point — restores it.
	minimum, maximum := capacityCorridor()
	minimum.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, minimum.BasePool.Amount)
	maximum.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, maximum.BasePool.Amount.MulInt64(4))
	_, err = s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
		Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Committee:        capacityCommittee(),
		ActivationHeight: capacityActivation,
		ExpiryHeight:     capacityExpiry,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
		MaxTobinTax:      math.LegacyZeroDec(),
	})
	s.Require().NoError(err)

	res, err = queryServer.ConversionMandate(s.ctx, &types.QueryConversionMandateRequest{})
	s.Require().NoError(err)
	s.Require().True(res.Active)
}

// TestRebaseBackRevivesAStrandedMandate checks that returning to the corridor's denomination
// restores its usability under the same term and remaining window.
func (s *KeeperTestSuite) TestRebaseBackRevivesAStrandedMandate() {
	appointment := s.seedLiveConversionMandate()
	s.Require().Equal(chain.XDRBaseDenom, appointment.MinimumPolicy.BasePool.Denom)

	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracleRateSetForRebase(),
	))
	stranded, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	usdPool, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().False(stranded.IsActive(usdPool, capacityActivation))

	// Back to the original unit at the inverse rate.
	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.USDBaseDenom,
		chain.XDRBaseDenom,
		oracleRateSetForRebase(),
	))

	revived, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	xdrPool, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	// The appointment was never rewritten, so it is the one governance made.
	s.Require().Equal(appointment, revived)
	s.Require().True(revived.IsActive(xdrPool, capacityActivation))
	s.Require().True(livePool().Equal(xdrPool.BasePool.Amount))

	// Usable is not a claim about the query alone: the committee can act again.
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: revived.Term,
		Policy:       candidateInsideCorridor(),
	})
	s.Require().NoError(err)
}

// TestSetMandateRejectsClosedWindow pins the appointment-time height
// check. A window the chain has already left is a delegation that reads as live
// in state and in the event stream while authorizing nothing, so it fails as a
// proposal rather than at the moment a committee musters behind it.
func (s *KeeperTestSuite) TestSetMandateRejectsClosedWindow() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	minimum, maximum := capacityCorridor()
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(livePool())
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	before, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)

	msg := &types.MsgSetConversionMandate{
		Authority:        authority,
		Committee:        capacityCommittee(),
		ActivationHeight: capacityActivation,
		ExpiryHeight:     capacityExpiry,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
		MaxTobinTax:      math.LegacyZeroDec(),
	}

	// The window is half-open, so the appointment's own expiry is the first
	// height at which it could never act.
	s.setCapacityHeight(capacityExpiry)
	_, err = s.msgServer.SetConversionMandate(s.ctx, msg)
	s.Require().ErrorContains(err, "expires at height 20, which is not above the current height 20")

	s.setCapacityHeight(capacityExpiry + 5)
	_, err = s.msgServer.SetConversionMandate(s.ctx, msg)
	s.Require().ErrorContains(err, "not above the current height 25")

	// A refused appointment leaves the previous one untouched, term included.
	after, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)

	// One height earlier the appointment still has a live block, so it stands:
	// the check rejects a closed window, not a short one.
	s.setCapacityHeight(capacityExpiry - 1)
	_, err = s.msgServer.SetConversionMandate(s.ctx, msg)
	s.Require().NoError(err)
	appointed, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(appointed.IsActive(current, capacityExpiry-1))
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyRescalesDelta() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	tests := []struct {
		name          string
		oldDelta      math.LegacyDec
		expectedDelta math.LegacyDec
	}{
		{name: "positive delta", oldDelta: math.LegacyNewDec(25), expectedDelta: math.LegacyNewDec(50)},
		{name: "negative delta", oldDelta: math.LegacyNewDec(-25), expectedDelta: math.LegacyNewDec(-50)},
		{name: "zero delta", oldDelta: math.LegacyZeroDec(), expectedDelta: math.LegacyZeroDec()},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			current := types.DefaultConversionPolicy()
			current.BasePool = xdrBasePool(math.LegacyNewDec(100))
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, test.oldDelta))

			// The recovery period rides along with the depth change to show both
			// dials are applied as submitted, not just carried over.
			updated := current
			updated.BasePool = xdrBasePool(math.LegacyNewDec(200))
			updated.PoolRecoveryPeriod++
			_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
				Authority: authority,
				Policy:    updated,
			})
			s.Require().NoError(err)

			stored, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(updated, stored)
			storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(test.expectedDelta.Equal(storedDelta))

			s.requirePoolUpdateEvent(current.BasePool, updated.BasePool, test.oldDelta, test.expectedDelta)
		})
	}
}

// TestMsgUpdatePolicyPeriodOnlyChangeLeavesDelta covers the other half of
// the capacity pair: shortening recovery without resizing depth changes how fast
// the gap decays, and the gap itself is already expressed against that depth, so
// there is nothing to rescale.
func (s *KeeperTestSuite) TestMsgUpdatePolicyPeriodOnlyChangeLeavesDelta() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(math.LegacyNewDec(100))
	oldDelta := math.LegacyNewDec(25)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))

	updated := current
	updated.PoolRecoveryPeriod /= 2
	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: authority,
		Policy:    updated,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(updated, stored)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(oldDelta.Equal(storedDelta))
	// The event is emitted unconditionally, so an indexer sees every capacity
	// write rather than only the ones that moved depth.
	s.requirePoolUpdateEvent(current.BasePool, updated.BasePool, oldDelta, oldDelta)
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyRejectsInvalidCandidates() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	tests := []struct {
		name      string
		mutate    func(*types.ConversionPolicy)
		authority string
		expectErr string
		errorIs   error
	}{
		{
			name:      "invalid authority",
			authority: "invalid_authority",
			mutate:    func(*types.ConversionPolicy) {},
			expectErr: "invalid authority",
			errorIs:   errortypes.ErrUnauthorized,
		},
		{
			name:      "zero base pool",
			authority: authority,
			mutate:    func(p *types.ConversionPolicy) { p.BasePool = xdrBasePool(math.LegacyZeroDec()) },
			expectErr: "base pool must be positive",
		},
		{
			name:      "negative base pool",
			authority: authority,
			mutate: func(p *types.ConversionPolicy) {
				p.BasePool = sdk.DecCoin{Denom: chain.XDRBaseDenom, Amount: math.LegacyNewDec(-1)}
			},
			expectErr: "invalid base pool",
		},
		{
			name:      "zero recovery period",
			authority: authority,
			mutate:    func(p *types.ConversionPolicy) { p.PoolRecoveryPeriod = 0 },
			expectErr: "pool recovery period must be between one and",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			before := types.DefaultConversionPolicy()
			before.BasePool = xdrBasePool(math.LegacyNewDec(100))
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, before))
			submitted := before
			tc.mutate(&submitted)

			_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
				Authority: tc.authority,
				Policy:    submitted,
			})
			s.Require().ErrorContains(err, tc.expectErr)
			if tc.errorIs != nil {
				s.Require().ErrorIs(err, tc.errorIs)
			}

			after, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(before, after)
		})
	}
}

// TestMsgUpdatePolicyRejectsBasePoolDenomChange checks that only Oracle reference changes rebase
// the pool. Policy updates cannot independently change its unit.
func (s *KeeperTestSuite) TestMsgUpdatePolicyRejectsBasePoolDenomChange() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	tests := []struct {
		name   string
		mutate func(*types.ConversionPolicy)
	}{
		{
			name: "re-points to another priced denomination",
			mutate: func(p *types.ConversionPolicy) {
				p.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyNewDec(100))
			},
		},
		{
			// Changing the depth in the same message does not buy the
			// re-denomination a rescale path: the unit is refused first.
			name: "re-points while also changing depth",
			mutate: func(p *types.ConversionPolicy) {
				p.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyNewDec(200))
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			current := types.DefaultConversionPolicy()
			current.BasePool = xdrBasePool(math.LegacyNewDec(100))
			oldDelta := math.LegacyNewDec(25)
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))
			submitted := current
			tc.mutate(&submitted)
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			// No rate expectation: the message is refused before any conversion
			// is attempted, because converting the pool is not its job.
			_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
				Authority: authority,
				Policy:    submitted,
			})
			s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
			s.Require().ErrorContains(err, "base pool denom is set by the protocol reference")
			s.Require().ErrorContains(err, "MsgSetReferenceDenom")

			stored, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, stored)
			storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(oldDelta.Equal(storedDelta))
			s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
		})
	}
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyRejectsNonPositiveEffectivePool() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before := types.DefaultConversionPolicy()
	before.BasePool = xdrBasePool(math.LegacyNewDec(100))
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, before))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(-200)))

	submitted := before
	submitted.BasePool = xdrBasePool(math.LegacyNewDec(200))
	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: authority,
		Policy:    submitted,
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "effective ark pool must be positive")

	after, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyRejectsUnrepresentableEffectivePool() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, maxLegacyDecForKeeperTest()))

	s.Require().NotPanics(func() {
		_, err = s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
			Authority: authority,
			Policy:    types.DefaultConversionPolicy(),
		})
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "effective ark pool")

	after, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
}

func (s *KeeperTestSuite) TestMsgUpdatePolicyRescaleOverflowPreservesState() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	before := types.DefaultConversionPolicy()
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, before))
	oldDelta := maxLegacyDecForKeeperTest()
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))

	updated := before
	updated.BasePool.Amount = updated.BasePool.Amount.MulInt64(2)
	_, err := s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: authority,
		Policy:    updated,
	})
	s.Require().ErrorIs(err, types.ErrArithmeticOutOfRange)
	s.Require().ErrorContains(err, "rescaling ark pool delta")

	after, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(oldDelta.Equal(storedDelta))
}

func (s *KeeperTestSuite) TestCommitteeUpdatePolicyAppliesInsideCorridor() {
	appointment := s.seedLiveConversionMandate()
	candidate := candidateInsideCorridor()

	_, err := s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy:       candidate,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(candidate, stored)
	s.requirePoolUpdateEvent(
		xdrBasePool(livePool()),
		candidate.BasePool,
		math.LegacyZeroDec(),
		math.LegacyZeroDec(),
	)
}

func (s *KeeperTestSuite) TestCommitteeUpdatePolicyRejections() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	minimum, maximum := capacityCorridor()

	// The suite seeds a fresh appointment per subtest, and every replacement
	// advances the term, so cases carry a staleness flag rather than a literal
	// term: the live term is whatever the seed just produced.
	tests := []struct {
		name      string
		height    int64
		committee string
		staleTerm bool
		policy    types.ConversionPolicy
		expectErr string
	}{
		{
			name:      "wrong signer",
			height:    capacityActivation,
			committee: sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String(),
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: signer is not the exact appointed committee",
		},
		{
			name:      "governance authority cannot use the committee path",
			height:    capacityActivation,
			committee: authority,
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: signer is not the exact appointed committee",
		},
		{
			name:      "stale term",
			height:    capacityActivation,
			committee: capacityCommittee(),
			staleTerm: true,
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: term mismatch",
		},
		{
			// Term is reported ahead of the window, so a stale transaction names
			// the staleness that produced it rather than whichever window it
			// happens to straddle.
			name:      "stale term outside the window",
			height:    capacityExpiry,
			committee: capacityCommittee(),
			staleTerm: true,
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: term mismatch",
		},
		{
			name:      "before activation",
			height:    capacityActivation - 1,
			committee: capacityCommittee(),
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: mandate is not active",
		},
		{
			name:      "at expiry",
			height:    capacityExpiry,
			committee: capacityCommittee(),
			policy:    candidateInsideCorridor(),
			expectErr: "conversion mandate: mandate is not active",
		},
		{
			name:      "depth above the corridor",
			height:    capacityActivation,
			committee: capacityCommittee(),
			policy: types.ConversionPolicy{
				BasePool:           xdrBasePool(maximum.BasePool.Amount.MulInt64(2)),
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod,
				MinStabilitySpread: minimum.MinStabilitySpread,
			},
			expectErr: "base pool",
		},
		{
			name:      "recovery period below the corridor",
			height:    capacityActivation,
			committee: capacityCommittee(),
			policy: types.ConversionPolicy{
				BasePool:           maximum.BasePool,
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod - 1,
				MinStabilitySpread: minimum.MinStabilitySpread,
			},
			expectErr: "pool recovery period",
		},
		{
			// The corridor is bounded raise-only, so the committee is refused the
			// one direction that would narrow the buffer it exists to widen.
			name:      "stability spread below the corridor",
			height:    capacityActivation,
			committee: capacityCommittee(),
			policy: types.ConversionPolicy{
				BasePool:           maximum.BasePool,
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod,
				MinStabilitySpread: minimum.MinStabilitySpread.QuoInt64(2),
			},
			expectErr: "min stability spread",
		},
		{
			name:      "stability spread above the corridor",
			height:    capacityActivation,
			committee: capacityCommittee(),
			policy: types.ConversionPolicy{
				BasePool:           maximum.BasePool,
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod,
				MinStabilitySpread: maximum.MinStabilitySpread.Add(math.LegacySmallestDec()),
			},
			expectErr: "min stability spread",
		},
		{
			name:      "structurally invalid candidate",
			height:    capacityActivation,
			committee: capacityCommittee(),
			policy: types.ConversionPolicy{
				BasePool:           xdrBasePool(math.LegacyZeroDec()),
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod,
				MinStabilitySpread: minimum.MinStabilitySpread,
			},
			expectErr: "base pool must be positive",
		},
		{
			name:      "malformed committee address",
			height:    capacityActivation,
			committee: "not-an-address",
			policy:    candidateInsideCorridor(),
			expectErr: "committee signer is invalid",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			appointment := s.seedLiveConversionMandate()
			s.setCapacityHeight(tc.height)
			before, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)

			expectedTerm := appointment.Term
			if tc.staleTerm {
				expectedTerm++
			}
			_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
				Committee:    tc.committee,
				ExpectedTerm: expectedTerm,
				Policy:       tc.policy,
			})
			s.Require().ErrorContains(err, tc.expectErr)

			after, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(before, after)
		})
	}
}

// TestGovernanceCapacityPathIgnoresCorridor proves the two entry points are what
// the design says: governance overrides the delegation rather than sharing it, so
// it applies a candidate the committee would be refused.
func (s *KeeperTestSuite) TestGovernanceCapacityPathIgnoresCorridor() {
	appointment := s.seedLiveConversionMandate()
	_, maximum := capacityCorridor()
	beyondCorridor := types.ConversionPolicy{
		BasePool:           xdrBasePool(maximum.BasePool.Amount.MulInt64(10)),
		PoolRecoveryPeriod: maximum.PoolRecoveryPeriod,
		MinStabilitySpread: maximum.MinStabilitySpread,
	}

	_, err := s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy:       beyondCorridor,
	})
	s.Require().ErrorContains(err, "outside mandate range")

	_, err = s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Policy:    beyondCorridor,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(beyondCorridor, stored)
}

// TestRebaseStrandsConversionMandate checks pool conversion preserves its meaning while approved
// corridor units remain fixed. A mismatched corridor cannot authorise policy updates.
func (s *KeeperTestSuite) TestRebaseStrandsConversionMandate() {
	appointment := s.seedLiveConversionMandate()

	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracleRateSetForRebase(),
	))

	rebased, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, rebased.BasePool.Denom)

	// The appointment is byte-identical: a rebase never rewrites a delegation.
	stranded, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(appointment, stranded)

	// A candidate in the new unit falls outside a corridor still denominated in
	// the old one.
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy: types.ConversionPolicy{
			BasePool:           sdk.NewDecCoinFromDec(chain.USDBaseDenom, livePool().MulInt64(2)),
			PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod / 4,
			MinStabilitySpread: types.DefaultMinStabilitySpread,
		},
	})
	s.Require().ErrorContains(err, "base pool denomination ausd is outside the mandate")

	// A candidate in the old unit satisfies the corridor but is refused as a
	// re-denomination, which only MsgSetReferenceDenom may perform.
	_, err = s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy:       candidateInsideCorridor(),
	})
	s.Require().ErrorContains(err, "base pool denom is set by the protocol reference")

	// Governance keeps the slow path throughout, in the new unit.
	_, err = s.msgServer.UpdatePolicy(s.ctx, &types.MsgUpdatePolicy{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Policy: types.ConversionPolicy{
			BasePool:           sdk.NewDecCoinFromDec(chain.USDBaseDenom, livePool()),
			PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod,
			MinStabilitySpread: types.DefaultMinStabilitySpread,
		},
	})
	s.Require().NoError(err)
}

// TestMsgUpdateParamsLeavesCapacityAlone checks that whole-object Params replacement cannot alter
// committee-delegable conversion policy.
func (s *KeeperTestSuite) TestMsgUpdateParamsLeavesCapacityAlone() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	resized := types.DefaultConversionPolicy()
	resized.BasePool = xdrBasePool(math.LegacyNewDec(999))
	resized.PoolRecoveryPeriod = 7
	resized.MinStabilitySpread = math.LegacyNewDecWithPrec(5, 2)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, resized))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(11)))

	params := types.DefaultParams()
	params.DefaultTobinTax = math.LegacyNewDecWithPrec(5, 3)
	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	})
	s.Require().NoError(err)

	storedCapacity, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(resized, storedCapacity)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDec(11).Equal(storedDelta))
}

func tobinCapForTest() math.LegacyDec {
	return math.LegacyNewDecWithPrec(5, 2)
}

// seedLiveTobinMandate appoints the standard corridor with a Tobin cap on top,
// at a height inside the window.
func (s *KeeperTestSuite) seedLiveTobinMandate(tobinCap math.LegacyDec) types.ConversionMandate {
	s.setCapacityHeight(capacityActivation)
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(livePool())
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

	minimum, maximum := capacityCorridor()
	_, err := s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
		Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Committee:        capacityCommittee(),
		ActivationHeight: capacityActivation,
		ExpiryHeight:     capacityExpiry,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
		MaxTobinTax:      tobinCap,
	})
	s.Require().NoError(err)

	appointment, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)

	return appointment
}

// TestCommitteeSetTobinTaxAppliesAndUnwinds walks the whole band: up during an
// incident, further up, and all the way back to the default once it passes. The
// return leg is the point — the committee retires its own emergency rate
// without a proposal, and only the trip below the default needs governance.
func (s *KeeperTestSuite) TestCommitteeSetTobinTaxAppliesAndUnwinds() {
	appointment := s.seedLiveTobinMandate(tobinCapForTest())

	set := func(rate math.LegacyDec) error {
		_, err := s.msgServer.CommitteeSetTobinTax(s.ctx, &types.MsgCommitteeSetTobinTax{
			Committee:    capacityCommittee(),
			ExpectedTerm: appointment.Term,
			Denom:        chain.USDBaseDenom,
			TobinTax:     rate,
		})

		return err
	}
	requireStored := func(expected math.LegacyDec) {
		stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, chain.USDBaseDenom)
		s.Require().NoError(err)
		s.Require().Equal(expected, stored)
	}

	raise := math.LegacyNewDecWithPrec(2, 2)
	s.Require().NoError(set(raise))
	requireStored(raise)

	higher := math.LegacyNewDecWithPrec(3, 2)
	s.Require().NoError(set(higher))
	requireStored(higher)

	// Stepping back down is authorised now: the floor is the chain-wide
	// default, not wherever the committee last left the rate.
	s.Require().NoError(set(raise))
	requireStored(raise)

	s.Require().NoError(set(types.DefaultTobinTax))
	requireStored(types.DefaultTobinTax)

	// Below the default stays governance's alone.
	s.Require().ErrorContains(
		set(types.DefaultTobinTax.QuoInt64(2)),
		"below the default rate",
	)
	requireStored(types.DefaultTobinTax)
}

// TestCommitteeSetTobinTaxReachesGovernanceOverride checks committee authority over the shared
// override map within its governance-set band, including lowering a governance-written override.
func (s *KeeperTestSuite) TestCommitteeSetTobinTaxReachesGovernanceOverride() {
	appointment := s.seedLiveTobinMandate(tobinCapForTest())

	governanceRate := math.LegacyNewDecWithPrec(4, 2)
	s.Require().NoError(s.keeper.TobinTaxOverrides.Set(
		s.ctx,
		chain.USDBaseDenom,
		governanceRate,
	))

	_, err := s.msgServer.CommitteeSetTobinTax(s.ctx, &types.MsgCommitteeSetTobinTax{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Denom:        chain.USDBaseDenom,
		TobinTax:     types.DefaultTobinTax,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultTobinTax, stored)
}

func (s *KeeperTestSuite) TestCommitteeSetTobinTaxRejections() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	tests := []struct {
		name string
		// tobinCap seeds the appointment; nil skips seeding entirely so the
		// suite's default disabled mandate stays live.
		tobinCap math.LegacyDec
		// setup runs after the seed, before the message.
		setup       func()
		msg         func(term uint64) *types.MsgCommitteeSetTobinTax
		expectErr   string
		expectErrIs error
	}{
		{
			name:     "capacity-only mandate",
			tobinCap: math.LegacyZeroDec(),
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErr: "conversion mandate delegates no Tobin power",
		},
		{
			name: "disabled mandate",
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErr: "signer is not the exact appointed committee",
		},
		{
			name:     "wrong signer",
			tobinCap: tobinCapForTest(),
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    authority,
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErr: "signer is not the exact appointed committee",
		},
		{
			name:     "stale term",
			tobinCap: tobinCapForTest(),
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term + 1,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErr: "term mismatch",
		},
		{
			name:     "outside the window",
			tobinCap: tobinCapForTest(),
			setup:    func() { s.setCapacityHeight(capacityExpiry) },
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErr: "mandate is not active",
		},
		{
			// The band's floor is the chain-wide default, so a candidate under
			// it is refused however high the denomination currently sits.
			name:     "below the default rate",
			tobinCap: tobinCapForTest(),
			setup: func() {
				s.Require().NoError(s.keeper.SetTobinTaxOverride(
					s.ctx,
					chain.USDBaseDenom,
					math.LegacyNewDecWithPrec(3, 2),
				))
			},
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     types.DefaultTobinTax.QuoInt64(2),
				}
			},
			expectErr: "below the default rate",
		},
		{
			name:     "above the cap",
			tobinCap: tobinCapForTest(),
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.USDBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(6, 2),
				}
			},
			expectErr: "exceeds the mandate cap",
		},
		{
			// The registry check is the same one the governance setter runs: a
			// committee typo must fail as loudly as a proposal typo.
			name:     "unregistered denomination",
			tobinCap: tobinCapForTest(),
			setup: func() {
				s.assetStatuses = map[string]assettypes.AssetStatus{
					chain.EURBaseDenom: assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED,
				}
			},
			msg: func(term uint64) *types.MsgCommitteeSetTobinTax {
				return &types.MsgCommitteeSetTobinTax{
					Committee:    capacityCommittee(),
					ExpectedTerm: term,
					Denom:        chain.EURBaseDenom,
					TobinTax:     math.LegacyNewDecWithPrec(2, 2),
				}
			},
			expectErrIs: assettypes.ErrAssetNotFound,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			var term uint64
			if tc.tobinCap.IsNil() {
				// Replace whatever the previous subtest appointed with the
				// canonical disabled mandate.
				_, err := s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{Authority: authority})
				s.Require().NoError(err)
				current, err := s.keeper.ConversionMandate.Get(s.ctx)
				s.Require().NoError(err)
				term = current.Term
			} else {
				term = s.seedLiveTobinMandate(tc.tobinCap).Term
			}
			s.assetStatuses = map[string]assettypes.AssetStatus{}
			if tc.setup != nil {
				tc.setup()
			}

			_, err := s.msgServer.CommitteeSetTobinTax(s.ctx, tc.msg(term))
			s.Require().Error(err)
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
			}
			if tc.expectErrIs != nil {
				s.Require().ErrorIs(err, tc.expectErrIs)
			}
		})
	}
}

// TestGovernanceRestoresCommitteeRate pins what stays governance's alone once
// the committee can move within its band: taking a denomination below the
// default, removing an override outright so it tracks the default again, and
// the fact that a mandate replacement never rewrites an applied rate.
func (s *KeeperTestSuite) TestGovernanceRestoresCommitteeRate() {
	appointment := s.seedLiveTobinMandate(tobinCapForTest())

	raise := math.LegacyNewDecWithPrec(2, 2)
	_, err := s.msgServer.CommitteeSetTobinTax(s.ctx, &types.MsgCommitteeSetTobinTax{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Denom:        chain.GBPBaseDenom,
		TobinTax:     raise,
	})
	s.Require().NoError(err)

	// Governance goes below the default, which the band forbids the committee:
	// its path reads no mandate at all.
	lowered := math.LegacyNewDecWithPrec(1, 3)
	s.Require().NoError(s.keeper.SetTobinTaxOverride(s.ctx, chain.GBPBaseDenom, lowered))
	stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, chain.GBPBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(lowered, stored)

	// And removes outright.
	s.Require().NoError(s.keeper.RemoveTobinTaxOverride(s.ctx, chain.GBPBaseDenom))

	// Replacing the mandate never rewrites an applied raise: the override an
	// outgoing committee set keeps protecting its market until governance
	// acts, exactly like an applied capacity policy.
	_, err = s.msgServer.CommitteeSetTobinTax(s.ctx, &types.MsgCommitteeSetTobinTax{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Denom:        chain.GBPBaseDenom,
		TobinTax:     raise,
	})
	s.Require().NoError(err)
	_, err = s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	})
	s.Require().NoError(err)
	stored, err = s.keeper.TobinTaxOverrides.Get(s.ctx, chain.GBPBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(raise, stored)
}

// TestStrandedCorridorLeavesTobinPowerUsable pins the design line the mandate
// comment draws: the cap is a dimensionless rate, so the re-point that strands
// the depth corridor has no claim on the Tobin power.
func (s *KeeperTestSuite) TestStrandedCorridorLeavesTobinPowerUsable() {
	appointment := s.seedLiveTobinMandate(tobinCapForTest())

	s.Require().NoError(s.keeper.RebaseBasePool(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracleRateSetForRebase(),
	))

	// The corridor is stranded in the old unit...
	_, err := s.msgServer.CommitteeUpdatePolicy(s.ctx, &types.MsgCommitteeUpdatePolicy{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Policy:       candidateInsideCorridor(),
	})
	s.Require().ErrorContains(err, "base pool denom is set by the protocol reference")

	// ...and the Tobin power is not.
	raise := math.LegacyNewDecWithPrec(2, 2)
	_, err = s.msgServer.CommitteeSetTobinTax(s.ctx, &types.MsgCommitteeSetTobinTax{
		Committee:    capacityCommittee(),
		ExpectedTerm: appointment.Term,
		Denom:        chain.KRWBaseDenom,
		TobinTax:     raise,
	})
	s.Require().NoError(err)

	stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(raise, stored)
}

// TestSetConversionMandateRejectsEmptyTobinBand pins the appointment-time guard
// on the band: a cap under the chain-wide default delegates a power that
// refuses every candidate, which is the same stillborn appointment as a
// corridor in the wrong unit. Delegating nothing is spelled with a zero cap.
func (s *KeeperTestSuite) TestSetConversionMandateRejectsEmptyTobinBand() {
	s.setCapacityHeight(capacityActivation)
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(livePool())
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

	minimum, maximum := capacityCorridor()
	appoint := func(cap math.LegacyDec) error {
		_, err := s.msgServer.SetConversionMandate(s.ctx, &types.MsgSetConversionMandate{
			Authority:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Committee:        capacityCommittee(),
			ActivationHeight: capacityActivation,
			ExpiryHeight:     capacityExpiry,
			MinimumPolicy:    minimum,
			MaximumPolicy:    maximum,
			MaxTobinTax:      cap,
		})

		return err
	}

	s.Require().ErrorContains(
		appoint(types.DefaultTobinTax.QuoInt64(2)),
		"delegates an empty band",
	)

	// The default itself is a one-point band, which authorises exactly the
	// baseline rate, so it is a real delegation rather than a stillborn one.
	s.Require().NoError(appoint(types.DefaultTobinTax))

	// And a zero cap stays the canonical capacity-only appointment.
	s.Require().NoError(appoint(math.LegacyZeroDec()))
	appointment, err := s.keeper.ConversionMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(appointment.MaxTobinTax.IsZero())
}
