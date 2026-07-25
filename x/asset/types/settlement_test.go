package types_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func TestSettlementPlanValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.SettlementPlan)
		expectErr string
	}{
		{
			name: "valid open settlement",
		},
		{
			name: "valid earliest closing height",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingHeight = 20
			},
		},
		{
			name: "invalid denom",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.Denom = "aGOLD"
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "nil redemption rate",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = math.LegacyDec{}
			},
			expectErr: "redemption rate must not be nil",
		},
		{
			name: "zero redemption rate",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = math.LegacyZeroDec()
			},
			expectErr: "redemption rate must be positive",
		},
		{
			name: "negative redemption rate",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = math.LegacyOneDec().Neg()
			},
			expectErr: "redemption rate must be positive",
		},
		{
			name: "redemption rate out of range",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = math.LegacyNewDecFromBigInt(
					new(big.Int).Lsh(big.NewInt(1), 256),
				)
			},
			expectErr: "redemption rate is out of range",
		},
		{
			name: "zero activation height",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.ActivationHeight = 0
			},
			expectErr: "activation height must be positive",
		},
		{
			name: "negative earliest closing height",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingHeight = -1
			},
			expectErr: "earliest closing height must not be negative",
		},
		{
			name: "earliest closing height equals activation",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingHeight = plan.ActivationHeight
			},
			expectErr: "must be after activation height",
		},
		{
			name: "zero version",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.Version = 0
			},
			expectErr: "settlement version must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := testSettlementPlan()
			if tt.mutate != nil {
				tt.mutate(&plan)
			}

			err := plan.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}

func TestSettlementPlanQuoteRedemption(t *testing.T) {
	maxAmount := math.NewIntFromBigInt(
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
	)
	tests := []struct {
		name      string
		plan      assettypes.SettlementPlan
		offer     sdk.Coin
		expected  sdk.Coin
		expectErr string
	}{
		{
			name:     "exact output",
			plan:     testSettlementPlan(),
			offer:    sdk.NewInt64Coin("agold", 4),
			expected: sdk.NewInt64Coin(chain.NoahBaseDenom, 3),
		},
		{
			name:     "output rounds down",
			plan:     testSettlementPlan(),
			offer:    sdk.NewInt64Coin("agold", 5),
			expected: sdk.NewInt64Coin(chain.NoahBaseDenom, 3),
		},
		{
			name: "wrong offer denom",
			plan: testSettlementPlan(),
			offer: sdk.NewInt64Coin(
				"asilver",
				5,
			),
			expectErr: "must match asset denom",
		},
		{
			name: "zero offer",
			plan: testSettlementPlan(),
			offer: sdk.NewCoin(
				"agold",
				math.ZeroInt(),
			),
			expectErr: "settlement offer must be positive",
		},
		{
			name: "output rounds to zero",
			plan: assettypes.SettlementPlan{
				Denom:            "agold",
				RedemptionRate:   math.LegacySmallestDec(),
				ActivationHeight: 10,
				Version:          2,
			},
			offer:     sdk.NewInt64Coin("agold", 1),
			expectErr: "settlement output rounds to zero",
		},
		{
			name: "multiplication overflow",
			plan: assettypes.SettlementPlan{
				Denom:            "agold",
				RedemptionRate:   math.LegacyNewDec(2),
				ActivationHeight: 10,
				Version:          2,
			},
			offer: sdk.NewCoin(
				"agold",
				maxAmount,
			),
			expectErr: "multiplying settlement redemption rate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.plan.QuoteRedemption(tt.offer)
			if tt.expectErr == "" {
				require.NoError(t, err)
				require.Equal(t, tt.expected, output)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}

func TestWriteOffRecordValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.WriteOffRecord)
		expectErr string
	}{
		{
			name: "valid direct write-off",
		},
		{
			name: "valid settlement write-off",
			mutate: func(record *assettypes.WriteOffRecord) {
				plan := testSettlementPlan()
				record.SettlementPlan = &plan
			},
		},
		{
			name: "zero version",
			mutate: func(record *assettypes.WriteOffRecord) {
				record.Version = 0
			},
			expectErr: "write-off version must be positive",
		},
		{
			name: "zero height",
			mutate: func(record *assettypes.WriteOffRecord) {
				record.WriteOffHeight = 0
			},
			expectErr: "write-off height must be positive",
		},
		{
			name: "non-positive supply",
			mutate: func(record *assettypes.WriteOffRecord) {
				record.OutstandingSupply.Amount = math.ZeroInt()
			},
			expectErr: "outstanding supply must be positive",
		},
		{
			name: "supply denom mismatch",
			mutate: func(record *assettypes.WriteOffRecord) {
				record.OutstandingSupply.Denom = "asilver"
			},
			expectErr: "must match asset denom agold",
		},
		{
			name: "invalid settlement plan",
			mutate: func(record *assettypes.WriteOffRecord) {
				plan := testSettlementPlan()
				plan.ActivationHeight = 0
				record.SettlementPlan = &plan
			},
			expectErr: "write-off settlement plan is invalid",
		},
		{
			name: "settlement plan denom mismatch",
			mutate: func(record *assettypes.WriteOffRecord) {
				plan := testSettlementPlan()
				plan.Denom = "asilver"
				record.SettlementPlan = &plan
			},
			expectErr: "must match asset denom agold",
		},
		{
			name: "settlement version does not precede write-off",
			mutate: func(record *assettypes.WriteOffRecord) {
				plan := testSettlementPlan()
				plan.Version = record.Version
				record.SettlementPlan = &plan
			},
			expectErr: "must precede write-off version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testWriteOffRecord()
			if tt.mutate != nil {
				tt.mutate(&record)
			}

			err := record.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}

func TestWriteOffRecordNoahLiability(t *testing.T) {
	direct := testWriteOffRecord()
	liability, available, err := direct.NoahLiability()
	require.NoError(t, err)
	require.False(t, available)
	require.Equal(t, sdk.Coin{}, liability)

	settlement := testWriteOffRecord()
	plan := testSettlementPlan()
	settlement.SettlementPlan = &plan
	liability, available, err = settlement.NoahLiability()
	require.NoError(t, err)
	require.True(t, available)
	require.Equal(t, sdk.NewInt64Coin(chain.NoahBaseDenom, 75), liability)
}

func testSettlementPlan() assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom:            "agold",
		RedemptionRate:   math.LegacyMustNewDecFromStr("0.75"),
		ActivationHeight: 10,
		Version:          2,
	}
}

func testWriteOffRecord() assettypes.WriteOffRecord {
	return assettypes.WriteOffRecord{
		Denom:             "agold",
		Version:           3,
		WriteOffHeight:    20,
		OutstandingSupply: sdk.NewInt64Coin("agold", 100),
	}
}
