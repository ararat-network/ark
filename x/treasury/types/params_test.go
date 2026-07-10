package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/treasury/types"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(p *types.Params) {},
		},
		// TaxPolicy
		{
			name: "tax policy rate max less than rate min",
			mutate: func(p *types.Params) {
				p.TaxPolicy.RateMax = math.LegacyNewDecWithPrec(1, 4)
				p.TaxPolicy.RateMin = math.LegacyNewDecWithPrec(1, 2)
			},
			expectErr: "TaxPolicy.RateMax",
		},
		{
			name: "tax policy rate max nil",
			mutate: func(p *types.Params) {
				p.TaxPolicy.RateMax = math.LegacyDec{}
			},
			expectErr: "TaxPolicy.RateMax must be set",
		},
		{
			name: "tax policy rate min nil",
			mutate: func(p *types.Params) {
				p.TaxPolicy.RateMin = math.LegacyDec{}
			},
			expectErr: "TaxPolicy.RateMin must be set",
		},
		{
			name: "tax policy rate min negative",
			mutate: func(p *types.Params) {
				p.TaxPolicy.RateMin = math.LegacyNewDec(-1)
			},
			expectErr: "TaxPolicy.RateMin must be zero or positive",
		},
		{
			name: "tax policy rate min zero is valid",
			mutate: func(p *types.Params) {
				p.TaxPolicy.RateMin = math.LegacyZeroDec()
			},
		},
		{
			name: "tax policy cap invalid",
			mutate: func(p *types.Params) {
				p.TaxPolicy.Cap = sdk.Coin{Denom: "", Amount: math.NewInt(-1)}
			},
			expectErr: "TaxPolicy.Cap is invalid",
		},
		{
			name: "tax policy change rate max negative",
			mutate: func(p *types.Params) {
				p.TaxPolicy.ChangeRateMax = math.LegacyNewDec(-1)
			},
			expectErr: "TaxPolicy.ChangeRateMax must be zero or positive",
		},
		{
			name: "tax policy change rate max nil",
			mutate: func(p *types.Params) {
				p.TaxPolicy.ChangeRateMax = math.LegacyDec{}
			},
			expectErr: "TaxPolicy.ChangeRateMax must be set",
		},
		{
			name: "tax policy change rate max zero is valid",
			mutate: func(p *types.Params) {
				p.TaxPolicy.ChangeRateMax = math.LegacyZeroDec()
			},
		},
		// RewardPolicy
		{
			name: "reward policy rate max less than rate min",
			mutate: func(p *types.Params) {
				p.RewardPolicy.RateMax = math.LegacyNewDecWithPrec(1, 2)
				p.RewardPolicy.RateMin = math.LegacyNewDecWithPrec(10, 2)
			},
			expectErr: "RewardPolicy.RateMax",
		},
		{
			name: "reward policy rate max nil",
			mutate: func(p *types.Params) {
				p.RewardPolicy.RateMax = math.LegacyDec{}
			},
			expectErr: "RewardPolicy.RateMax must be set",
		},
		{
			name: "reward policy rate min nil",
			mutate: func(p *types.Params) {
				p.RewardPolicy.RateMin = math.LegacyDec{}
			},
			expectErr: "RewardPolicy.RateMin must be set",
		},
		{
			name: "reward policy rate min negative",
			mutate: func(p *types.Params) {
				p.RewardPolicy.RateMin = math.LegacyNewDec(-1)
			},
			expectErr: "RewardPolicy.RateMin",
		},
		{
			name: "reward policy rate min zero is valid",
			mutate: func(p *types.Params) {
				p.RewardPolicy.RateMin = math.LegacyZeroDec()
				p.RewardPolicy.RateMax = math.LegacyNewDecWithPrec(50, 2)
			},
		},
		{
			name: "reward policy change rate max negative",
			mutate: func(p *types.Params) {
				p.RewardPolicy.ChangeRateMax = math.LegacyNewDec(-1)
			},
			expectErr: "RewardPolicy.ChangeRateMax must be zero or positive",
		},
		{
			name: "reward policy change rate max nil",
			mutate: func(p *types.Params) {
				p.RewardPolicy.ChangeRateMax = math.LegacyDec{}
			},
			expectErr: "RewardPolicy.ChangeRateMax must be set",
		},
		{
			name: "reward policy change rate max zero is valid",
			mutate: func(p *types.Params) {
				p.RewardPolicy.ChangeRateMax = math.LegacyZeroDec()
			},
		},
		// SeigniorageBurdenTarget
		{
			name: "seigniorage burden target negative",
			mutate: func(p *types.Params) {
				p.SeigniorageBurdenTarget = math.LegacyNewDec(-1)
			},
			expectErr: "SeigniorageBurdenTarget must be zero or positive",
		},
		{
			name: "seigniorage burden target nil",
			mutate: func(p *types.Params) {
				p.SeigniorageBurdenTarget = math.LegacyDec{}
			},
			expectErr: "SeigniorageBurdenTarget must be set",
		},
		{
			name: "seigniorage burden target zero is valid",
			mutate: func(p *types.Params) {
				p.SeigniorageBurdenTarget = math.LegacyZeroDec()
			},
		},
		// MiningIncrement
		{
			name: "burn weight negative",
			mutate: func(p *types.Params) {
				p.BurnWeight = math.LegacyNewDec(-1)
			},
			expectErr: "BurnWeight must be between zero and 1 - RewardPolicy.RateMax",
		},
		{
			name: "burn weight nil",
			mutate: func(p *types.Params) {
				p.BurnWeight = math.LegacyDec{}
			},
			expectErr: "BurnWeight must be set",
		},
		{
			name: "burn weight plus reward max greater than one",
			mutate: func(p *types.Params) {
				p.BurnWeight = math.LegacyNewDecWithPrec(60, 2)
			},
			expectErr: "BurnWeight must be between zero and 1 - RewardPolicy.RateMax",
		},
		{
			name: "burn weight zero is valid",
			mutate: func(p *types.Params) {
				p.BurnWeight = math.LegacyZeroDec()
			},
		},
		{
			name: "burn weight at max allowed is valid",
			mutate: func(p *types.Params) {
				p.BurnWeight = math.LegacyOneDec().Sub(p.RewardPolicy.RateMax)
			},
		},
		// MiningIncrement
		{
			name: "mining increment negative",
			mutate: func(p *types.Params) {
				p.MiningIncrement = math.LegacyNewDec(-1)
			},
			expectErr: "MiningIncrement must be zero or positive",
		},
		{
			name: "mining increment nil",
			mutate: func(p *types.Params) {
				p.MiningIncrement = math.LegacyDec{}
			},
			expectErr: "MiningIncrement must be set",
		},
		{
			name: "mining increment zero is valid",
			mutate: func(p *types.Params) {
				p.MiningIncrement = math.LegacyZeroDec()
			},
		},
		// WindowLong / WindowShort
		{
			name: "window short zero",
			mutate: func(p *types.Params) {
				p.WindowShort = 0
			},
			expectErr: "WindowShort must be positive",
		},
		{
			name: "window long zero",
			mutate: func(p *types.Params) {
				p.WindowLong = 0
				p.WindowShort = 0
			},
			expectErr: "WindowLong must be positive",
		},
		{
			name: "window long equal to window short",
			mutate: func(p *types.Params) {
				p.WindowLong = 4
				p.WindowShort = 4
			},
			expectErr: "WindowLong must be greater than WindowShort",
		},
		{
			name: "window long less than window short",
			mutate: func(p *types.Params) {
				p.WindowLong = 2
				p.WindowShort = 4
			},
			expectErr: "WindowLong must be greater than WindowShort",
		},
		{
			name: "window long one more than window short is valid",
			mutate: func(p *types.Params) {
				p.WindowLong = 5
				p.WindowShort = 4
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
