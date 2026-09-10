package simulation

import (
	"context"
	"slices"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/types"
)

// pickAsset returns a registered asset in one of the given statuses, skipping
// the operation when none exists. Skipping is the norm rather than a fault: a
// status is only occupied once an earlier transition has put an asset there,
// so the lifecycle opens to simulation one step at a time.
func pickAsset(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
	statuses ...types.AssetStatus,
) (types.Asset, bool) {
	assets, err := k.ListAssets(ctx)
	if err != nil {
		reporter.Skip("list assets: " + err.Error())

		return types.Asset{}, false
	}

	candidates := make([]types.Asset, 0, len(assets))
	for _, asset := range assets {
		if slices.Contains(statuses, asset.Status) {
			candidates = append(candidates, asset)
		}
	}
	if len(candidates) == 0 {
		reporter.Skip("no asset in a status this transition accepts")

		return types.Asset{}, false
	}

	return candidates[testData.Rand().IntInRange(0, len(candidates))], true
}

// MsgUpdateParamsFactory creates a governance proposal for a valid Asset
// parameter update.
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    RandomisedParams(testData.Rand().Rand),
		}
	}
}

// MsgHaltIssuanceFactory halts issuance on an active asset.
func MsgHaltIssuanceFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgHaltIssuance] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgHaltIssuance) {
		asset, ok := pickAsset(ctx, k, testData, reporter, types.AssetStatus_ASSET_STATUS_ACTIVE)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgHaltIssuance{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgResumeIssuanceFactory returns a halted asset to active issuance.
func MsgResumeIssuanceFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgResumeIssuance] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgResumeIssuance) {
		asset, ok := pickAsset(ctx, k, testData, reporter, types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgResumeIssuance{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgSuspendAssetFactory suspends an asset that is still issuing or halted.
func MsgSuspendAssetFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSuspendAsset] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSuspendAsset) {
		asset, ok := pickAsset(
			ctx, k, testData, reporter,
			types.AssetStatus_ASSET_STATUS_ACTIVE,
			types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
		)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgSuspendAsset{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgWriteOffAssetFactory derecognises a suspended asset.
func MsgWriteOffAssetFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgWriteOffAsset] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgWriteOffAsset) {
		asset, ok := pickAsset(ctx, k, testData, reporter, types.AssetStatus_ASSET_STATUS_SUSPENDED)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgWriteOffAsset{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgRecoverAssetFactory restores a suspended or written-off asset to halted
// issuance, which is where recovery lands rather than straight back to active.
func MsgRecoverAssetFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgRecoverAsset] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgRecoverAsset) {
		asset, ok := pickAsset(
			ctx, k, testData, reporter,
			types.AssetStatus_ASSET_STATUS_SUSPENDED,
			types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgRecoverAsset{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgCancelSettlementFactory withdraws a settlement plan before it activates.
// Only a suspended asset carrying a plan qualifies: cancelling is the reversal
// of opening, and a written-off asset has already left that window.
func MsgCancelSettlementFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCancelSettlement] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCancelSettlement) {
		assets, err := k.ListAssets(ctx)
		if err != nil {
			reporter.Skip("list assets: " + err.Error())

			return nil, nil
		}

		candidates := make([]types.Asset, 0, len(assets))
		for _, asset := range assets {
			if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED {
				continue
			}
			_, found, err := k.GetSettlementPlan(ctx, asset.Denom)
			if err != nil {
				reporter.Skip("get settlement plan: " + err.Error())

				return nil, nil
			}
			if found {
				candidates = append(candidates, asset)
			}
		}
		if len(candidates) == 0 {
			reporter.Skip("no suspended asset carries a settlement plan")

			return nil, nil
		}

		asset := candidates[testData.Rand().IntInRange(0, len(candidates))]

		return nil, &types.MsgCancelSettlement{
			Authority:       testData.ModuleAccountAddress(reporter, "gov"),
			Denom:           asset.Denom,
			ExpectedVersion: asset.Version,
		}
	}
}

// MsgSetEmergencyMandateFactory appoints the emergency committee. The
// appointee is drawn from the simulation's own accounts, so the committee is
// an address the run holds a key for — which is what lets the committee
// surface be exercised later rather than merely stored.
func MsgSetEmergencyMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetEmergencyMandate] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetEmergencyMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the Asset authority")

			return nil, nil
		}

		r := testData.Rand()
		activationHeight := r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetEmergencyMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activationHeight,
			// Validation demands activation strictly precede expiry.
			ExpiryHeight: activationHeight + r.Uint64InRange(1, 100_000),
		}
	}
}

