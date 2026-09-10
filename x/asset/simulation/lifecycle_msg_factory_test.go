package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/x/asset/simulation"
	"github.com/ararat-network/ark/x/asset/types"
)

// Each lifecycle factory must emit messages from an accepted status and skip ineligible states.
// Explicit fixtures distinguish unreachable factory output from legitimate simulation skips.

func govAuthority() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

// TestPickAssetSelectsOnlyTheAcceptedStatuses covers the shared selector the
// lifecycle factories are built on. It is the single point where a status
// filter could silently exclude every asset.
func TestPickAssetSelectsOnlyTheAcceptedStatuses(t *testing.T) {
	t.Run("skips when no asset occupies an accepted status", func(t *testing.T) {
		f := newAssetFixture(t)
		f.seed(t, 0, types.AssetStatus_ASSET_STATUS_ACTIVE, 5)

		_, msg := simulation.MsgResumeIssuanceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no asset in a status this transition accepts")
		require.Nil(t, msg)
	})

	t.Run("selects the asset that does", func(t *testing.T) {
		f := newAssetFixture(t)
		f.seed(t, 0, types.AssetStatus_ASSET_STATUS_ACTIVE, 5)
		halted := f.seed(t, 1, types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, 5)

		_, msg := simulation.MsgResumeIssuanceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, halted.Denom, msg.Denom)
	})
}

// TestLifecycleFactoriesEmitAcceptedTransitions delivers each factory's message through its keeper
// path, checking status eligibility and the current expected version.
func TestLifecycleFactoriesEmitAcceptedTransitions(t *testing.T) {
	tests := map[string]struct {
		from   types.AssetStatus
		supply int64
		emit   func(f assetFixture) (string, uint64, string)
		apply  func(f assetFixture, denom string, version uint64) error
		to     types.AssetStatus
	}{
		"halt issuance": {
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgHaltIssuanceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.HaltIssuance(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
		"resume issuance": {
			from: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgResumeIssuanceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.ResumeIssuance(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_ACTIVE,
		},
		"suspend an active asset": {
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgSuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.SuspendAsset(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
		"suspend a halted asset": {
			from: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgSuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.SuspendAsset(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_SUSPENDED,
		},
		"write off a suspended asset": {
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			// A write-off is a derecognition of outstanding supply, so an
			// asset with none has nothing to derecognise.
			supply: 4,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgWriteOffAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.WriteOffAsset(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		},
		// Recovery lands on halted issuance rather than straight back to
		// active, from either status it accepts.
		"recover a suspended asset": {
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgRecoverAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.RecoverAsset(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
		"recover a written-off asset": {
			from: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			emit: func(f assetFixture) (string, uint64, string) {
				_, msg := simulation.MsgRecoverAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg.Denom, msg.ExpectedVersion, msg.Authority
			},
			apply: func(f assetFixture, denom string, version uint64) error {
				return f.keeper.RecoverAsset(f.ctx, denom, version)
			},
			to: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newAssetFixture(t)
			asset := f.seed(t, 0, tc.from, tc.supply)

			denom, version, authority := tc.emit(f)

			require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
			require.Equal(t, govAuthority(), authority)
			require.Equal(t, asset.Denom, denom)
			require.Equal(t, asset.Version, version)
			require.NoError(t, tc.apply(f, denom, version))

			moved, err := f.keeper.GetAsset(f.ctx, denom)
			require.NoError(t, err)
			require.Equal(t, tc.to, moved.Status)
		})
	}
}

// TestLifecycleFactoriesSkipFromAStatusTheyRefuse is the negative half: a
// factory reached before the lifecycle has opened its status must decline
// rather than emit a transition the handler would refuse.
func TestLifecycleFactoriesSkipFromAStatusTheyRefuse(t *testing.T) {
	tests := map[string]struct {
		from types.AssetStatus
		emit func(f assetFixture) any
	}{
		"halt issuance from suspended": {
			from: types.AssetStatus_ASSET_STATUS_SUSPENDED,
			emit: func(f assetFixture) any {
				_, msg := simulation.MsgHaltIssuanceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
		},
		"suspend from written off": {
			from: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			emit: func(f assetFixture) any {
				_, msg := simulation.MsgSuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
		},
		"write off from active": {
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			emit: func(f assetFixture) any {
				_, msg := simulation.MsgWriteOffAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
		},
		"recover from active": {
			from: types.AssetStatus_ASSET_STATUS_ACTIVE,
			emit: func(f assetFixture) any {
				_, msg := simulation.MsgRecoverAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newAssetFixture(t)
			f.seed(t, 0, tc.from, 5)

			msg := tc.emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), "no asset in a status this transition accepts")
			require.Nil(t, msg)
		})
	}
}

// TestMsgCancelSettlementFactory pins the one lifecycle factory whose
// precondition is a plan rather than a status: cancelling reverses opening, so
// a suspended asset without a standing plan has nothing to withdraw.
func TestMsgCancelSettlementFactory(t *testing.T) {
	t.Run("skips a suspended asset carrying no plan", func(t *testing.T) {
		f := newAssetFixture(t)
		f.seed(t, 0, types.AssetStatus_ASSET_STATUS_SUSPENDED, 3)

		_, msg := simulation.MsgCancelSettlementFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no suspended asset carries a settlement plan")
		require.Nil(t, msg)
	})

	t.Run("withdraws a standing plan", func(t *testing.T) {
		f := newAssetFixture(t)
		asset := f.seed(t, 0, types.AssetStatus_ASSET_STATUS_SUSPENDED, 3)
		_, opened := simulation.MsgOpenSettlementFactory(f.keeper)(f.ctx, f.testData, f.reporter)
		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.NoError(t, f.keeper.OpenSettlement(
			f.ctx, opened.Denom, opened.ExpectedVersion, opened.RedemptionRate, opened.EarliestClosingHeight,
		))

		_, msg := simulation.MsgCancelSettlementFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, govAuthority(), msg.Authority)
		require.Equal(t, asset.Denom, msg.Denom)
		require.NoError(t, f.keeper.CancelSettlement(f.ctx, msg.Denom, msg.ExpectedVersion))

		_, found, err := f.keeper.GetSettlementPlan(f.ctx, msg.Denom)
		require.NoError(t, err)
		require.False(t, found)
	})
}

// TestMsgUpdateParamsFactory holds the params proposal to the domain its own
// validation enforces. The activation delay is the module's only parameter and
// validation refuses zero, so a generator starting at zero would make every
// draw a refused proposal.
func TestMsgUpdateParamsFactory(t *testing.T) {
	f := newAssetFixture(t)

	_, msg := simulation.MsgUpdateParamsFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, govAuthority(), msg.Authority)
	require.NoError(t, msg.Params.Validate())
	require.NoError(t, f.keeper.Params.Set(f.ctx, msg.Params))
}
