package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	chain "github.com/ararat-network/ark/pkg/chain"
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
	if data.MonetaryMandate.Committee == k.authority {
		return fmt.Errorf("monetary-policy committee must be distinct from Treasury authority")
	}

	// The reference tax cap and Market's base pool are the same unit by
	// design, permanently: the protocol reference. A treasury genesis whose
	// cap disagrees with the configured reference is not a launchable
	// configuration. x/oracle imports before x/treasury, so the reference is
	// already in state here.
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
	if len(factors) == 0 {
		denoms, err := k.assetKeeper.OraclePricedDenoms(ctx)
		if err != nil {
			return fmt.Errorf("getting oracle-priced denominations: %w", err)
		}
		// Seeded, not derived: a fresh genesis holds no rates by construction,
		// so every member starts at a factor of one rather than a conversion
		// no rate can serve. The seed is the same value the refresh uses for
		// an arrival it cannot derive, and the first block's pass re-derives
		// it from real rates.
		for _, denom := range denoms {
			factors = append(factors, types.ConversionFactor{
				Denom:  denom,
				Factor: math.LegacyOneDec(),
			})
		}
	}
	// A supplied factor set stays loose in one direction only: a member holding
	// no factor is the gap an arrival opens until the next BeginBlocker covers
	// it, so an export taken inside that window still imports. Factors are not
	// checked against live rates either — a kept factor is anchored to the rate
	// it was last derived under.
	//
	// Every factor denomination must name a registry member, because the factor
	// set is the tax base: a factor is the one thing that makes a denomination
	// taxable, so one naming a never-member would have the chain collect tax it
	// can never settle. Such coins verdict UNRECOGNISED, and settlement defers what
	// it cannot price rather than moving it, so they would accumulate in the
	// collector permanently.
	//
	// This refuses nothing a real export carries. A cap outliving its member's
	// departure is exactly what the refresh is built to keep — but departure is
	// a lifecycle status, not a loss of membership, and registry rows are never
	// deleted, so a kept cap still names a member here.
	for _, factor := range factors {
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

	// The collector is held to a weaker rule than the funds above, because it
	// is not a fund: an export taken mid-window carries the member tax that
	// window collected, and NOAH-only would refuse a chain its own export. What
	// it is held to is what the collector can ever legitimately contain — tax,
	// which is collected only in capped denominations, and those are members by
	// the rule above.
	//
	// Bank writes genesis balances directly, so this is the only place the rule
	// can be stated. Nothing at runtime can reach the account: it is a blocked
	// address, which stops every user send and every IBC delivery, and the one
	// inbound path is the ante handler routing tax out of the fee collector.
	collector := k.accountKeeper.GetModuleAccount(ctx, types.StabilityTaxCollectorName)
	if collector == nil {
		return fmt.Errorf("%s module account has not been set", types.StabilityTaxCollectorName)
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
				types.StabilityTaxCollectorName,
				balance.Denom,
				chain.NoahBaseDenom,
			)
		}
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.MonetaryPolicy.Set(ctx, data.MonetaryPolicy); err != nil {
		return fmt.Errorf("setting monetary policy: %w", err)
	}
	for _, factor := range factors {
		if err := k.ConversionFactors.Set(ctx, factor.Denom, factor); err != nil {
			return fmt.Errorf("setting conversion factor %s: %w", factor.Denom, err)
		}
	}
	if err := k.RewardFunding.Set(ctx, data.RewardFunding); err != nil {
		return fmt.Errorf("setting reward funding state: %w", err)
	}
	if err := k.MonetaryMandate.Set(ctx, data.MonetaryMandate); err != nil {
		return fmt.Errorf("setting monetary mandate: %w", err)
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

	// Absent stays absent: the NOAH cross has no seed, so a genesis without
	// one imports a chain on which NOAH has never been derivable.
	if data.NoahConversionFactor != nil {
		if err := k.NoahConversionFactor.Set(ctx, *data.NoahConversionFactor); err != nil {
			return fmt.Errorf("setting the NOAH conversion factor: %w", err)
		}
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
	monetaryPolicy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary policy: %w", err)
	}
	rewardFunding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward funding state: %w", err)
	}
	monetaryMandate, err := k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
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

	var noahFactor *types.ConversionFactor
	stored, err := k.NoahConversionFactor.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("getting the NOAH conversion factor: %w", err)
	}
	if err == nil {
		noahFactor = &stored
	}

	return &types.GenesisState{
		Params:                 params,
		ConversionFactors:      factors,
		RewardFunding:          rewardFunding,
		MonetaryMandate:        monetaryMandate,
		MonetaryPolicy:         monetaryPolicy,
		ExposureState:          exposureState,
		ExposureRefreshPending: exposureUpdatePending,
		BaseGasPrice:           baseGasPrice,
		NoahConversionFactor:   noahFactor,
	}, nil
}
