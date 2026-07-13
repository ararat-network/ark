package oracle_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/oracle"
	abcitestutil "ark/abci/testutil"
	vetypes "ark/abci/ve/types"
)

func TestGetOracleVotes(t *testing.T) {
	commitBz := []byte("commit")
	validBz := []byte("valid")
	undecodableBz := []byte("undecodable")
	invalidBz := []byte("invalid")
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})
	decodeErr := errors.New("decode failed")

	testCases := []struct {
		name      string
		setup     func(*abcitestutil.MockVoteExtensionCodec, *abcitestutil.MockExtendedCommitCodec)
		expectErr bool
		check     func(*testing.T, []oracle.Vote)
	}{
		{
			name: "invalid payloads are classified independently",
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 3, validBz),
						abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 2, undecodableBz),
						abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator3"), 1, invalidBz),
						abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator4"), 1, nil),
					},
				}, nil)
				veCodec.EXPECT().Decode(validBz).Return(validVoteExtension, nil)
				veCodec.EXPECT().Decode(undecodableBz).Return(vetypes.OracleVoteExtension{}, decodeErr)
				veCodec.EXPECT().Decode(invalidBz).Return(vetypes.OracleVoteExtension{
					Rates: map[string][]byte{"uusd": nil},
				}, nil)
			},
			check: func(t *testing.T, votes []oracle.Vote) {
				require.Len(t, votes, 4)
				require.Equal(t, validVoteExtension, votes[0].OracleVoteExtension)
				require.Empty(t, votes[1].OracleVoteExtension.Rates)
				require.Empty(t, votes[2].OracleVoteExtension.Rates)
				require.Empty(t, votes[3].OracleVoteExtension.Rates)
			},
		},
		{
			name: "extended commit decode error remains fatal",
			setup: func(_ *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{}, decodeErr)
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			extCommitCodec := abcitestutil.NewMockExtendedCommitCodec(ctrl)
			tc.setup(veCodec, extCommitCodec)

			votes, err := oracle.GetOracleVotes(
				[][]byte{commitBz},
				veCodec,
				extCommitCodec,
			)
			if tc.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			tc.check(t, votes)
		})
	}
}