// MsgOpenSettlementFactory selects suspended or written-off assets with supply and no plan. Rates
// stay in (0, 1]; closing is far enough beyond the draw to follow activation at delayed proposal
// execution.
func MsgOpenSettlementFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgOpenSettlement] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgOpenSettlement) {
		assets, err := k.ListAssets(ctx)
		if err != nil {
			reporter.Skip("list assets: " + err.Error())

			return nil, nil
		}

		candidates := make([]types.Asset, 0, len(assets))
		for _, asset := range assets {
			if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
				asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
				continue
			}
			if !k.AssetSupply(ctx, asset.Denom).IsPositive() {
				continue
			}
			_, found, err := k.GetSettlementPlan(ctx, asset.Denom)
			if err != nil {
				reporter.Skip("get settlement plan: " + err.Error())

				return nil, nil
			}
			if found {
				continue
			}
			candidates = append(candidates, asset)
		}
		if len(candidates) == 0 {
			reporter.Skip("no asset may open settlement")

			return nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get asset params: " + err.Error())

			return nil, nil
		}

		r := testData.Rand()
		asset := candidates[r.IntInRange(0, len(candidates))]
		activation := sdk.UnwrapSDKContext(ctx).BlockHeight() + int64(params.SettlementActivationDelayBlocks)

		return nil, &types.MsgOpenSettlement{
			Authority:             testData.ModuleAccountAddress(reporter, "gov"),
			Denom:                 asset.Denom,
			ExpectedVersion:       asset.Version,
			RedemptionRate:        math.LegacyNewDecWithPrec(int64(r.Uint64InRange(1, 1_000_000)), 6),
			EarliestClosingHeight: activation + int64(r.IntInRange(1_000, 10_000)),
		}
	}
}

// retirement is an asset that may finalise retirement with the smallest
// residual bound the handler accepts for its status.
type retirement struct {
	asset types.Asset
	bound math.Int
}

// MsgFinaliseRetirementFactory selects eligible assets: halted with a covering residual bound,
// suspended with zero supply, or written-off with zero bound. Halted cases also exercise bounds
// above the minimum.
func MsgFinaliseRetirementFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgFinaliseRetirement] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgFinaliseRetirement) {
		assets, err := k.ListAssets(ctx)
		if err != nil {
			reporter.Skip("list assets: " + err.Error())

			return nil, nil
		}

		candidates := make([]retirement, 0, len(assets))
		for _, asset := range assets {
			supply := k.AssetSupply(ctx, asset.Denom)
			var bound math.Int
			switch asset.Status {
			case types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED:
				bound = supply.Amount
			case types.AssetStatus_ASSET_STATUS_SUSPENDED:
				if !supply.IsZero() {
					continue
				}
				bound = math.ZeroInt()
			case types.AssetStatus_ASSET_STATUS_WRITTEN_OFF:
				bound = math.ZeroInt()
			default:
				continue
			}
			candidates = append(candidates, retirement{asset: asset, bound: bound})
		}
		if len(candidates) == 0 {
			reporter.Skip("no asset may finalise retirement")

			return nil, nil
		}

		r := testData.Rand()
		candidate := candidates[r.IntInRange(0, len(candidates))]
		bound := candidate.bound
		if candidate.asset.Status == types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED && r.Bool() {
			bound = bound.Add(math.NewInt(int64(r.IntInRange(1, 1_000))))
		}

		return nil, &types.MsgFinaliseRetirement{
			Authority:         testData.ModuleAccountAddress(reporter, "gov"),
			Denom:             candidate.asset.Denom,
			ExpectedVersion:   candidate.asset.Version,
			MaxResidualSupply: bound,
		}
	}
}
