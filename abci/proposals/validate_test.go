package proposals_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/proposals"
	abcitestutil "ark/abci/testutil"
	"ark/abci/ve"
)

func TestValidateExtendedCommitInfo(t *testing.T) {
	validateErr := errors.New("commit validation failed")
	extendedCommit := cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{
			abcitestutil.NewCommitExtendedVoteInfo(sdk.ConsAddress("validator1"), 1, []byte("vote-extension")),
		},
	}

	testCases := []struct {
		name      string
		validate  ve.ValidateVoteExtensionsFn
		expectErr bool
	}{
		{
			name: "authenticated extended commit is accepted",
			validate: func(_ sdk.Context, got cometabci.ExtendedCommitInfo) error {
				require.Equal(t, extendedCommit, got)
				return nil
			},
		},
		{
			name: "commit validation error is returned",
			validate: func(_ sdk.Context, got cometabci.ExtendedCommitInfo) error {
				require.Equal(t, extendedCommit, got)
				return validateErr
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestProposalHandler(t, tc.validate)

			err := handler.ValidateExtendedCommitInfo(abcitestutil.NewSDKContext(3, 2), 3, extendedCommit)
			if tc.expectErr {
				require.ErrorIs(t, err, validateErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newTestProposalHandler(
	t *testing.T,
	validate ve.ValidateVoteExtensionsFn,
) *proposals.Handler {
	t.Helper()

	ctrl := gomock.NewController(t)
	return proposals.NewHandler(
		log.NewTestLogger(t),
		passThroughPrepareProposal,
		acceptProcessProposal,
		validate,
		abcitestutil.NewMockExtendedCommitCodec(ctrl),
	)
}
