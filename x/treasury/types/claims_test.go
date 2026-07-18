package types_test

import (
	"bytes"
	"strings"
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
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
			name:    "cancellation period equals active span",
			mandate: validClaimsMandate,
			mutate: func(mandate *types.ClaimsMandate) {
				mandate.ExpiryHeight = mandate.ActivationHeight + mandate.CancellationPeriodBlocks
			},
		},
		{
			name:      "partial sentinel",
			mandate:   types.DefaultClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CancellationPeriodBlocks = 1 },
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
			name:      "zero cancellation period",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CancellationPeriodBlocks = 0 },
			expectErr: "cancellation period blocks must be positive",
		},
		{
			name:    "cancellation period exceeds active span",
			mandate: validClaimsMandate,
			mutate: func(mandate *types.ClaimsMandate) {
				mandate.ExpiryHeight = mandate.ActivationHeight + mandate.CancellationPeriodBlocks - 1
			},
			expectErr: "cannot exceed the Claims mandate active span",
		},
		{
			name:      "unset committee claim limit",
			mandate:   validClaimsMandate,
			mutate:    func(mandate *types.ClaimsMandate) { mandate.CommitteeClaimLimit = math.Int{} },
			expectErr: "claim limit must be set",
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
			expectErr: "claim limit must be positive",
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
	mandate := validClaimsMandate()
	require.False(t, mandate.IsActive(mandate.ActivationHeight-1))
	require.True(t, mandate.IsActive(mandate.ActivationHeight))
	require.True(t, mandate.IsActive(mandate.ExpiryHeight-1))
	require.False(t, mandate.IsActive(mandate.ExpiryHeight))
	require.False(t, types.NewDisabledClaimsMandate(mandate.Term+1).IsActive(mandate.ActivationHeight))
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
			mutate:    func(claim *types.Claim) { claim.Origin = types.ClaimOrigin_CLAIM_ORIGIN_UNSPECIFIED },
			expectErr: "origin is invalid",
		},
		{
			name:      "zero mandate term",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.MandateTerm = 0 },
			expectErr: "mandate term must be positive",
		},
		{
			name:  "Insurance self-payment",
			claim: validPendingClaim,
			mutate: func(claim *types.Claim) {
				claim.Recipient = authtypes.NewModuleAddress(types.InsuranceName).String()
			},
			expectErr: "cannot be the Insurance module",
		},
		{
			name:      "executable at submission",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.ExecutableHeight = claim.SubmittedHeight },
			expectErr: "must follow its submission height",
		},
		{
			name:      "pending with finalized by",
			claim:     validPendingClaim,
			mutate:    func(claim *types.Claim) { claim.FinalizedBy = testAddress(4) },
			expectErr: "cannot contain finalisation fields",
		},
		{
			name:      "paid before executable",
			claim:     validPaidClaim,
			mutate:    func(claim *types.Claim) { claim.FinalizedHeight = claim.ExecutableHeight - 1 },
			expectErr: "cannot finalise before its executable height",
		},
		{
			name:      "paid with cancellation reason",
			claim:     validPaidClaim,
			mutate:    func(claim *types.Claim) { claim.CancellationReason = "reason" },
			expectErr: "cannot contain cancellation fields",
		},
		{
			name:      "cancelled at executable height",
			claim:     validCancelledClaim,
			mutate:    func(claim *types.Claim) { claim.FinalizedHeight = claim.ExecutableHeight },
			expectErr: "during its cancellation period",
		},
		{
			name:      "cancelled without reason",
			claim:     validCancelledClaim,
			mutate:    func(claim *types.Claim) { claim.CancellationReason = "" },
			expectErr: "cancellation reason must not be empty",
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

func TestClaimReferenceBounds(t *testing.T) {
	require.NoError(t, types.ValidateClaimReference(
		"reference",
		strings.Repeat("r", types.MaxClaimReferenceLength),
		true,
	))
	require.Error(t, types.ValidateClaimReference(
		"reference",
		strings.Repeat("r", types.MaxClaimReferenceLength+1),
		true,
	))
}

func validClaimsMandate() types.ClaimsMandate {
	return types.ClaimsMandate{
		Term:                     1,
		Committee:                testAddress(1),
		ActivationHeight:         10,
		ExpiryHeight:             100,
		CancellationPeriodBlocks: 5,
		CommitteeClaimLimit:      math.NewInt(1_000),
	}
}

func validPendingClaim() types.Claim {
	return types.Claim{
		ClaimId:           1,
		Submitter:         testAddress(1),
		Origin:            types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE,
		MandateTerm:       1,
		IncidentReference: "incident",
		Recipient:         testAddress(3),
		Amount:            sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		EvidenceReference: "evidence",
		Status:            types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight:   20,
		ExecutableHeight:  25,
	}
}

func validPaidClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 2
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.FinalizedHeight = 25
	claim.FinalizedBy = testAddress(4)
	return claim
}

func validCancelledClaim() types.Claim {
	claim := validPendingClaim()
	claim.ClaimId = 3
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	claim.FinalizedHeight = 22
	claim.FinalizedBy = testAddress(2)
	claim.CancellationReason = "not covered"
	claim.CancellationReference = "proposal-1"
	return claim
}

func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}
