package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
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
			name: "valid earliest closing time",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingTime = 20
			},
		},
		{
			name: "earliest closing time at the header's last instant",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingTime = chain.MaxBlockSeconds
			},
		},
		{
			name: "earliest closing time past the header's int64",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingTime = chain.MaxBlockSeconds + 1
			},
			expectErr: "exceeds the header's int64",
		},
		{
			name: "invalid denom",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.Denom = "aGOLD"
			},
			expectErr: "Ark-native base denom matching",
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
			name: "redemption rate at the store bound",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = oracletypes.MaxExchangeRate
			},
		},
		{
			name: "redemption rate above the store bound",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.RedemptionRate = oracletypes.MaxExchangeRate.Add(math.LegacySmallestDec())
			},
			expectErr: "exceeds",
		},
		{
			name: "zero opened height",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.OpenedHeight = 0
			},
			expectErr: "opened height must be positive",
		},
		{
			name: "zero opened time",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.OpenedTime = 0
			},
			expectErr: "opened time must be positive",
		},
		{
			name: "activation at the opening",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.ActivationTime = plan.OpenedTime
			},
			expectErr: "must precede activation time",
		},
		{
			name: "earliest closing time before activation",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingTime = plan.ActivationTime - 1
			},
			expectErr: "must be after activation time",
		},
		{
			name: "earliest closing time equals activation",
			mutate: func(plan *assettypes.SettlementPlan) {
				plan.EarliestClosingTime = plan.ActivationTime
			},
			expectErr: "must be after activation time",
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

func TestResolutionRecordValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.ResolutionRecord)
		expectErr string
	}{
		{
			name: "valid direct write-off",
		},
		{
			name: "valid settlement write-off",
			mutate: func(record *assettypes.ResolutionRecord) {
				plan := testSettlementPlan()
				record.SettlementPlan = &plan
			},
		},
		{
			name: "zero version",
			mutate: func(record *assettypes.ResolutionRecord) {
				record.Version = 0
			},
			expectErr: "resolution version must be positive",
		},
		{
			name: "zero height",
			mutate: func(record *assettypes.ResolutionRecord) {
				record.ResolutionHeight = 0
			},
			expectErr: "resolution height must be positive",
		},
		{
			name: "non-positive supply",
			mutate: func(record *assettypes.ResolutionRecord) {
				record.OutstandingSupply.Amount = math.ZeroInt()
			},
			expectErr: "outstanding supply must be positive",
		},
		{
			name: "supply denom mismatch",
			mutate: func(record *assettypes.ResolutionRecord) {
				record.OutstandingSupply.Denom = "asilver"
			},
			expectErr: "must match asset denom agold",
		},
		{
			name: "invalid settlement plan",
			mutate: func(record *assettypes.ResolutionRecord) {
				plan := testSettlementPlan()
				plan.ActivationTime = 0
				record.SettlementPlan = &plan
			},
			expectErr: "resolution settlement plan is invalid",
		},
		{
			name: "settlement plan denom mismatch",
			mutate: func(record *assettypes.ResolutionRecord) {
				plan := testSettlementPlan()
				plan.Denom = "asilver"
				record.SettlementPlan = &plan
			},
			expectErr: "must match asset denom agold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testResolutionRecord()
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

func testSettlementPlan() assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom:               "agold",
		RedemptionRate:      math.LegacyMustNewDecFromStr("1.25"),
		ActivationTime:      10,
		EarliestClosingTime: 110,
		OpenedHeight:        1,
		OpenedTime:          1,
	}
}

func testResolutionRecord() assettypes.ResolutionRecord {
	return assettypes.ResolutionRecord{
		Denom:             "agold",
		Kind:              assettypes.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
		Version:           3,
		ResolutionHeight:  20,
		OutstandingSupply: sdk.NewInt64Coin("agold", 100),
	}
}
