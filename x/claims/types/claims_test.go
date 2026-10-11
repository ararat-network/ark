package types_test

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/claims/types"
)

func TestClaimsMandateValidate(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.ClaimsMandate
		mutate    func(*types.ClaimsMandate)
		expectErr string
	}{
		{name: "default sentinel", mandate: types.DefaultClaimsMandate, mutate: func(*types.ClaimsMandate) {}},
		{
			name: "disabled with retained term",
			mandate: func() types.ClaimsMandate {
				return types.NewDisabledClaimsMandate(3)
			},
			mutate: func(*types.ClaimsMandate) {},
		},
		{name: "configured", mandate: validClaimsMandate, mutate: func(*types.ClaimsMandate) {}},
		{
			name:      "partial sentinel",
			mandate:   types.DefaultClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = math.NewInt(1) },
			expectErr: "must be empty",
		},
		{
			name:      "zero configured term",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.Term = 0 },
			expectErr: "term must be positive",
		},
		{
			name:      "invalid committee",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.Committee = "invalid" },
			expectErr: "committee is invalid",
		},
		{
			name:      "invalid active heights",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.ActivationTime = mandate.ExpiryTime },
			expectErr: "activation time must precede expiry time",
		},
		{
			name:      "unset committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = math.Int{} },
			expectErr: "Claims committee claim limit: amount must be nonnegative",
		},
		{
			name:      "zero committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = math.ZeroInt() },
			expectErr: "claim limit must be positive",
		},
		{
			name:      "negative committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = math.NewInt(-1) },
			expectErr: "amount must be nonnegative",
		},
		{
			name:    "committee claim limit at 128 bits",
			mandate: validClaimsMandate,
			mutate: func(mandate *types.ClaimsMandate) {
				mandate.CommitteeClaimLimit = math.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)))
			},
		},
		{
			name:    "committee claim limit past 128 bits",
			mandate: validClaimsMandate,
			mutate: func(mandate *types.ClaimsMandate) {
				mandate.CommitteeClaimLimit = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
			},
			expectErr: "at most 128 bits",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mandate := tc.mandate()
			tc.mutate(&mandate)
			err := mandate.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}

func TestClaimsMandateIsActive(t *testing.T) {
	claimsMandate := validClaimsMandate()
	activation := claimsMandate.ActivationTime
	expiry := claimsMandate.ExpiryTime
	require.False(t, claimsMandate.IsActive(activation-1))
	require.True(t, claimsMandate.IsActive(activation))
	require.True(t, claimsMandate.IsActive(expiry-1))
	require.False(t, claimsMandate.IsActive(expiry))
	require.False(t, types.NewDisabledClaimsMandate(claimsMandate.Term+1).IsActive(activation))
}

