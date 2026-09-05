package oracle_test

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	"github.com/ararat-network/ark/abci/oracle"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

var (
	benchmarkRates          map[string]math.LegacyDec
	benchmarkValidatedRates []oracle.VoteRate
	benchmarkOracleVotes    []oracle.Vote
)

func TestParseVoteExtension(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	// A leading zero byte decodes to the same integer, so it is the compact
	// encoding's alternate-representation attack.
	paddedRate := append([]byte{0x00}, validRate...)
	// The widest permitted rate: the largest raw value encoding to
	// MaxEncodedVoteRateBytes big-endian bytes.
	maxSizeValue := math.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 8*oracletypes.MaxEncodedVoteRateBytes), big.NewInt(1)),
		math.LegacyPrecision,
	)
	maxSizeRate := abcitestutil.MustEncodeRate(t, maxSizeValue)
	require.Len(t, maxSizeRate, oracletypes.MaxEncodedVoteRateBytes)

	tests := []struct {
		name        string
		voteExt     vetypes.OracleVoteExtension
		expected    map[string]math.LegacyDec
		expectedErr string
	}{
		{
			name: "complete report",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"akrw": validRate, "ausd": validRate},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expected: map[string]math.LegacyDec{
				"akrw": math.LegacyNewDec(100),
				"ausd": math.LegacyNewDec(100),
			},
		},
		{
			name: "empty report",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expected: map[string]math.LegacyDec{},
		},
		{
			name: "zero rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": {0x00}},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "padded alternate representation",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": paddedRate},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "nil rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": nil},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "empty rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": {}},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "longest permitted rate",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": maxSizeRate},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expected: map[string]math.LegacyDec{"ausd": maxSizeValue},
		},
		{
			name: "oversized rate bytes",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         map[string][]byte{"ausd": make([]byte, oracletypes.MaxEncodedVoteRateBytes+1)},
				TargetVersion: oracletypes.InitialFeedVersion,
			},
			expectedErr: "exceeds maximum",
		},
		{
			name: "too many rates",
			voteExt: vetypes.OracleVoteExtension{
				Rates:         makeRateMap(t, oracletypes.MaxFeeds+1),
				TargetVersion: oracletypes.InitialFeedVersion,
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
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"akrw", "ausd"},
	}

	tests := []struct {
		name          string
		voteExtension vetypes.OracleVoteExtension
		targets       oracletypes.FeedSet
		expectedErr   string
	}{
		{
			name: "complete report",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"akrw": validRate, "ausd": validRate},
			},
			targets: targets,
		},
		{
			name: "partial report",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"ausd": validRate},
			},
			targets: targets,
		},
		{
			name: "zero rate is unrepresentable",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"ausd": {}},
			},
			targets:     targets,
			expectedErr: "invalid oracle vote extension rate",
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
				Rates:         map[string][]byte{"ausd": {0x00, 0x01}},
			},
			targets:     targets,
			expectedErr: "invalid oracle vote extension rate",
		},
		{
			name: "rate at the size bound",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates: map[string][]byte{
					"ausd": abcitestutil.MustEncodeRate(
						t,
						math.LegacyNewDecFromBigIntWithPrec(
							new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 8*oracletypes.MaxEncodedVoteRateBytes), big.NewInt(1)),
							math.LegacyPrecision,
						),
					),
				},
			},
			targets: targets,
		},
		{
			name: "rate above the size bound",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates: map[string][]byte{
					"ausd": abcitestutil.MustEncodeRate(
						t,
						math.LegacyMustNewDecFromStr("1"+strings.Repeat("0", 22)),
					),
				},
			},
			targets:     targets,
			expectedErr: "exceeds maximum",
		},
		{
			name: "unexpected target",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
				Rates:         map[string][]byte{"aeur": validRate},
			},
			targets:     targets,
			expectedErr: "is not in expected targets",
		},
		{
			name: "wrong version",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version + 1,
				Rates:         map[string][]byte{"ausd": validRate},
			},
			targets:     targets,
			expectedErr: "does not match expected version",
		},
		{
			name: "zero target version",
			voteExtension: vetypes.OracleVoteExtension{
				Rates: map[string][]byte{"ausd": validRate},
			},
			targets:     targets,
			expectedErr: "does not match expected version",
		},
		{
			name: "zero expected version",
			voteExtension: vetypes.OracleVoteExtension{
				TargetVersion: targets.Version,
			},
			targets: oracletypes.FeedSet{
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
					expected, decodeErr := encoding.DecodeCompactLegacyDec(rawRate)
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
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"ausd": math.LegacyNewDec(100),
	})
	unversionedVoteExtension := validVoteExtension
	unversionedVoteExtension.TargetVersion = 0
	unexpectedTargetVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"akrw": math.LegacyNewDec(100),
	})
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ausd"},
	}

	t.Run("invalid payloads are classified independently", func(t *testing.T) {
		validBz := abcitestutil.MustEncodeVoteExtension(t, validVoteExtension)
		invalidBz := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{
			Rates:         map[string][]byte{"ausd": nil},
			TargetVersion: targets.Version,
		})
		unversionedBz := abcitestutil.MustEncodeVoteExtension(t, unversionedVoteExtension)
		unexpectedTargetBz := abcitestutil.MustEncodeVoteExtension(t, unexpectedTargetVoteExtension)
		commitBz := abcitestutil.MustEncodeExtendedCommit(t, cmtabci.ExtendedCommitInfo{
			Votes: []cmtabci.ExtendedVoteInfo{
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 3, validBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 2, []byte("not-zlib")),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator3"), 1, invalidBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator4"), 1, nil),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator5"), 1, unversionedBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator6"), 1, unexpectedTargetBz),
			},
		})

		votes, err := oracle.GetOracleVotes([][]byte{commitBz}, targets, 6)
		require.NoError(t, err)
		require.Len(t, votes, 6)
		require.Len(t, votes[0].Rates, 1)
		require.Zero(t, votes[0].Rates[0].TargetIndex)
		require.True(t, math.LegacyNewDec(100).Equal(votes[0].Rates[0].Value))
		require.False(t, votes[0].Invalid)
		for _, vote := range votes[1:] {
			require.Nil(t, vote.Rates)
		}
		// The absent validator is the only nil-rates entry not flagged invalid.
		require.True(t, votes[1].Invalid)
		require.True(t, votes[2].Invalid)
		require.False(t, votes[3].Invalid)
		require.True(t, votes[4].Invalid)
		require.True(t, votes[5].Invalid)
	})

	t.Run("valid empty report is equivalent to a missing extension", func(t *testing.T) {
		validEmptyBz := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{
			TargetVersion: targets.Version,
		})
		commitBz := abcitestutil.MustEncodeExtendedCommit(t, cmtabci.ExtendedCommitInfo{
			Votes: []cmtabci.ExtendedVoteInfo{
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, validEmptyBz),
				abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 1, nil),
			},
		})

		votes, err := oracle.GetOracleVotes([][]byte{commitBz}, targets, 2)
		require.NoError(t, err)
		require.Len(t, votes, 2)
		require.Empty(t, votes[0].Rates)
		require.Empty(t, votes[1].Rates)
		require.False(t, votes[0].Invalid)
		require.False(t, votes[1].Invalid)
	})

	t.Run("extended commit decode error remains fatal", func(t *testing.T) {
		_, err := oracle.GetOracleVotes([][]byte{[]byte("not-protobuf")}, targets, 1)
		require.Error(t, err)
	})
}

