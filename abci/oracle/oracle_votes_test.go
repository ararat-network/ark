package oracle_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/codec"
	"ark/abci/oracle"
	abcitestutil "ark/abci/testutil"
	vetypes "ark/abci/voteextension/types"
	arkencoding "ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

var (
	benchmarkRates          map[string]math.LegacyDec
	benchmarkValidatedRates []oracle.VoteRate
	benchmarkOracleVotes    []oracle.Vote
)

func TestParseVoteExtension(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	zeroRate := abcitestutil.MustEncodeRate(t, math.LegacyZeroDec())
	negativeRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(-1))
	alternateRate := append([]byte{'+'}, validRate...)

	tests := []struct {
		name        string
		voteExt     vetypes.OracleVoteExtension
		expected    map[string]math.LegacyDec
		expectedErr string
	}{
		{
			name: "complete report",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ukrw": validRate, "uusd": validRate},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expected: map[string]math.LegacyDec{
				"ukrw": math.LegacyNewDec(100),
				"uusd": math.LegacyNewDec(100),
			},
		},
		{
			name: "empty report",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expected: map[string]math.LegacyDec{},
		},
		{
			name: "zero rate",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": zeroRate},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expected: map[string]math.LegacyDec{"uusd": math.LegacyZeroDec()},
		},
		{
			name: "negative rate",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": negativeRate},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expected: map[string]math.LegacyDec{"uusd": math.LegacyNewDec(-1)},
		},
		{
			name: "alternate integer representation",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": alternateRate},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expected: map[string]math.LegacyDec{"uusd": math.LegacyNewDec(100)},
		},
		{
			name: "nil rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": nil},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "empty rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": {}},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "malformed rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": []byte("not-a-rate")},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "oversized rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"uusd": make([]byte, arkencoding.MaxEncodedLegacyDecBytes+1)},
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expectedErr: "exceeds maximum",
		},
		{
			name: "too many rates",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         makeRateMap(t, oracletypes.MaxVoteTargets+1),
				TargetVersion: oracletypes.InitialVoteTargetVersion,
			},
			expectedErr: "exceeds maximum",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rates, err := oracle.ParseVoteExtension(tc.voteExt)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, rates, len(tc.expected))
			for denom, expected := range tc.expected {
				actual, found := rates[denom]
				require.True(t, found)
				require.True(t, expected.Equal(actual))
			}
		})
	}
}

func TestValidateVoteExtension(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	targets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  []string{"ukrw", "uusd"},
	}

	tests := []struct {
		name          string
		voteExtension vetypes.OracleVoteExtension
		targets       oracletypes.VoteTargetSet
		expectedErr   string
	}{
		{
			name: "complete report",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"ukrw": validRate, "uusd": validRate},
			},
			targets: targets,
		},
		{
			name: "partial report",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"uusd": validRate},
			},
			targets: targets,
		},
		{
			name: "zero rate remains decodable for aggregation",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"uusd": abcitestutil.MustEncodeRate(t, math.LegacyZeroDec())},
			},
			targets: targets,
		},
		{
			name: "empty report",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
			},
			targets: targets,
		},
		{
			name: "malformed rate",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"uusd": []byte("not-a-rate")},
			},
			targets:     targets,
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "unexpected target",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"ueur": validRate},
			},
			targets:     targets,
			expectedErr: "is not in expected targets",
		},
		{
			name: "wrong version",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version + 1,
				Rates:         map[string][]byte{"uusd": validRate},
			},
			targets:     targets,
			expectedErr: "does not match expected version",
		},
		{
			name: "zero target version",
			voteExtension: vetypes.OracleVoteExtension{
				Rates: map[string][]byte{"uusd": validRate},
			},
			targets:     targets,
			expectedErr: "does not match expected version",
		},
		{
			name: "zero expected version",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
			},
			targets: oracletypes.VoteTargetSet{
				Denoms: targets.Denoms,
			},
			expectedErr: "expected vote-target version must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rates, err := oracle.ValidateVoteExtension(tc.voteExtension, tc.targets)
			if tc.expectedErr == "" {
				require.NoError(t, err)
				require.Len(t, rates, len(tc.voteExtension.Rates))
				indexedRates := make(map[int]math.LegacyDec, len(rates))
				for _, rate := range rates {
					indexedRates[rate.TargetIndex] = rate.Value
				}
				for targetIndex, denom := range tc.targets.Denoms {
					rawRate, submitted := tc.voteExtension.Rates[denom]
					if !submitted {
						_, found := indexedRates[targetIndex]
						require.False(t, found)
						continue
					}
					expected, decodeErr := arkencoding.DecodeLegacyDec(rawRate)
					require.NoError(t, decodeErr)
					actual, found := indexedRates[targetIndex]
					require.True(t, found)
					require.True(t, expected.Equal(actual))
				}
				return
			}
			require.ErrorContains(t, err, tc.expectedErr)
			require.Nil(t, rates)
		})
	}
}

