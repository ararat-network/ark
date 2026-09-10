package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/x/market/simulation"
	"github.com/ararat-network/ark/x/market/types"
)

// The conversion committee signs its messages rather than proposing them, so
// a run reaches this surface only when genesis appointed an address the run
// holds and the window is open. Both failures read as skips, which is why the
// corridor is exercised from a state where the factory must produce something.

// appointCommittee installs the appointment the sim's own genesis generator
// draws over the run's accounts, widening the corridor around the standing
// policy the way a run's genesis does.
func (f marketFixture) appointCommittee(t *testing.T) types.ConversionMandate {
	t.Helper()

	addresses := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		addresses = append(addresses, account.Address.String())
	}
	policy, err := f.keeper.ConversionPolicy.Get(f.ctx)
	require.NoError(t, err)
	mandate := simulation.GenConversionMandate(f.rand, addresses, policy)
	require.NoError(t, f.keeper.ConversionMandate.Set(f.ctx, mandate))

	return mandate
}

// TestMsgCommitteeUpdatePolicyFactoryStaysInsideTheCorridor checks that generated committee
// policies are reachable and accepted within mandate bounds.
func TestMsgCommitteeUpdatePolicyFactoryStaysInsideTheCorridor(t *testing.T) {
	for seed := range int64(factorySeeds) {
		f := newMarketFixture(t, seed+1)
		mandate := f.appointCommittee(t)

		signers, msg := simulation.MsgCommitteeUpdatePolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Len(t, signers, 1)
		require.Equal(t, mandate.Committee, signers[0].Address.String(), "the appointed committee must sign")
		require.Equal(t, mandate.Term, msg.ExpectedTerm)
		require.True(t, msg.Policy.BasePool.Amount.GTE(mandate.MinimumPolicy.BasePool.Amount))
		require.True(t, msg.Policy.BasePool.Amount.LTE(mandate.MaximumPolicy.BasePool.Amount))
		require.NoError(t, msg.Policy.Validate())

		_, err := f.msgServer.CommitteeUpdatePolicy(f.ctx, msg)
		require.NoError(t, err)
	}
}

// TestMsgCommitteeSetTobinTaxFactoryRaisesWithinTheCap covers the other
// delegated power. The committee may only raise, so the draw starts at the
// parameter default and stops at the mandate's cap.
func TestMsgCommitteeSetTobinTaxFactoryRaisesWithinTheCap(t *testing.T) {
	for seed := range int64(factorySeeds) {
		f := newMarketFixture(t, seed+1)
		mandate := f.appointCommittee(t)
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)

		signers, msg := simulation.MsgCommitteeSetTobinTaxFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Len(t, signers, 1)
		require.Equal(t, mandate.Committee, signers[0].Address.String())
		require.Equal(t, simDenom, msg.Denom, "only a registered denomination may be priced")
		require.True(t, msg.TobinTax.GTE(params.DefaultTobinTax), "the committee may only raise")
		require.True(t, msg.TobinTax.LTE(mandate.MaxTobinTax))

		_, err = f.msgServer.CommitteeSetTobinTax(f.ctx, msg)
		require.NoError(t, err)
	}
}

func TestCommitteeFactoriesSkipWithoutAnActiveMandate(t *testing.T) {
	tests := map[string]func(f marketFixture) any{
		"update policy": func(f marketFixture) any {
			_, msg := simulation.MsgCommitteeUpdatePolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"set tobin tax": func(f marketFixture) any {
			_, msg := simulation.MsgCommitteeSetTobinTaxFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
	}

	for name, emit := range tests {
		t.Run(name, func(t *testing.T) {
			f := newMarketFixture(t, 1)
			require.NoError(t, f.keeper.ConversionMandate.Set(f.ctx, types.DefaultConversionMandate()))

			msg := emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), "not active")
			require.Nil(t, msg)
		})
	}
}

// TestMsgCommitteeSetTobinTaxFactorySkipsWithoutADelegatedCap pins the guard
// that keeps the factory silent under an appointment granting no Tobin power,
// which is what the shipped mandate corridor grants.
func TestMsgCommitteeSetTobinTaxFactorySkipsWithoutADelegatedCap(t *testing.T) {
	f := newMarketFixture(t, 1)
	mandate := f.appointCommittee(t)
	mandate.MaxTobinTax = mandate.MaxTobinTax.MulInt64(0)
	require.NoError(t, f.keeper.ConversionMandate.Set(f.ctx, mandate))

	_, msg := simulation.MsgCommitteeSetTobinTaxFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "delegates no Tobin power")
	require.Nil(t, msg)
}

// With nothing registered there is no denomination to price, which is the
// state a chain is in before the first oracle window closes.
func TestMsgCommitteeSetTobinTaxFactorySkipsWithoutARegisteredDenom(t *testing.T) {
	f := newMarketFixture(t, 1, withPricedDenoms())
	f.appointCommittee(t)

	_, msg := simulation.MsgCommitteeSetTobinTaxFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no registered denomination to price")
	require.Nil(t, msg)
}
