package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

const pricingDenom = chain.USDBaseDenom

// TestPriceVerdictFoldsStatusToVerdict pins the registry's authority table:
// which statuses are priced, on whose authority, and the reason attached to
// each that is not.
func TestPriceVerdictFoldsStatusToVerdict(t *testing.T) {
	rate := math.LegacyNewDec(2)
	planRate := math.LegacyNewDec(5)

	testCases := []struct {
		name       string
		status     types.AssetStatus
		rated      bool
		plan       bool
		wantPriced bool
		wantRate   math.LegacyDec
		wantSource types.PriceSource
		wantReason types.UnpricedReason
	}{
		{
			name:       "active with a fresh rate prices on Oracle authority",
			status:     types.AssetStatus_ASSET_STATUS_ACTIVE,
			rated:      true,
			wantPriced: true,
			wantRate:   rate,
			wantSource: types.PriceSource_PRICE_SOURCE_ORACLE,
		},
		{
			name:       "issuance-halted is a member like any other",
			status:     types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			rated:      true,
			wantPriced: true,
			wantRate:   rate,
			wantSource: types.PriceSource_PRICE_SOURCE_ORACLE,
		},
		{
			name:       "a member the Oracle could not price is feed-unavailable",
			status:     types.AssetStatus_ASSET_STATUS_ACTIVE,
			wantReason: types.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE,
		},
		{
			// A member is never asked for a plan, so one present alongside a
			// priceable status must not reach the verdict.
			name:       "a member ignores a settlement plan",
			status:     types.AssetStatus_ASSET_STATUS_ACTIVE,
			rated:      true,
			plan:       true,
			wantPriced: true,
			wantRate:   rate,
			wantSource: types.PriceSource_PRICE_SOURCE_ORACLE,
		},
		{
			name:       "suspended with a plan prices on the governance commitment",
			status:     types.AssetStatus_ASSET_STATUS_SUSPENDED,
			plan:       true,
			wantPriced: true,
			wantRate:   planRate,
			wantSource: types.PriceSource_PRICE_SOURCE_SETTLEMENT,
		},
		{
			name:       "suspended without a plan is untrusted",
			status:     types.AssetStatus_ASSET_STATUS_SUSPENDED,
			wantReason: types.UnpricedReason_UNPRICED_REASON_UNTRUSTED,
		},
		{
			// A suspended feed keeps running, so a rate is present and must
			// lose to the status: the market price is what stopped being
			// trustworthy.
			name:       "suspended ignores a lingering Oracle rate",
			status:     types.AssetStatus_ASSET_STATUS_SUSPENDED,
			rated:      true,
			wantReason: types.UnpricedReason_UNPRICED_REASON_UNTRUSTED,
		},
		{
			name:       "written-off carries no redemption obligation",
			status:     types.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			rated:      true,
			wantReason: types.UnpricedReason_UNPRICED_REASON_WRITTEN_OFF,
		},
		{
			name:       "retired is residual supply",
			status:     types.AssetStatus_ASSET_STATUS_RETIRED,
			wantReason: types.UnpricedReason_UNPRICED_REASON_RETIRED,
		},
		{
			name:       "an unspecified status is not understood",
			status:     types.AssetStatus_ASSET_STATUS_UNSPECIFIED,
			wantReason: types.UnpricedReason_UNPRICED_REASON_UNRECOGNISED,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			asset := types.Asset{Denom: pricingDenom, Status: testCase.status, Version: 1}
			rates := oracletypes.NewRateSet()
			if testCase.rated {
				rates[pricingDenom] = rate
			}
			var plan *types.SettlementPlan
			if testCase.plan {
				plan = &types.SettlementPlan{
					Denom:                 pricingDenom,
					RedemptionRate:        planRate,
					OpenedHeight:          1,
					ActivationHeight:      2,
					EarliestClosingHeight: 3,
				}
			}

			pricing := types.PriceVerdict(asset, rates, plan)

			require.Equal(t, testCase.wantPriced, pricing.IsPriced())
			if !testCase.wantPriced {
				require.Equal(t, testCase.wantReason, pricing.Reason)
				return
			}
			require.Equal(t, testCase.wantSource, pricing.Source)
			require.NotNil(t, pricing.Rate)
			require.True(t, testCase.wantRate.Equal(*pricing.Rate))
		})
	}
}

// TestPriceVerdictIgnoresActivationHeight pins that a plan values suspended
// supply from the block it opens, before redemption is open. Consumers that
// care whether redemption may execute ask the activation-gated accessor; a
// verdict that moved with activation would make them disagree for the length
// of the delay.
func TestPriceVerdictIgnoresActivationHeight(t *testing.T) {
	asset := types.Asset{
		Denom:   pricingDenom,
		Status:  types.AssetStatus_ASSET_STATUS_SUSPENDED,
		Version: 1,
	}
	planRate := math.LegacyNewDec(5)
	plan := &types.SettlementPlan{
		Denom:                 pricingDenom,
		RedemptionRate:        planRate,
		OpenedHeight:          1,
		ActivationHeight:      1_000_000,
		EarliestClosingHeight: 1_000_001,
	}

	pricing := types.PriceVerdict(asset, oracletypes.NewRateSet(), plan)

	require.True(t, pricing.IsPriced())
	require.Equal(t, types.PriceSource_PRICE_SOURCE_SETTLEMENT, pricing.Source)
	require.True(t, planRate.Equal(*pricing.Rate))
}

// TestNumeraireVerdictIsOneByDefinition pins NOAH's verdict.
func TestNumeraireVerdictIsOneByDefinition(t *testing.T) {
	verdict := types.NumeraireVerdict()

	require.True(t, verdict.IsPriced())
	require.Equal(t, types.PriceSource_PRICE_SOURCE_NUMERAIRE, verdict.Source)
	require.True(t, verdict.Rate.Equal(math.LegacyOneDec()))
}

// TestAssetPricingsConvertUsesPricedVerdictsOnly pins that conversion refuses
// an unpriced denomination rather than reaching past the verdict for a rate.
func TestAssetPricingsConvertUsesPricedVerdictsOnly(t *testing.T) {
	pricings := types.AssetPricings{
		chain.NoahBaseDenom: types.NumeraireVerdict(),
		pricingDenom: {
			Reason: types.UnpricedReason_UNPRICED_REASON_UNTRUSTED,
		},
	}

	_, err := pricings.Convert(
		sdk.NewDecCoin(pricingDenom, math.NewInt(10)),
		chain.NoahBaseDenom,
	)

	require.ErrorIs(t, err, oracletypes.ErrUnknownDenom)
}
