package chain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/chain"
)

func TestValidateReference(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    string
		required bool
		err      string
	}{
		{"transaction hash", "0x" + strings.Repeat("ab", 32), true, ""},
		{"at the limit", strings.Repeat("r", chain.MaxReferenceBytes), true, ""},
		{"one byte past the limit", strings.Repeat("r", chain.MaxReferenceBytes+1), true, "must not exceed 2048 bytes"},
		{"limit counts bytes, not characters", strings.Repeat("é", chain.MaxReferenceBytes/2+1), true, "must not exceed"},
		{"empty when required", "", true, "must not be empty"},
		{"blank when required", " \t\n", true, "must not be empty"},
		{"empty when optional", "", false, ""},
		{"past the limit when optional", strings.Repeat("r", chain.MaxReferenceBytes+1), false, "must not exceed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := chain.ValidateReference("reference", tc.value, tc.required)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
