package types_test

import (
	"bytes"
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
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = noahCoin(1) },
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
			mutate:    func(mandate *types.ClaimsMandate) { mandate.ActivationHeight = mandate.ExpiryHeight },
			expectErr: "activation height must precede expiry height",
		},
		{
			name:      "unset committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = sdk.Coin{} },
			expectErr: "invalid Claims committee claim limit",
		},
		{
			name:      "zero committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = noahCoin(0) },
			expectErr: "claim limit must be positive",
		},
		{
			name:    "negative committee claim limit",
			mandate: validClaimsMandate,
			// Built by hand rather than through a constructor: sdk.NewInt64Coin
			// panics on a negative amount, so the only way a negative limit
			// reaches Validate is decoded off the wire, which is exactly the
			// case worth pinning.
			mutate: func(mandate *types.ClaimsMandate) {
				mandate.CommitteeClaimLimit = sdk.Coin{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}
			},
			expectErr: "negative coin amount",
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
	activation := claimsMandate.ActivationHeight
	expiry := claimsMandate.ExpiryHeight
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
			},
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
			mutate:    func(claim *types.Claim) { claim.ClosingHeight = claim.SubmittedHeight },
			expectErr: "must follow its submission height",
		},
		{
			name:      "paid at its submission height",
			claim:     validPaidClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingHeight = claim.SubmittedHeight },
			expectErr: "must follow its submission height",
		},
		{
			// A veto may land in the very block that submitted the claim.
			name:   "cancelled in its submission block",
			claim:  validCancelledClaim,
			mutate: func(claim *types.Claim) { claim.ClosingHeight = claim.SubmittedHeight },
		},
		{
			name:      "closes before submission",
			claim:     validCancelledClaim,
			mutate:    func(claim *types.Claim) { claim.ClosingHeight = claim.SubmittedHeight - 1 },
			expectErr: "cannot precede its submission height",
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
				claim.Reference = strings.Repeat("r", types.MaxClaimReferenceLength)
			},
		},
		{
			name:  "reference over maximum length",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Reference = strings.Repeat("r", types.MaxClaimReferenceLength+1)
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
			Term:             1,
			Committee:        testAddress(1),
			ActivationHeight: 10,
			ExpiryHeight:     100,
		},
		CommitteeClaimLimit: noahCoin(1_000),
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
		ClosingHeight:   25,
	}
}

func validPaidClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 2
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.ClosingHeight = 25
	return claim
}

func validCancelledClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 3
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	// A veto closes the claim early, so its closing height moves back from the
	// 25 submission scheduled to the height the cancellation landed.
	claim.ClosingHeight = 22
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
