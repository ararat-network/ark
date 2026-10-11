package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.NoError(t, genesis.Validate())
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.Equal(t, types.DefaultClaimsMandate(), genesis.ClaimsMandate)
	require.True(t, genesis.ClaimsAllowanceUsed.IsZero())
	require.True(t, genesis.InsuranceReserved.Empty())
	require.Equal(t, uint64(1), genesis.NextClaimId)
	require.Empty(t, genesis.Claims)
}

func TestNewGenesisStateCopiesClaims(t *testing.T) {
	claims := []types.Claim{validPendingClaim()}
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		validClaimsMandate(),
		claims[0].Charge,
		sdk.NewCoins(claims[0].Amount),
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
			claim.Charge,
			sdk.NewCoins(claim.Amount),
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
				genesis.InsuranceReserved = sdk.NewCoins(noahCoin(200))
				genesis.NextClaimId = 3
			},
			expectErr: "must run from ID 1 without a gap: claim 1 at index 1",
		},
		{
			name: "Insurance reservation mismatch",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = sdk.NewCoins(noahCoin(99))
			},
			expectErr: "does not equal pending claim sum",
		},
		{
			name: "reservation in the wrong denomination",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
			},
			expectErr: "does not equal pending claim sum",
		},
		{
			name: "reservations sum by denomination",
			mutate: func(genesis *types.GenesisState) {
				stable := validPendingClaim()
				stable.ClaimId = 2
				stable.Amount = sdk.NewInt64Coin(chain.USDBaseDenom, 40)
				stable.Charge = math.NewInt(60)
				genesis.Claims = append(genesis.Claims, stable)
				genesis.ClaimsAllowanceUsed = math.NewInt(160)
				genesis.InsuranceReserved = sdk.NewCoins(noahCoin(100), sdk.NewInt64Coin(chain.USDBaseDenom, 40))
				genesis.NextClaimId = 3
			},
		},
		{
			name: "allowance usage sums charges, not amounts",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].Amount = sdk.NewInt64Coin(chain.USDBaseDenom, 100)
				genesis.InsuranceReserved = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
				genesis.Claims[0].Charge = math.NewInt(140)
				genesis.ClaimsAllowanceUsed = math.NewInt(140)
			},
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
			expectErr: "does not equal current-term committee charge sum",
		},
		{
			name: "governance claims do not use committee allowance",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims[0].Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
				genesis.Claims[0].MandateTerm = 0
				genesis.Claims[0].Charge = math.ZeroInt()
				genesis.ClaimsAllowanceUsed = math.ZeroInt()
			},
		},
		{
			name: "terminal claims are not reserved",
			mutate: func(genesis *types.GenesisState) {
				claim := validPaidClaim()
				claim.ClaimId = 1
				genesis.Claims = []types.Claim{claim}
				genesis.InsuranceReserved = sdk.NewCoins()
			},
		},
		{
			name: "zero Insurance reservation entry",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = sdk.Coins{noahCoin(0)}
			},
			expectErr: "amount is not positive",
		},
		{
			name: "negative Insurance reservation",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}
			},
			expectErr: "amount is not positive",
		},
		{
			name: "unsorted Insurance reservations",
			mutate: func(genesis *types.GenesisState) {
				genesis.InsuranceReserved = sdk.Coins{sdk.NewInt64Coin(chain.USDBaseDenom, 1), noahCoin(100)}
			},
			expectErr: "is not sorted",
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
