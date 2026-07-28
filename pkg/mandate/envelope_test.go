package mandate_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/mandate"
)

func testCommittee() string {
	return authtypes.NewModuleAddress("test-committee").String()
}

func validEnvelope() mandate.Envelope {
	return mandate.Envelope{
		Term:             1,
		Committee:        testCommittee(),
		ActivationHeight: 10,
		ExpiryHeight:     20,
	}
}

func TestEnvelopeValidate(t *testing.T) {
	tests := []struct {
		name      string
		envelope  func() mandate.Envelope
		mutate    func(*mandate.Envelope)
		expectErr string
	}{
		{
			name:     "disabled default",
			envelope: func() mandate.Envelope { return mandate.Disabled(0) },
			mutate:   func(*mandate.Envelope) {},
		},
		{
			name:     "disabled retaining term",
			envelope: func() mandate.Envelope { return mandate.Disabled(7) },
			mutate:   func(*mandate.Envelope) {},
		},
		{
			name:     "configured",
			envelope: validEnvelope,
			mutate:   func(*mandate.Envelope) {},
		},
		{
			name:      "disabled with activation height",
			envelope:  func() mandate.Envelope { return mandate.Disabled(1) },
			mutate:    func(e *mandate.Envelope) { e.ActivationHeight = 1 },
			expectErr: "disabled envelope must not have an activation or expiry height",
		},
		{
			name:      "disabled with expiry height",
			envelope:  func() mandate.Envelope { return mandate.Disabled(1) },
			mutate:    func(e *mandate.Envelope) { e.ExpiryHeight = 1 },
			expectErr: "disabled envelope must not have an activation or expiry height",
		},
		{
			name:      "configured zero term",
			envelope:  validEnvelope,
			mutate:    func(e *mandate.Envelope) { e.Term = 0 },
			expectErr: "configured term must be positive",
		},
		{
			name:      "non-canonical committee",
			envelope:  validEnvelope,
			mutate:    func(e *mandate.Envelope) { e.Committee = "invalid" },
			expectErr: "committee is invalid",
		},
		{
			name:      "empty window",
			envelope:  validEnvelope,
			mutate:    func(e *mandate.Envelope) { e.ActivationHeight = e.ExpiryHeight },
			expectErr: "activation height must precede expiry height",
		},
		{
			name:      "inverted window",
			envelope:  validEnvelope,
			mutate:    func(e *mandate.Envelope) { e.ActivationHeight = e.ExpiryHeight + 1 },
			expectErr: "activation height must precede expiry height",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope := tc.envelope()
			tc.mutate(&envelope)
			err := envelope.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestEnvelopeIsActive(t *testing.T) {
	envelope := validEnvelope()
	tests := []struct {
		name   string
		height uint64
		want   bool
	}{
		{name: "before activation", height: 9},
		{name: "at activation", height: 10, want: true},
		{name: "last active height", height: 19, want: true},
		{name: "at expiry", height: 20},
		{name: "after expiry", height: 21},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, envelope.IsActive(tc.height))
		})
	}
}

func TestDisabledEnvelopeIsNeverActive(t *testing.T) {
	envelope := mandate.Disabled(3)
	require.True(t, envelope.IsDisabled())
	require.False(t, envelope.IsActive(0))
	require.False(t, envelope.IsActive(math.MaxInt64))
	require.False(t, validEnvelope().IsDisabled())
}

func TestEnvelopeNextTerm(t *testing.T) {
	term, err := mandate.Disabled(0).NextTerm()
	require.NoError(t, err)
	require.Equal(t, uint64(1), term)

	term, err = validEnvelope().NextTerm()
	require.NoError(t, err)
	require.Equal(t, uint64(2), term)

	_, err = mandate.Disabled(math.MaxUint64).NextTerm()
	require.ErrorContains(t, err, "term cannot advance")
}

func TestEnvelopeRequireTerm(t *testing.T) {
	envelope := validEnvelope()
	require.NoError(t, envelope.RequireTerm(1))
	require.ErrorContains(
		t,
		envelope.RequireTerm(2),
		"term mismatch: expected 1, got 2",
	)
	require.ErrorContains(
		t,
		envelope.RequireTerm(0),
		"term mismatch: expected 1, got 0",
	)
}
