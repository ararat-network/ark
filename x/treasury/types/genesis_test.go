package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.True(t, types.DefaultMonetaryPolicy().Equal(genesis.MonetaryPolicy))
	require.Empty(t, genesis.TaxCaps)
	require.Equal(t, types.DefaultClaimsMandate(), genesis.ClaimsMandate)
	require.True(t, genesis.ClaimsAllowanceUsed.IsZero())
	require.True(t, genesis.InsuranceReserved.IsZero())
	require.Equal(t, uint64(1), genesis.NextClaimId)
	require.Empty(t, genesis.Claims)
	require.Equal(t, types.DefaultRewardFundingState(), genesis.RewardFunding)
	require.Equal(t, types.DefaultMonetaryMandate(), genesis.MonetaryMandate)
	require.NoError(t, genesis.Validate())
}

func TestNewGenesisStateCopiesSlices(t *testing.T) {
	mandate := validClaimsMandate()
	claims := []types.Claim{validPendingClaim()}
	taxCaps := []types.TaxCap{{Denom: chain.USDBaseDenom, TaxCap: math.OneInt()}}
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		types.DefaultMonetaryPolicy(),
		taxCaps,
		mandate,
		math.NewInt(100),
		math.NewInt(100),
		2,
		claims,
		types.DefaultRewardFundingState(),
		types.DefaultMonetaryMandate(),
	)
	taxCaps[0].Denom = "mutated"
	claims[0].ClaimId = 99
	require.Equal(t, chain.USDBaseDenom, genesis.TaxCaps[0].Denom)
	require.Equal(t, uint64(1), genesis.Claims[0].ClaimId)
}

func TestGenesisTaxCapValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name: "positive reference cap requires positive derived caps",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.OneInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()}}
			},
		},
		{
			name: "positive reference cap rejects uncapped sentinel",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.OneInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()}}
			},
			expectErr: "must be positive when the reference tax cap is positive",
		},
		{
			name: "zero reference cap permits uncapped sentinel",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()}}
			},
		},
		{
			name: "zero reference cap rejects positive derived cap",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
				genesis.TaxCaps = []types.TaxCap{{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()}}
			},
			expectErr: "must be zero when the reference tax cap is zero",
		},
		{
			name: "sorted tax caps are valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.KRWBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
		},
		{
			name: "unsorted tax caps",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.KRWBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
			expectErr: "genesis tax caps must be sorted by unique denom",
		},
		{
			name: "duplicate tax cap denom",
			mutate: func(genesis *types.GenesisState) {
				genesis.TaxCaps = []types.TaxCap{
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
					{Denom: chain.USDBaseDenom, TaxCap: math.ZeroInt()},
				}
			},
			expectErr: "genesis tax caps must be sorted by unique denom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(genesis)

			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestGenesisClaimsValidation(t *testing.T) {
	valid := func() *types.GenesisState {
		claim := validPendingClaim()
		return types.NewGenesisState(
			types.DefaultParams(),
			types.DefaultMonetaryPolicy(),
			[]types.TaxCap{},
			validClaimsMandate(),
			claim.Amount.Amount,
			claim.Amount.Amount,
			4,
			[]types.Claim{claim},
			types.DefaultRewardFundingState(),
			types.DefaultMonetaryMandate(),
		)
	}

	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{name: "valid", mutate: func(*types.GenesisState) {}},
		{
			name: "sorted claims are valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []types.Claim{validPendingClaim(), validPaidClaim()}
				genesis.ClaimsAllowanceUsed = math.NewInt(200)
			},
		},
		{
			name: "unsorted claims",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []types.Claim{validPaidClaim(), validPendingClaim()}
				genesis.ClaimsAllowanceUsed = math.NewInt(200)
			},
			expectErr: "genesis claims must be sorted by unique claim ID",
		},
		{
			name: "zero next claim ID",
			mutate: func(genesis *types.GenesisState) {
				genesis.NextClaimId = 0
			},
			expectErr: "next claim ID must be positive",
		},
		{
			name: "next claim ID does not follow imported claims",
			mutate: func(genesis *types.GenesisState) {
				genesis.NextClaimId = genesis.Claims[0].ClaimId
			},
			expectErr: "must be below next claim ID",
		},
		{
			name: "duplicate claim ID",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = append(genesis.Claims, genesis.Claims[0])
				genesis.InsuranceReserved = math.NewInt(200)
			},
			expectErr: "genesis claims must be sorted by unique claim ID",
		},
		{
			name: "Insurance reservation mismatch",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = math.NewInt(99)
			},
			expectErr: "does not equal pending claim sum",
		},
		{
			name: "unset allowance usage",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsAllowanceUsed = math.Int{}
			},
			expectErr: "allowance usage must be set",
		},
		{
			name: "negative allowance usage",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsAllowanceUsed = math.NewInt(-1)
			},
			expectErr: "allowance usage must be zero or positive",
		},
		{
			name: "allowance usage exceeds mandate limit",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsAllowanceUsed = genesis.ClaimsMandate.CommitteeClaimLimit.AddRaw(1)
			},
			expectErr: "exceeds mandate limit",
		},
		{
			name: "allowance usage mismatch",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsAllowanceUsed = math.NewInt(99)
			},
			expectErr: "does not equal current-term committee claim sum",
		},
		{
			name: "governance claims do not use committee allowance",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].Origin = types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE
				genesis.Claims[0].MandateTerm = 0
				genesis.ClaimsAllowanceUsed = math.ZeroInt()
			},
		},
		{
			name: "terminal claims are not reserved",
			mutate: func(genesis *types.GenesisState) {
				claim := validPaidClaim()
				genesis.Claims = []types.Claim{claim}
				genesis.InsuranceReserved = math.ZeroInt()
			},
		},
		{
			name: "unset Insurance reservation",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = math.Int{}
			},
			expectErr: "must be set",
		},
		{
			name: "negative Insurance reservation",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = math.NewInt(-1)
			},
			expectErr: "zero or positive",
		},
		{
			name: "invalid claim",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].Recipient = "invalid"
			},
			expectErr: "recipient is invalid",
		},
		{
			name: "same Claims and monetary-policy committee",
			mutate: func(genesis *types.GenesisState) {
				genesis.MonetaryMandate = validMonetaryMandate()
				genesis.MonetaryMandate.Committee = genesis.ClaimsMandate.Committee
			},
			expectErr: "monetary-policy committee must be distinct from Claims committee",
		},
		{
			name:      "claim from future mandate term",
			mutate:    func(genesis *types.GenesisState) { genesis.Claims[0].MandateTerm = genesis.ClaimsMandate.Term + 1 },
			expectErr: "exceeds current Claims mandate term",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := valid()
			tc.mutate(genesis)
			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
