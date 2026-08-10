package mandate_test

import (
	"math"
	"strings"
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

func TestNext(t *testing.T) {
	tests := []struct {
		name             string
		current          func() mandate.Envelope
		committee        string
		activationHeight uint64
		expiryHeight     uint64
		expect           mandate.Envelope
		expectErr        string
	}{
		{
			name:             "configured successor advances the term",
			current:          validEnvelope,
			committee:        testCommittee(),
			activationHeight: 30,
			expiryHeight:     40,
			expect: mandate.Envelope{
				Term:             2,
				Committee:        testCommittee(),
				ActivationHeight: 30,
				ExpiryHeight:     40,
			},
		},
		{
			name:             "empty committee disables at the advanced term",
			current:          validEnvelope,
			committee:        "",
			activationHeight: 30,
			expiryHeight:     40,
			expect:           mandate.Disabled(2),
		},
		{
			name:      "disabled predecessor still advances the term",
			current:   func() mandate.Envelope { return mandate.Disabled(7) },
			committee: testCommittee(),
			expect: mandate.Envelope{
				Term:      8,
				Committee: testCommittee(),
			},
		},
		{
			name:      "term exhaustion fails closed",
			current:   func() mandate.Envelope { return mandate.Disabled(math.MaxUint64) },
			committee: testCommittee(),
			expectErr: "term cannot advance",
		},
		{
			// Bech32 accepts an all-uppercase spelling of the same account, so
			// the appointment stores the canonical form to keep state, events,
			// and every later string comparison on one spelling.
			name:             "uppercase committee is stored canonical",
			current:          validEnvelope,
			committee:        strings.ToUpper(testCommittee()),
			activationHeight: 30,
			expiryHeight:     40,
			expect: mandate.Envelope{
				Term:             2,
				Committee:        testCommittee(),
				ActivationHeight: 30,
				ExpiryHeight:     40,
			},
		},
		{
			name:      "malformed committee",
			current:   validEnvelope,
			committee: "not-an-address",
			expectErr: "committee is invalid",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			envelope, committeeAddress, err := mandate.Next(
				testCase.current(),
				testCase.committee,
				testCase.activationHeight,
				testCase.expiryHeight,
			)
			if testCase.expectErr != "" {
				require.ErrorContains(t, err, testCase.expectErr)
				require.Equal(t, mandate.Envelope{}, envelope)
				require.Nil(t, committeeAddress)
				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.expect, envelope)
			// The decoded committee accompanies an appointment and is absent
			// from a disablement, so a caller never resolves an empty address.
			if envelope.IsDisabled() {
				require.Nil(t, committeeAddress)
				return
			}
			require.Equal(t, envelope.Committee, committeeAddress.String())
		})
	}
}

func TestEnvelopeAuthorise(t *testing.T) {
	tests := []struct {
		name         string
		envelope     func() mandate.Envelope
		signer       string
		expectedTerm uint64
		height       uint64
		expectErr    string
	}{
		{
			name:         "active window and exact term",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 1,
			height:       10,
		},
		{
			name:         "last active height",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 1,
			height:       19,
		},
		{
			name:         "disabled envelope",
			envelope:     func() mandate.Envelope { return mandate.Disabled(3) },
			signer:       testCommittee(),
			expectedTerm: 3,
			height:       10,
			expectErr:    "signer is not the exact appointed committee",
		},
		{
			name:         "disabled envelope rejects its own empty committee",
			envelope:     func() mandate.Envelope { return mandate.Disabled(3) },
			signer:       "",
			expectedTerm: 3,
			height:       10,
			expectErr:    "signer is not the exact appointed committee",
		},
		{
			name:         "wrong signer",
			envelope:     validEnvelope,
			signer:       authtypes.NewModuleAddress("impostor").String(),
			expectedTerm: 1,
			height:       10,
			expectErr:    "signer is not the exact appointed committee",
		},
		{
			// The ante handler authenticates the signer by decoded bytes, so an
			// uppercase spelling is the same signing account and must authorise.
			name:         "uppercase signer authorises",
			envelope:     validEnvelope,
			signer:       strings.ToUpper(testCommittee()),
			expectedTerm: 1,
			height:       10,
		},
		{
			name:         "malformed signer",
			envelope:     validEnvelope,
			signer:       "not-an-address",
			expectedTerm: 1,
			height:       10,
			expectErr:    "committee signer is invalid",
		},
		{
			name:         "stale term",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 0,
			height:       10,
			expectErr:    "term mismatch: expected 1, got 0",
		},
		{
			name:         "stale term reported before inactive window",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 2,
			height:       25,
			expectErr:    "term mismatch: expected 1, got 2",
		},
		{
			name:         "before activation",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 1,
			height:       9,
			expectErr:    "mandate is not active: window is [10, 20)",
		},
		{
			name:         "at expiry",
			envelope:     validEnvelope,
			signer:       testCommittee(),
			expectedTerm: 1,
			height:       20,
			expectErr:    "mandate is not active: window is [10, 20)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.envelope().Authorise(tc.signer, tc.expectedTerm, tc.height)
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
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