func BenchmarkVoteExtension(b *testing.B) {
	voteExtension := vetypes.OracleVoteExtension{
		Rates:         makeRateMap(b, oracletypes.MaxFeeds),
		TargetVersion: oracletypes.InitialFeedVersion,
	}
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  makeTargetDenoms(oracletypes.MaxFeeds),
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
	cases := []struct {
		targetCount int
		reportCount int
	}{
		{targetCount: 8, reportCount: 8},
		{targetCount: oracletypes.MaxFeeds, reportCount: 8},
		{targetCount: oracletypes.MaxFeeds, reportCount: oracletypes.MaxFeeds},
	}
	for _, benchmarkCase := range cases {
		b.Run(fmt.Sprintf(
			"validators_100/targets_%d/reports_%d",
			benchmarkCase.targetCount,
			benchmarkCase.reportCount,
		), func(b *testing.B) {
			targets := oracletypes.FeedSet{
				Version: oracletypes.InitialFeedVersion,
				Denoms:  makeTargetDenoms(benchmarkCase.targetCount),
			}
			voteExtension := vetypes.OracleVoteExtension{
				Rates:         makeRateMap(b, benchmarkCase.reportCount),
				TargetVersion: targets.Version,
			}
			voteExtensionBz, err := codec.EncodeVoteExtension(voteExtension)
			require.NoError(b, err)
			extendedVotes := make([]cmtabci.ExtendedVoteInfo, 100)
			for validatorIndex := range extendedVotes {
				address := make([]byte, 20)
				binary.BigEndian.PutUint64(address[12:], uint64(validatorIndex+1))
				extendedVotes[validatorIndex] = abcitestutil.NewCommitExtendedVoteInfo(
					sdk.ConsAddress(address),
					1,
					voteExtensionBz,
				)
			}
			commitBz, err := codec.EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{
				Votes: extendedVotes,
			})
			require.NoError(b, err)

			votes, err := oracle.GetOracleVotes([][]byte{commitBz}, targets, len(extendedVotes))
			require.NoError(b, err)
			require.Len(b, votes, len(extendedVotes))
			require.Len(b, votes[0].Rates, benchmarkCase.reportCount)

			b.ReportAllocs()
			b.SetBytes(int64(len(commitBz)))
			b.ResetTimer()
			for b.Loop() {
				benchmarkOracleVotes, err = oracle.GetOracleVotes([][]byte{commitBz}, targets, len(extendedVotes))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func makeRateMap(tb testing.TB, count int) map[string][]byte {
	tb.Helper()

	rates := make(map[string][]byte, count)
	for i := range count {
		encoded, err := encoding.EncodeCompactLegacyDec(math.LegacyNewDec(int64(i + 1)))
		require.NoError(tb, err)
		rates[fmt.Sprintf("a%03d", i)] = encoded
	}
	return rates
}

func makeTargetDenoms(count int) []string {
	denoms := make([]string, count)
	for i := range count {
		denoms[i] = fmt.Sprintf("a%03d", i)
	}
	return denoms
}