func TestClaimValidate(t *testing.T) {
	tests := []struct {
		name      string
		claim     func() types.Claim
		mutate    func(*types.Claim)
		expectErr string
	}{
		{name: "pending", claim: validPendingClaim, mutate: func(*types.Claim) {}},
		{name: "paid", claim: validPaidClaim, mutate: func(*types.Claim) {}},
		{name: "cancelled", claim: validCancelledClaim, mutate: func(*types.Claim) {}},
		{
			name:      "zero claim ID",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.ClaimId = 0 },
			expectErr: "claim ID must be positive",
		},
		{
			name:      "invalid submitter",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Submitter = "invalid" },
			expectErr: "submitter is invalid",
		},
		{
			name:      "invalid origin",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Origin = types.ClaimAuthority_CLAIM_AUTHORITY_UNSPECIFIED },
			expectErr: "origin is invalid",
		},
		{
			name:      "committee claim without mandate term",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.MandateTerm = 0 },
			expectErr: "must record its authorising mandate term",
		},
		{
			name:  "governance claim without mandate term",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
				claim.MandateTerm = 0
				claim.Charge = math.ZeroInt()
			},
		},
		{
			name:      "committee claim without a charge",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Charge = math.ZeroInt() },
			expectErr: "committee claim charge: amount must be positive",
		},
		{
			name:      "committee claim with an unset charge",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Charge = math.Int{} },
			expectErr: "committee claim charge",
		},
		{
			name:  "committee claim charge past 128 bits",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Charge = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
			},
			expectErr: "at most 128 bits",
		},
		{
			name:  "governance claim with a charge",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
				claim.MandateTerm = 0
			},
			expectErr: "governance claim charges nothing",
		},
		{
			name:  "governance claim with an unset charge",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
				claim.MandateTerm = 0
				claim.Charge = math.Int{}
			},
			expectErr: "governance claim charges nothing",
		},
		{
			name:   "stablecoin amount",
			claim:  validPendingClaim,
			mutate: func(claim *types.Claim) { claim.Amount = sdk.NewInt64Coin(chain.USDBaseDenom, 100) },
		},
		{
			name:      "amount outside the Ark denomination shape",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Amount = sdk.NewInt64Coin("uatom", 100) },
			expectErr: "denom must be an Ark-native base denom",
		},
		{
			name:      "zero amount",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Amount = sdk.NewInt64Coin(chain.USDBaseDenom, 0) },
			expectErr: "claim amount must be positive",
		},
		{
			name:  "governance claim with mandate term",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Origin = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
			},
			expectErr: "must not record a mandate term",
		},
		{
			name:  "Insurance self-payment",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Recipient = authtypes.NewModuleAddress(types.InsuranceName).String()
			},
			expectErr: "cannot be the Insurance module",
		},
		{name: "failed", claim: validFailedClaim, mutate: func(*types.Claim) {}},
		{
			name:      "closes at submission",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingTime = claim.SubmittedTime },
			expectErr: "must follow its submission time",
		},
		{
			name:      "paid at its submission time",
			claim:     validPaidClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingTime = claim.SubmittedTime },
			expectErr: "must follow its submission time",
		},
		{
			// A veto may land in the very block that submitted the claim.
			name:   "cancelled in its submission block",
			claim:  validCancelledClaim,
			mutate: func(claim *types.Claim) { claim.ClosingTime = claim.SubmittedTime },
		},
		{
			name:      "closes before submission",
			claim:     validCancelledClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingTime = claim.SubmittedTime - 1 },
			expectErr: "cannot precede its submission time",
		},
		{
			name:      "closes past the header's int64",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingTime = chain.MaxBlockSeconds + 1 },
			expectErr: "exceeds the header's int64",
		},
		{
			name:  "pending with canceller",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.CancelledBy = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
			},
			expectErr: "pending claim cannot record a canceller",
		},
		{
			name:  "paid with canceller",
			claim: validPaidClaim,
			mutate: func(claim *types.Claim) {
				claim.CancelledBy = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
			},
			expectErr: "settled claim cannot record a canceller",
		},
		{
			name:  "failed with canceller",
			claim: validFailedClaim,
			mutate: func(claim *types.Claim) {
				claim.CancelledBy = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
			},
			expectErr: "settled claim cannot record a canceller",
		},
		{
			name:  "cancelled without canceller",
			claim: validCancelledClaim,
			mutate: func(claim *types.Claim) {
				claim.CancelledBy = types.ClaimAuthority_CLAIM_AUTHORITY_UNSPECIFIED
			},
			expectErr: "must record the authority that vetoed it",
		},
		{
			name:      "empty reference",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.Reference = "   " },
			expectErr: "claim reference must not be empty",
		},
		{
			name:  "reference at maximum length",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Reference = strings.Repeat("r", chain.MaxReferenceBytes)
			},
		},
		{
			name:  "reference over maximum length",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Reference = strings.Repeat("r", chain.MaxReferenceBytes+1)
			},
			expectErr: "must not exceed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claim := tc.claim()
			tc.mutate(&claim)
			err := claim.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}

func validClaimsMandate() types.ClaimsMandate {
	return types.ClaimsMandate{
		Envelope: mandate.Envelope{
			Term:           1,
			Committee:      testAddress(1),
			ActivationTime: 10,
			ExpiryTime:     100,
		},
		CommitteeClaimLimit: math.NewInt(1_000),
	}
}

func validPendingClaim() types.Claim {
	return types.Claim{
		ClaimId:         1,
		Submitter:       testAddress(1),
		Origin:          types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE,
		MandateTerm:     1,
		Reference:       "incident-1",
		Recipient:       testAddress(3),
		Amount:          sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: 20,
		SubmittedTime:   20,
		ClosingTime:     25,
		Charge:          math.NewInt(100),
	}
}

func validPaidClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 2
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.ClosingTime = 25
	return claim
}

func validCancelledClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 3
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	// A veto closes the claim early, so its closing time moves back from the
	// 25 submission scheduled to the time the cancellation landed.
	claim.ClosingTime = 22
	claim.CancelledBy = types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE
	return claim
}

func validFailedClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 4
	claim.Status = types.ClaimStatus_CLAIM_STATUS_FAILED
	return claim
}

func noahCoin(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(chain.NoahBaseDenom, amount)
}

func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}
