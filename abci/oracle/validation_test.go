package oracle_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/abci/oracle"
	oracleencoding "ark/abci/oracle/encoding"
	abcitestutil "ark/abci/testutil"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

func TestValidateVoteExtension(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	zeroRate := abcitestutil.MustEncodeRate(t, math.LegacyZeroDec())
	negativeRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(-1))

	testCases := []struct {
		name      string
		voteExt   vetypes.OracleVoteExtension
		expectErr bool
	}{
		{
			name: "valid canonical rate",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": validRate,
			}},
		},
		{
			name: "zero rate is valid explicit abstention",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": zeroRate,
			}},
		},
		{
			name: "negative rate is valid explicit abstention",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": negativeRate,
			}},
		},
		{
			name: "nil rate bytes reject",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": nil,
			}},
			expectErr: true,
		},
		{
			name: "empty rate bytes reject",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": {},
			}},
			expectErr: true,
		},
		{
			name: "malformed rate bytes reject",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": []byte("not-a-rate"),
			}},
			expectErr: true,
		},
		{
			name: "oversized rate bytes reject",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uusd": make([]byte, oracleencoding.MaxEncodedRateBytes+1),
			}},
			expectErr: true,
		},
		{
			name: "invalid denom rejects",
			voteExt: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"bad denom": validRate,
			}},
			expectErr: true,
		},
		{
			name: "too many rates reject",
			voteExt: vetypes.OracleVoteExtension{
				Rates: makeRateMap(t, oracletypes.MaxVoteTargets+1),
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := oracle.ValidateVoteExtension(tc.voteExt)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func makeRateMap(t *testing.T, count int) map[string][]byte {
	t.Helper()

	rates := make(map[string][]byte, count)
	for i := range count {
		rates[fmt.Sprintf("u%03d", i)] = abcitestutil.MustEncodeRate(t, math.LegacyNewDec(int64(i+1)))
	}
	return rates
}
