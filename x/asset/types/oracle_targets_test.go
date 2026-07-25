package types_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func TestNewOracleTargetsCanonicalisesDenoms(t *testing.T) {
	input := []string{"ausd", "agold"}

	targets := assettypes.NewOracleTargets(input)
	input[0] = "amutated"

	require.Equal(t, assettypes.InitialOracleTargetVersion, targets.Version)
	require.Equal(t, []string{"agold", "ausd"}, targets.Denoms)
}

func TestOracleTargetsAtHeight(t *testing.T) {
	targets := assettypes.OracleTargets{
		Denoms:  []string{"agold"},
		Version: 4,
		Pending: &assettypes.PendingOracleTargets{
			Denoms:               []string{"asilver"},
			Version:              5,
			ActivationVoteHeight: 20,
		},
	}

	before := targets.AtHeight(19)
	at := targets.AtHeight(20)
	before.Denoms[0] = "amutated"
	at.Denoms[0] = "amutated"

	require.Equal(t, uint64(4), before.Version)
	require.Equal(t, uint64(5), at.Version)
	require.Equal(t, []string{"agold"}, targets.Denoms)
	require.Equal(t, []string{"asilver"}, targets.Pending.Denoms)
}

func TestOracleTargetsValidate(t *testing.T) {
	valid := func() assettypes.OracleTargets {
		return assettypes.OracleTargets{
			Denoms:  []string{"agold", "ausd"},
			Version: 1,
		}
	}
	tooMany := make([]string, assettypes.MaxOracleTargets+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("aasset%03d", i)
	}

	tests := []struct {
		name      string
		mutate    func(*assettypes.OracleTargets)
		expectErr string
	}{
		{
			name: "valid active",
		},
		{
			name: "valid empty active",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = nil
			},
		},
		{
			name: "valid pending",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               []string{"agold"},
					Version:              2,
					ActivationVoteHeight: 10,
				}
			},
		},
		{
			name: "zero active version",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Version = 0
			},
			expectErr: "active vote-target version must be positive",
		},
		{
			name: "too many active targets",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = slices.Clone(tooMany)
			},
			expectErr: "exceeds maximum vote targets",
		},
		{
			name: "unsorted active targets",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"ausd", "agold"}
			},
			expectErr: "must be sorted",
		},
		{
			name: "duplicate active target",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"agold", "agold"}
			},
			expectErr: "contains duplicate denom agold",
		},
		{
			name: "invalid target denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{"aGOLD"}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "native target denom",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Denoms = []string{chain.NoahBaseDenom}
			},
			expectErr: "must not contain native denom anoah",
		},
		{
			name: "pending version does not follow active",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               []string{"agold"},
					Version:              3,
					ActivationVoteHeight: 10,
				}
			},
			expectErr: "must follow active version",
		},
		{
			name: "non-positive activation height",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Pending = &assettypes.PendingOracleTargets{
					Denoms:  []string{"agold"},
					Version: 2,
				}
			},
			expectErr: "activation height must be positive",
		},
		{
			name: "pending matches active",
			mutate: func(targets *assettypes.OracleTargets) {
				targets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               slices.Clone(targets.Denoms),
					Version:              2,
					ActivationVoteHeight: 10,
				}
			},
			expectErr: "must differ from active vote targets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := valid()
			if tt.mutate != nil {
				tt.mutate(&targets)
			}

			err := targets.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}
