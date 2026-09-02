package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/market/types"
)

func validConversionTotals() types.ConversionTotals {
	return types.ConversionTotals{
		GrossOffer:        math.NewInt(100),
		EligiblePrincipal: math.NewInt(95),
		RedemptionOutput:  math.NewInt(40),
		RedeemedValue:     math.LegacyNewDec(50),
	}
}

func TestConversionTotalsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.ConversionTotals)
		expectErr string
	}{
		{
			name:   "valid totals",
			mutate: func(*types.ConversionTotals) {},
		},
		{
			name: "zero totals",
			mutate: func(totals *types.ConversionTotals) {
				*totals = types.ConversionTotals{
					GrossOffer:        math.ZeroInt(),
					EligiblePrincipal: math.ZeroInt(),
					RedemptionOutput:  math.ZeroInt(),
					RedeemedValue:     math.LegacyZeroDec(),
				}
			},
		},
		{
			name: "eligible principal equals the gross offer",
			mutate: func(totals *types.ConversionTotals) {
				totals.EligiblePrincipal = totals.GrossOffer
			},
		},
		{
			name: "redemption output equals the redeemed liability",
			mutate: func(totals *types.ConversionTotals) {
				totals.RedemptionOutput = math.NewInt(50)
			},
		},
		{
			name: "unset gross offer",
			mutate: func(totals *types.ConversionTotals) {
				totals.GrossOffer = math.Int{}
			},
			expectErr: "incomplete",
		},
		{
			name: "unset eligible principal",
			mutate: func(totals *types.ConversionTotals) {
				totals.EligiblePrincipal = math.Int{}
			},
			expectErr: "incomplete",
		},
		{
			name: "unset redeemed value",
			mutate: func(totals *types.ConversionTotals) {
				totals.RedeemedValue = math.LegacyDec{}
			},
			expectErr: "incomplete",
		},
		{
			name: "negative gross offer",
			mutate: func(totals *types.ConversionTotals) {
				totals.GrossOffer = math.NewInt(-1)
			},
			expectErr: "cannot be negative",
		},
		{
			name: "negative redeemed value",
			mutate: func(totals *types.ConversionTotals) {
				totals.RedeemedValue = math.LegacyNewDec(-1)
			},
			expectErr: "cannot be negative",
		},
		{
			name: "eligible principal exceeds the gross offer",
			mutate: func(totals *types.ConversionTotals) {
				totals.EligiblePrincipal = totals.GrossOffer.AddRaw(1)
			},
			expectErr: "exceeds the gross offer",
		},
		{
			name: "redemption output exceeds the redeemed liability",
			mutate: func(totals *types.ConversionTotals) {
				totals.RedemptionOutput = math.NewInt(51)
			},
			expectErr: "exceeds the redeemed liability",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			totals := validConversionTotals()
			tc.mutate(&totals)
			err := totals.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

// TestConversionTotalsIsZero pins that any recorded flow, including a
// spread-only expansion that grew liability by nothing, obliges settlement.
func TestConversionTotalsIsZero(t *testing.T) {
	zero := types.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	}
	require.True(t, zero.IsZero())

	tests := []struct {
		name   string
		mutate func(*types.ConversionTotals)
	}{
		{
			name: "spread-only expansion",
			mutate: func(totals *types.ConversionTotals) {
				totals.GrossOffer = math.OneInt()
			},
		},
		{
			name: "redemption",
			mutate: func(totals *types.ConversionTotals) {
				totals.RedemptionOutput = math.OneInt()
				totals.RedeemedValue = math.LegacyOneDec()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			totals := zero
			tc.mutate(&totals)
			require.False(t, totals.IsZero())
		})
	}
}
