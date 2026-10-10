package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/claims/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.NoError(t, genesis.Validate())
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.Equal(t, types.DefaultClaimsMandate(), genesis.ClaimsMandate)
	require.True(t, genesis.ClaimsAllowanceUsed.IsZero())
	require.True(t, genesis.InsuranceReserved.IsZero())
	require.Equal(t, uint64(1), genesis.NextClaimId)
	require.Empty(t, genesis.Claims)
}

func TestNewGenesisStateCopiesClaims(t *testing.T) {
	claims := []types.Claim{validPendingClaim()}
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		validClaimsMandate(),
		claims[0].Amount.Amount,
		claims[0].Amount.Amount,
		4,
		claims,
	)

	claims[0].ClaimId = 99
	require.Equal(t, uint64(1), genesis.Claims[0].ClaimId)
}

func TestGenesisValidate(t *testing.T) {
	valid := func() *types.GenesisState {
		claim := validPendingClaim()
		return types.NewGenesisState(
			types.DefaultParams(),
			validClaimsMandate(),
			claim.Amount.Amount,
			claim.Amount.Amount,
			2,
			[]types.Claim{claim},
		)
	}

	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{name: "valid", mutate: func(*types.GenesisState) {}},
		{
			name: "invalid params",
			mutate: func(genesis *types.GenesisState) {
				genesis.Params.ClaimCancellationPeriod = 0
			},
			expectErr: cancellationPeriodOutOfRange,
		},
		{
			name: "invalid mandate",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsMandate.Committee = "invalid"
			},
			expectErr: "committee is invalid",
		},
		{
			name: "contiguous claims are valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []types.Claim{validPendingClaim(), validPaidClaim()}
				genesis.ClaimsAllowanceUsed = math.NewInt(200)
				genesis.NextClaimId = 3
			},
		},
		{
			name: "unsorted claims",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []types.Claim{validPaidClaim(), validPendingClaim()}
				genesis.ClaimsAllowanceUsed = math.NewInt(200)
				genesis.NextClaimId = 3
			},
			expectErr: "must run from ID 1 without a gap: claim 2 at index 0",
		},
		{
			name: "gap in claim IDs",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []types.Claim{validPendingClaim(), validCancelledClaim()}
				genesis.NextClaimId = 3
			},
			expectErr: "must run from ID 1 without a gap: claim 3 at index 1",
		},
		{
			name: "zero next claim ID",
			mutate: func(genesis *types.GenesisState) {
				genesis.NextClaimId = 0
			},
			expectErr: "next claim ID 0 must follow the 1 genesis claims",
		},
		{
			name: "next claim ID behind the claims",
			mutate: func(genesis *types.GenesisState) {
				genesis.NextClaimId = genesis.Claims[0].ClaimId
			},
			expectErr: "next claim ID 1 must follow the 1 genesis claims",
		},
		{
			name: "next claim ID ahead of the claims",
			mutate: func(genesis *types.GenesisState) {
				genesis.NextClaimId = 4
			},
			expectErr: "next claim ID 4 must follow the 1 genesis claims",
		},
		{
			name: "duplicate claim ID",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = append(genesis.Claims, genesis.Claims[0])
				genesis.InsuranceReserved = math.NewInt(200)
				genesis.NextClaimId = 3
			},
			expectErr: "must run from ID 1 without a gap: claim 1 at index 1",
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
				genesis.ClaimsAllowanceUsed = genesis.ClaimsMandate.CommitteeClaimLimit.Amount.AddRaw(1)
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
				genesis.Claims[0].Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
				genesis.Claims[0].MandateTerm = 0
				genesis.ClaimsAllowanceUsed = math.ZeroInt()
			},
		},
		{
			name: "terminal claims are not reserved",
			mutate: func(genesis *types.GenesisState) {
				claim := validPaidClaim()
				claim.ClaimId = 1
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
			name:      "claim from future mandate term",
			mutate:    func(genesis *types.GenesisState) { genesis.Claims[0].MandateTerm = genesis.ClaimsMandate.Term + 1 },
			expectErr: "exceeds current Claims mandate term",
		},
		{
			name: "current-term pending claim closing past mandate expiry",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].ClosingTime = genesis.ClaimsMandate.ExpiryTime + 1
			},
			expectErr: "exceeds its authorising mandate expiry time",
		},
		{
			// Submission permits a closing time exactly at expiry, so import
			// must as well.
			name: "current-term pending claim closing at mandate expiry",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].ClosingTime = genesis.ClaimsMandate.ExpiryTime
			},
		},
		{
			// A prior term's envelope is no longer part of state, so its window
			// cannot be re-checked at import.
			name: "prior-term pending claim closing past mandate expiry",
			mutate: func(genesis *types.GenesisState) {
				genesis.ClaimsMandate.Term = 2
				genesis.ClaimsAllowanceUsed = math.ZeroInt()
				genesis.Claims[0].ClosingTime = genesis.ClaimsMandate.ExpiryTime + 1
			},
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