func TestGetOracleVotes(t *testing.T) {
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})
	unversionedVoteExtension := validVoteExtension
	unversionedVoteExtension.TargetVersion = 0
	unexpectedTargetVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"ukrw": math.LegacyNewDec(100),
	})
	targets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  []string{"uusd"},
	}

	t.Run("invalid payloads are classified independently", func(t *testing.T) {
		validBz := abcitestutil.MustEncodeVoteExtension(t, validVoteExtension)
		invalidBz := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{
			Rates:         map[string][]byte{"uusd": nil},
			TargetVersion: targets.Version,
		})
		unversionedBz := abcitestutil.MustEncodeVoteExtension(t, unversionedVoteExtension)
		unexpectedTargetBz := abcitestutil.MustEncodeVoteExtension(t, unexpectedTargetVoteExtension)
		commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
			Votes: []cometabci.ExtendedVoteInfo{
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 3, validBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 2, []byte("not-zlib")),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator3"), 1, invalidBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator4"), 1, nil),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator5"), 1, unversionedBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator6"), 1, unexpectedTargetBz),
			},
		})

		votes, err := oracle.GetOracleVotes(voteExtensionCodec, [][]byte{commitBz}, targets, 6)
		require.NoError(t, err)
		require.Len(t, votes, 6)
		require.True(t, votes[0].ValidReport)
		require.Len(t, votes[0].Rates, 1)
		require.Zero(t, votes[0].Rates[0].TargetIndex)
		require.True(t, math.LegacyNewDec(100).Equal(votes[0].Rates[0].Value))
		for _, vote := range votes[1:] {
			require.False(t, vote.ValidReport)
			require.Nil(t, vote.Rates)
		}
	})

	t.Run("valid empty report remains distinguishable from missing extension", func(t *testing.T) {
		validEmptyBz := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{
			TargetVersion: targets.Version,
		})
		commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
			Votes: []cometabci.ExtendedVoteInfo{
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, validEmptyBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 1, nil),
			},
		})

		votes, err := oracle.GetOracleVotes(voteExtensionCodec, [][]byte{commitBz}, targets, 2)
		require.NoError(t, err)
		require.Len(t, votes, 2)
		require.True(t, votes[0].ValidReport)
		require.NotNil(t, votes[0].Rates)
		require.Empty(t, votes[0].Rates)
		require.False(t, votes[1].ValidReport)
		require.Nil(t, votes[1].Rates)
	})

	t.Run("extended commit decode error remains fatal", func(t *testing.T) {
		_, err := oracle.GetOracleVotes(voteExtensionCodec, [][]byte{[]byte("not-protobuf")}, targets, 1)
		require.Error(t, err)
	})
}

func BenchmarkVoteExtension(b *testing.B) {
	voteExtension := vetypes.OracleVoteExtension{
		Rates:         makeRateMap(b, oracletypes.MaxVoteTargets),
		TargetVersion: oracletypes.InitialVoteTargetVersion,
	}
	targets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  makeTargetDenoms(oracletypes.MaxVoteTargets),
	}

	b.Run("parse", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rates, err := oracle.ParseVoteExtension(voteExtension)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkRates = rates
		}
	})

	b.Run("validate_vote_extension", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rates, err := oracle.ValidateVoteExtension(voteExtension, targets)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkValidatedRates = rates
		}
	})
}

func BenchmarkGetOracleVotes(b *testing.B) {
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	cases := []struct {
		targetCount int
		reportCount int
	}{
		{targetCount: 8, reportCount: 8},
		{targetCount: oracletypes.MaxVoteTargets, reportCount: 8},
		{targetCount: oracletypes.MaxVoteTargets, reportCount: oracletypes.MaxVoteTargets},
	}
	for _, benchmarkCase := range cases {
		b.Run(fmt.Sprintf(
			"validators_100/targets_%d/reports_%d",
			benchmarkCase.targetCount,
			benchmarkCase.reportCount,
		), func(b *testing.B) {
			targets := oracletypes.VoteTargetSet{
				Version: oracletypes.InitialVoteTargetVersion,
				Denoms:  makeTargetDenoms(benchmarkCase.targetCount),
			}
			voteExtension := vetypes.OracleVoteExtension{
				Rates:         makeRateMap(b, benchmarkCase.reportCount),
				TargetVersion: targets.Version,
			}
			voteExtensionBz, err := voteExtensionCodec.Encode(voteExtension)
			require.NoError(b, err)
			extendedVotes := make([]cometabci.ExtendedVoteInfo, 100)
			for validatorIndex := range extendedVotes {
				address := make([]byte, 20)
				binary.BigEndian.PutUint64(address[12:], uint64(validatorIndex+1))
				extendedVotes[validatorIndex] = abcitestutil.NewCommitExtendedVoteInfo(
					sdk.ConsAddress(address),
					1,
					voteExtensionBz,
				)
			}
			commitBz, err := codec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{
				Votes: extendedVotes,
			})
			require.NoError(b, err)

			votes, err := oracle.GetOracleVotes(voteExtensionCodec, [][]byte{commitBz}, targets, len(extendedVotes))
			require.NoError(b, err)
			require.Len(b, votes, len(extendedVotes))
			require.Len(b, votes[0].Rates, benchmarkCase.reportCount)

			b.ReportAllocs()
			b.SetBytes(int64(len(commitBz)))
			b.ResetTimer()
			for b.Loop() {
				benchmarkOracleVotes, err = oracle.GetOracleVotes(voteExtensionCodec, [][]byte{commitBz}, targets, len(extendedVotes))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func makeRateMap(t testing.TB, count int) map[string][]byte {
	t.Helper()

	rates := make(map[string][]byte, count)
	for i := range count {
		encoded, err := arkencoding.EncodeLegacyDec(math.LegacyNewDec(int64(i + 1)))
		require.NoError(t, err)
		rates[fmt.Sprintf("u%03d", i)] = encoded
	}
	return rates
}

func makeTargetDenoms(count int) []string {
	denoms := make([]string, count)
	for i := range count {
		denoms[i] = fmt.Sprintf("u%03d", i)
	}
	return denoms
}
