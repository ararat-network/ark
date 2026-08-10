package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

// Combining the delay bound into one condition means its lower and upper cases
// assert the same message, so the string lives once.
const delayOutOfRange = "SettlementActivationDelayBlocks must be between one and"

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
			mutate:    func(p *assettypes.Params) { p.SettlementActivationDelayBlocks = 0 },
			expectErr: delayOutOfRange,
		},
		{
			name: "settlement activation delay above the ceiling",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelayBlocks = assettypes.MaxSettlementActivationDelayBlocks + 1
			},
			expectErr: delayOutOfRange,
		},
		{
			name: "settlement activation delay at the ceiling is valid",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelayBlocks = assettypes.MaxSettlementActivationDelayBlocks
			},
		},
		{
			// The ceiling exists so the keeper's widening to the int64 block
			// height cannot overflow, which an unbounded uint64 would.
			name: "settlement activation delay cannot overflow the block height",
			mutate: func(p *assettypes.Params) {
				p.SettlementActivationDelayBlocks = 1 << 63
			},
			expectErr: delayOutOfRange,
		},
		{
			name:   "single-block settlement activation delay is valid",
			mutate: func(p *assettypes.Params) { p.SettlementActivationDelayBlocks = 1 },
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
	require.Equal(t, chain.BlocksPerDay, params.SettlementActivationDelayBlocks)
	require.NoError(t, params.Validate())
}
