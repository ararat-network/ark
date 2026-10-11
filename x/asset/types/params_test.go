package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	assettypes "github.com/ararat-network/ark/x/asset/types"
)

// Combining the delay bound into one condition means its lower and upper cases
// assert the same message, so the string lives once.
const delayOutOfRange = "SettlementActivationDelay must be between one and"

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.Params)
		expectErr string
	}{
		{name: "default is valid", mutate: func(*assettypes.Params) {}},
		{
			// Zero would read as "activate immediately", which silently removes
			// the only window a mistaken plan can be withdrawn in.
			name:      "zero settlement activation delay",
			mutate:    func(p *assettypes.Params) { p.SettlementActivationDelay = 0 },
			expectErr: delayOutOfRange,
		},
		{
			name: "settlement activation delay above the ceiling",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelay = assettypes.MaxSettlementActivationDelay + 1
			},
			expectErr: delayOutOfRange,
		},
		{
			name: "settlement activation delay at the ceiling is valid",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelay = assettypes.MaxSettlementActivationDelay
			},
		},
		{
			// The ceiling exists so the keeper's addition to block time
			// cannot overflow, which an unbounded uint64 would.
			name: "settlement activation delay cannot overflow the block time",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelay = 1 << 63
			},
			expectErr: delayOutOfRange,
		},
		{
			name:   "one-second settlement activation delay is valid",
			mutate: func(p *assettypes.Params) { p.SettlementActivationDelay = 1 },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := assettypes.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestDefaultParamsValues(t *testing.T) {
	params := assettypes.DefaultParams()
	require.Equal(t, uint64(7*86_400), params.SettlementActivationDelay, "a week of block time")
	require.NoError(t, params.Validate())
}
