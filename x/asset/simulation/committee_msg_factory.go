package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/types"
)

// MsgEmergencySuspendAssetFactory selects a suspendable asset under the live committee mandate,
// excluding denominations whose one suspension for the current term is spent.
func MsgEmergencySuspendAssetFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgEmergencySuspendAsset] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgEmergencySuspendAsset) {
		mandate, err := k.EmergencyMandate.Get(ctx)
		if err != nil {
			reporter.Skip("get emergency mandate: " + err.Error())

			return nil, nil
		}
		if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
			reporter.Skip("emergency mandate is not active at this height")

			return nil, nil
		}
		committee := testData.GetAccount(reporter, mandate.Committee)
		if reporter.IsSkipped() {
			return nil, nil
		}

		assets, err := k.ListAssets(ctx)
		if err != nil {
			reporter.Skip("list assets: " + err.Error())

			return nil, nil
		}

		candidates := make([]types.Asset, 0, len(assets))
		for _, asset := range assets {
			if asset.Status != types.AssetStatus_ASSET_STATUS_ACTIVE &&
				asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED {
				continue
			}
			used, err := k.EmergencySuspensions.Has(ctx, asset.Denom)
			if err != nil {
				reporter.Skip("check emergency suspension: " + err.Error())

				return nil, nil
			}
			if !used {
				candidates = append(candidates, asset)
			}
		}
		if len(candidates) == 0 {
			reporter.Skip("no asset this term may still suspend")

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgEmergencySuspendAsset{
			Committee:    mandate.Committee,
			Denom:        candidates[testData.Rand().IntInRange(0, len(candidates))].Denom,
			ExpectedTerm: mandate.Term,
		}
	}
}
