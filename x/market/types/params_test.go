package types_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/market/types"
)

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{
			name:   "default params",
			mutate: func(p *types.Params) {},
		},
		{
			name: "nil default tobin tax",
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyDec{}
			},
			expectErr: "tobin tax must be set",
		},
		{
			name: "negative default tobin tax",
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyNewDecWithPrec(-1, 4)
			},
			expectErr: "tobin tax must be in [0, 1)",
		},
		{
			// A rate of one consumes the whole output, which is a refusal to
			// convert dressed as a fee.
			name: "default tobin tax of one",
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyOneDec()
			},
			expectErr: "tobin tax must be in [0, 1)",
		},
		{
			name: "zero default tobin tax is valid",
			mutate: func(p *types.Params) {
				p.DefaultTobinTax = math.LegacyZeroDec()
			},
		},
		{
			name: "nil min stability spread",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyDec{}
			},
			expectErr: "min stability spread must be set",
		},
		{
			name: "negative min stability spread",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDec(-1)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name: "min stability spread greater than 1",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDecWithPrec(101, 2)
			},
			expectErr: "min stability spread must be in [0, 1]",
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

// TestParamsCarryNoCapacity pins the split: conversion capacity is delegable
// and must not ride along in the whole-object params replacement, where a
// proposal drafted from a stale copy could revert a committee's resize.
func TestParamsCarryNoCapacity(t *testing.T) {
	fields := reflect.TypeOf(types.DefaultParams())
	for i := range fields.NumField() {
		name := fields.Field(i).Name
		require.NotEqual(t, "BasePool", name)
		require.NotEqual(t, "PoolRecoveryPeriod", name)
	}
}
