package proposals

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	abcitestutil "noah/abci/testutil"
	"noah/abci/ve"
	vetypes "noah/abci/ve/types"
)

func TestValidateVoteExtension(t *testing.T) {
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})
	decodeErr := errors.New("decode failed")

	testCases := []struct {
		name      string
		vote      cometabci.ExtendedVoteInfo
		setup     func(*abcitestutil.MockVoteExtensionCodec)
		expectErr bool
	}{
		{
			name: "empty vote extension is valid",
			vote: abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, nil),
		},
		{
			name: "codec decode error is returned",
			vote: abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("bad")),
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("bad")).Return(vetypes.OracleVoteExtension{}, decodeErr)
			},
			expectErr: true,
		},
		{
			name: "invalid oracle vote extension is returned",
			vote: abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("invalid")),
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("invalid")).Return(vetypes.OracleVoteExtension{
					Rates: map[string][]byte{"uusd": nil},
				}, nil)
			},
			expectErr: true,
		},
		{
			name: "valid oracle vote extension passes",
			vote: abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("valid")),
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("valid")).Return(validVoteExtension, nil)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			if tc.setup != nil {
				tc.setup(veCodec)
			}

			err := validateVoteExtension(abcitestutil.NewSDKContext(3, 2), tc.vote, veCodec)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtendedCommitInfo(t *testing.T) {
	validateErr := errors.New("commit validation failed")
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})

	testCases := []struct {
		name              string
		extendedCommit    cometabci.ExtendedCommitInfo
		setup             func(*abcitestutil.MockVoteExtensionCodec)
		validate          ve.ValidateVoteExtensionsFn
		expectErr         bool
		expectedValidated cometabci.ExtendedCommitInfo
	}{
		{
			name: "commit validation error returns before vote extension decoding",
			extendedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{
					abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("valid")),
				},
			},
			validate: func(_ sdk.Context, _ cometabci.ExtendedCommitInfo) error {
				return validateErr
			},
			expectErr: true,
		},
		{
			name: "valid commit validates non-empty vote extensions",
			extendedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{
					abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("valid")),
					abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 1, nil),
				},
			},
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("valid")).Return(validVoteExtension, nil)
			},
			validate: ve.NoOpValidateVoteExtensions,
		},
		{
			name: "invalid oracle vote extension returns error",
			extendedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{
					abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("invalid")),
				},
			},
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("invalid")).Return(vetypes.OracleVoteExtension{
					Rates: map[string][]byte{"uusd": nil},
				}, nil)
			},
			validate:  ve.NoOpValidateVoteExtensions,
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			if tc.setup != nil {
				tc.setup(veCodec)
			}
			handler := newTestProposalHandler(t, veCodec, tc.validate)

			err := handler.ValidateExtendedCommitInfo(abcitestutil.NewSDKContext(3, 2), 3, tc.extendedCommit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPruneAndValidateExtendedCommitInfo(t *testing.T) {
	validateErr := errors.New("commit validation failed")
	validVoteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
		"uusd": math.LegacyNewDec(100),
	})
	validVote := abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("valid"))
	invalidVote := abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator2"), 1, []byte("invalid"))
	invalidVote.ExtensionSignature = []byte("sig")
	prunedInvalidVote := invalidVote
	prunedInvalidVote.BlockIdFlag = cometproto.BlockIDFlagAbsent
	prunedInvalidVote.ExtensionSignature = nil
	prunedInvalidVote.VoteExtension = nil

	testCases := []struct {
		name           string
		extendedCommit cometabci.ExtendedCommitInfo
		setup          func(*abcitestutil.MockVoteExtensionCodec)
		validate       ve.ValidateVoteExtensionsFn
		expectErr      bool
		expectedCommit cometabci.ExtendedCommitInfo
	}{
		{
			name: "invalid vote extension is pruned before commit validation",
			extendedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{validVote, invalidVote},
			},
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("valid")).Return(validVoteExtension, nil)
				veCodec.EXPECT().Decode([]byte("invalid")).Return(vetypes.OracleVoteExtension{}, errors.New("invalid vote extension"))
			},
			validate: func(_ sdk.Context, extInfo cometabci.ExtendedCommitInfo) error {
				require.Equal(t, []cometabci.ExtendedVoteInfo{validVote, prunedInvalidVote}, extInfo.Votes)
				return nil
			},
			expectedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{validVote, prunedInvalidVote},
			},
		},
		{
			name: "commit validation error returns empty commit",
			extendedCommit: cometabci.ExtendedCommitInfo{
				Votes: []cometabci.ExtendedVoteInfo{validVote},
			},
			setup: func(veCodec *abcitestutil.MockVoteExtensionCodec) {
				veCodec.EXPECT().Decode([]byte("valid")).Return(validVoteExtension, nil)
			},
			validate: func(sdk.Context, cometabci.ExtendedCommitInfo) error {
				return validateErr
			},
			expectErr:      true,
			expectedCommit: cometabci.ExtendedCommitInfo{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			if tc.setup != nil {
				tc.setup(veCodec)
			}
			handler := newTestProposalHandler(t, veCodec, tc.validate)

			pruned, err := handler.PruneAndValidateExtendedCommitInfo(abcitestutil.NewSDKContext(3, 2), tc.extendedCommit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedCommit, pruned)
		})
	}
}

func newTestProposalHandler(
	t *testing.T,
	veCodec *abcitestutil.MockVoteExtensionCodec,
	validate ve.ValidateVoteExtensionsFn,
) *ProposalHandler {
	t.Helper()

	ctrl := gomock.NewController(t)
	return NewProposalHandler(
		log.NewTestLogger(t),
		passThroughPrepareProposal,
		acceptProcessProposal,
		validate,
		veCodec,
		abcitestutil.NewMockExtendedCommitCodec(ctrl),
	)
}

func passThroughPrepareProposal(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
	return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
}

func acceptProcessProposal(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
	return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
}
