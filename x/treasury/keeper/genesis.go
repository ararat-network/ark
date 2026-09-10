package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

// InitGenesis validates and imports Treasury policy state. Fund custody is
// imported by Bank and is inspected here, never duplicated or minted.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("treasury genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid Treasury genesis state: %w", err)
	}
	// The InitGenesis context carries no consensus params, so these can only
	// compare against the fallback authority; the message path re-checks every
	// later replacement against the effective authority.
	if data.EconomicMandate.Committee == k.authority {
		return fmt.Errorf("economic-policy committee must be distinct from Treasury authority")
	}

	// ReferenceTaxCap must use the protocol reference denomination. Oracle imports first, making
	// the configured reference available for this check.
	referenceDenom, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return fmt.Errorf("getting protocol reference: %w", err)
	}
	if referenceDenom == "" {
		return fmt.Errorf(
			"treasury reference denom %s requires a configured protocol reference",
			data.Params.ReferenceDenom,
		)
	}
	if data.Params.ReferenceDenom != referenceDenom {
		return fmt.Errorf(
			"treasury reference denom %s must be the protocol reference %s",
			data.Params.ReferenceDenom,
			referenceDenom,
		)
	}

	factors := append([]types.ConversionFactor(nil), data.ConversionFactors...)
	// Validate guarantees the NOAH cross is present, so "no factors" means no
	// member factors: the cross prices gas and is not a member.
	memberFactors := 0
	for _, factor := range factors {
		if factor.Denom != chain.NoahBaseDenom {
			memberFactors++
		}
	}
	if memberFactors == 0 {
		denoms, err := k.assetKeeper.OraclePricedDenoms(ctx)
		if err != nil {
			return fmt.Errorf("getting oracle-priced denominations: %w", err)
		}
		// Seed member factors at one without reading Oracle rates, matching uncovered runtime
		// arrivals. The next refresh derives usable crosses from live observations.
		for _, denom := range denoms {
			factors = append(factors, types.ConversionFactor{
				Denom:  denom,
				Factor: math.LegacyOneDec(),
			})
		}
	}
	// Supplied factors may omit members awaiting refresh and need not match live rates. Every
	// non-NOAH factor must name a permanent registry member, keeping taxation within settleable
	// assets and preserving export/import validity.
	for _, factor := range factors {
		// The numeraire's entry is fee-pricing state, excluded from the tax
		// base in GetTaxCap, and deliberately never a registry member.
		if factor.Denom == chain.NoahBaseDenom {
			continue
		}
		member, err := k.assetKeeper.HasAsset(ctx, factor.Denom)
		if err != nil {
			return fmt.Errorf("checking the asset registry for %s: %w", factor.Denom, err)
		}
		if !member {
			return fmt.Errorf(
				"conversion factor denom %s is not an Ark-issued asset: a factor is what makes a "+
					"denomination taxable, and tax the chain cannot price never leaves the collector",
				factor.Denom,
			)
		}
	}

	for _, moduleName := range types.FundAccountNames() {
		moduleAccount := k.accountKeeper.GetModuleAccount(ctx, moduleName)
		if moduleAccount == nil {
			return fmt.Errorf("%s module account has not been set", moduleName)
		}
		balances := k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress())
		for _, balance := range balances {
			if balance.Denom != chain.NoahBaseDenom {
				return fmt.Errorf(
					"%s fund account contains unsupported genesis denom %s",
					moduleName,
					balance.Denom,
				)
			}
		}
	}

	// The blocked tax collector may hold registered member tax across windows, so genesis permits
	// that residue. Validate deposits here because Bank genesis bypasses runtime send restrictions.
	collector := k.accountKeeper.GetModuleAccount(ctx, types.TransferTaxCollectorName)
	if collector == nil {
		return fmt.Errorf("%s module account has not been set", types.TransferTaxCollectorName)
	}
	for _, balance := range k.bankKeeper.GetAllBalances(ctx, collector.GetAddress()) {
		if balance.Denom == chain.NoahBaseDenom {
			continue
		}
		member, err := k.assetKeeper.HasAsset(ctx, balance.Denom)
		if err != nil {
			return fmt.Errorf("checking the asset registry for %s: %w", balance.Denom, err)
		}
		if !member {
			return fmt.Errorf(
				"%s account contains unsupported genesis denom %s: collected tax must be %s "+
					"or an Ark-issued asset",
				types.TransferTaxCollectorName,
				balance.Denom,
				chain.NoahBaseDenom,
			)
		}
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.EconomicPolicy.Set(ctx, data.EconomicPolicy); err != nil {
		return fmt.Errorf("setting economic policy: %w", err)
	}
	for _, factor := range factors {
		if err := k.ConversionFactors.Set(ctx, factor.Denom, factor); err != nil {
			return fmt.Errorf("setting conversion factor %s: %w", factor.Denom, err)
		}
	}
	if err := k.RewardFunding.Set(ctx, data.RewardFunding); err != nil {
		return fmt.Errorf("setting reward funding state: %w", err)
	}
	if err := k.EconomicMandate.Set(ctx, data.EconomicMandate); err != nil {
		return fmt.Errorf("setting economic mandate: %w", err)
	}
	if err := k.ExposureState.Set(ctx, data.ExposureState); err != nil {
		return fmt.Errorf("setting exposure state: %w", err)
	}
	// Imported as state rather than derived, for the same reason the cap flag
	// is: an export taken while an update was owed keeps it owed here, and a
	// fresh genesis carries false so the first block does not recompute what
	// genesis just established.
	if err := k.ExposureRefreshPending.Set(ctx, data.ExposureRefreshPending); err != nil {
		return fmt.Errorf("setting pending exposure refresh: %w", err)
	}

	if err := k.BaseGasPrice.Set(ctx, data.BaseGasPrice); err != nil {
		return fmt.Errorf("setting base gas price: %w", err)
	}

	return nil
}

// ExportGenesis exports exactly the Treasury-owned policy state. Fund balances
// remain part of Bank genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	economicPolicy, err := k.EconomicPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting economic policy: %w", err)
	}
	rewardFunding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward funding state: %w", err)
	}
	economicMandate, err := k.EconomicMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting economic mandate: %w", err)
	}
	exposureState, err := k.getExposureState(ctx)
	if err != nil {
		return nil, err
	}
	exposureUpdatePending, err := k.ExposureRefreshPending.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("getting pending exposure refresh: %w", err)
	}

	factors := make([]types.ConversionFactor, 0)
	if err := k.ConversionFactors.Walk(ctx, nil, func(_ string, factor types.ConversionFactor) (bool, error) {
		factors = append(factors, factor)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating conversion factors: %w", err)
	}

	baseGasPrice, err := k.BaseGasPrice.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting base gas price: %w", err)
	}

	return &types.GenesisState{
		Params:                 params,
		ConversionFactors:      factors,
		RewardFunding:          rewardFunding,
		EconomicMandate:        economicMandate,
		EconomicPolicy:         economicPolicy,
		ExposureState:          exposureState,
		ExposureRefreshPending: exposureUpdatePending,
		BaseGasPrice:           baseGasPrice,
	}, nil
}
