package proposals_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/core/header"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/proposals"
	abcitestutil "ark/abci/testutil"
	arkabci "ark/abci/types"
)

const (
	proposalTestChainID = "proposal-test-chain"
	proposalTestHeight  = int64(101)
)

type proposalValidationFixture struct {
	ctx            sdk.Context
	commit         cometabci.ExtendedCommitInfo
	validatorStore testValidatorStore
}

type testValidatorStore map[string]cmtprotocrypto.PublicKey

func (s testValidatorStore) GetPubKeyByConsAddr(_ context.Context, consAddr sdk.ConsAddress) (cmtprotocrypto.PublicKey, error) {
	pubKey, ok := s[string(consAddr)]
	if !ok {
		return cmtprotocrypto.PublicKey{}, errors.New("validator not found")
	}

	return pubKey, nil
}

func TestPrepareProposalHandler(t *testing.T) {
	fixture := newProposalValidationFixture(t)
	initialCtx := abcitestutil.NewSDKContext(100, 1).
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, cometabci.CommitInfo{}))
	appTx1 := []byte("tx1")
	appTx2 := []byte("tx2")
	commitInfo := fixture.commit
	commitBz := abcitestutil.MustEncodeExtendedCommit(t, commitInfo)
	commitBzSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{commitBz})
	maxTxBytes := int64(len(commitBz) + 100)
	largeAppTx := make([]byte, maxTxBytes-int64(len(commitBz)))
	prepareErr := errors.New("prepare proposal failed")

	testCases := []struct {
		name                 string
		ctx                  sdk.Context
		req                  *cometabci.RequestPrepareProposal
		prepare              sdk.PrepareProposalHandler
		expectErr            bool
		expectNilResponse    bool
		expectedTxs          [][]byte
		expectedCategory     error
		expectedErrIs        error
		expectedPrepareTxs   [][]byte
		expectedMaxTxBytes   int64
		expectedWrappedCalls int
	}{
		{
			name: "nil request returns error",
			ctx:  fixture.ctx,
			req:  nil,
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:         true,
			expectNilResponse: true,
			expectedCategory:  arkabci.ErrNilRequest,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cometabci.RequestPrepareProposal{
				Height:     1,
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions disabled returns wrapped handler error",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cometabci.RequestPrepareProposal{
				Height:     1,
				Txs:        [][]byte{appTx1},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, _ *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return nil, prepareErr
			},
			expectErr:            true,
			expectedTxs:          [][]byte{},
			expectedCategory:     arkabci.ErrWrappedHandler,
			expectedErrIs:        prepareErr,
			expectedPrepareTxs:   [][]byte{appTx1},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "nonstandard initial height passes app txs without commit info",
			ctx:  initialCtx,
			req: &cometabci.RequestPrepareProposal{
				Height:     100,
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled injects encoded commit info",
			ctx:  fixture.ctx,
			req: &cometabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1, appTx2},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{commitBz, appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled falls back to commit-only proposal on wrapped handler error",
			ctx:  fixture.ctx,
			req: &cometabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, _ *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return nil, prepareErr
			},
			expectedTxs:          [][]byte{commitBz},
			expectedPrepareTxs:   [][]byte{appTx1},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "protobuf overhead excludes transaction that raw size would admit",
			ctx:  fixture.ctx,
			req: &cometabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{largeAppTx},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				return &cometabci.ResponsePrepareProposal{}, nil
			},
			expectedTxs:          [][]byte{commitBz},
			expectedPrepareTxs:   [][]byte{largeAppTx},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "encoded commit info larger than max tx bytes returns error",
			ctx:  fixture.ctx,
			req: &cometabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      commitBzSize - 1,
			},
			prepare: func(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:   true,
			expectedTxs: [][]byte{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var originalMaxTxBytes int64
			var originalTxs [][]byte
			if tc.req != nil {
				originalMaxTxBytes = tc.req.MaxTxBytes
				originalTxs = slices.Clone(tc.req.Txs)
			}
			wrappedCalls := 0
			prepare := sdk.PrepareProposalHandler(func(ctx sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
				wrappedCalls++
				if tc.expectedPrepareTxs != nil {
					require.Equal(t, tc.expectedPrepareTxs, req.Txs)
				}
				if tc.expectedMaxTxBytes != 0 {
					require.Equal(t, tc.expectedMaxTxBytes, req.MaxTxBytes)
				}
				return tc.prepare(ctx, req)
			})

			handler := proposals.NewHandler(
				prepare,
				acceptProcessProposal,
				fixture.validatorStore,
			)

			resp, err := handler.PrepareProposalHandler()(tc.ctx, tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectedErrIs != nil {
				require.ErrorIs(t, err, tc.expectedErrIs)
			}
			if tc.expectedCategory != nil {
				require.ErrorIs(t, err, tc.expectedCategory)
			}
			if tc.expectNilResponse {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedTxs, resp.Txs)
			}
			require.Equal(t, tc.expectedWrappedCalls, wrappedCalls)
			if tc.req != nil {
				require.Equal(t, originalMaxTxBytes, tc.req.MaxTxBytes)
				require.Equal(t, originalTxs, tc.req.Txs)
			}
		})
	}
}

func TestProcessProposalHandler(t *testing.T) {
	fixture := newProposalValidationFixture(t)
	initialCtx := abcitestutil.NewSDKContext(100, 1).
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, cometabci.CommitInfo{}))
	appTx := []byte("tx")
	commitInfo := fixture.commit
	commitBz := abcitestutil.MustEncodeExtendedCommit(t, commitInfo)
	invalidCommitInfo := commitInfo
	invalidCommitInfo.Votes = slices.Clone(commitInfo.Votes)
	invalidCommitInfo.Votes[0].ExtensionSignature = []byte("invalid-signature")
	invalidCommitBz := abcitestutil.MustEncodeExtendedCommit(t, invalidCommitInfo)
	excessVotesBz := bytes.Repeat([]byte{0x12, 0x00}, len(commitInfo.Votes)+1)

	testCases := []struct {
		name                 string
		ctx                  sdk.Context
		req                  *cometabci.RequestProcessProposal
		process              sdk.ProcessProposalHandler
		expectErr            bool
		expectNilResponse    bool
		expectedStatus       cometabci.ResponseProcessProposal_ProposalStatus
		expectedProcessTxs   [][]byte
		expectedFinalTxs     [][]byte
		expectedCategory     error
		expectedWrappedCalls int
	}{
		{
			name:              "nil request returns error",
			ctx:               fixture.ctx,
			req:               nil,
			process:           rejectUnexpectedProcessProposal(t),
			expectErr:         true,
			expectNilResponse: true,
			expectedCategory:  arkabci.ErrNilRequest,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cometabci.RequestProcessProposal{
				Height: 1,
				Txs:    [][]byte{appTx},
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "nonstandard initial height accepts app txs without commit info",
			ctx:  initialCtx,
			req: &cometabci.RequestProcessProposal{
				Height: 100,
				Txs:    [][]byte{appTx},
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled rejects missing injected commit info",
			ctx:  fixture.ctx,
			req: &cometabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    nil,
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: arkabci.ErrMissingCommitInfo,
			expectedStatus:   cometabci.ResponseProcessProposal_REJECT,
		},
		{
			name: "vote extensions enabled removes injected commit info before wrapped handler and restores it after",
			ctx:  fixture.ctx,
			req: &cometabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{commitBz, appTx},
			},
			process: func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cometabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{commitBz, appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "invalid vote extension signature rejects proposal before wrapped handler",
			ctx:  fixture.ctx,
			req: &cometabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{invalidCommitBz, appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: proposals.ErrExtendedCommitValidation,
			expectedStatus:   cometabci.ResponseProcessProposal_REJECT,
			expectedFinalTxs: [][]byte{
				invalidCommitBz,
				appTx,
			},
		},
		{
			name: "malformed commit info rejects proposal before wrapped handler",
			ctx:  fixture.ctx,
			req: &cometabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{[]byte("not-protobuf"), appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: arkabci.ErrCodec,
			expectedStatus:   cometabci.ResponseProcessProposal_REJECT,
		},
		{
			name: "excess commit votes reject proposal before protobuf unmarshal",
			ctx:  fixture.ctx,
			req: &cometabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{excessVotesBz, appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: arkabci.ErrCodec,
			expectedStatus:   cometabci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wrappedCalls := 0
			process := sdk.ProcessProposalHandler(func(ctx sdk.Context, req *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
				wrappedCalls++
				if tc.expectedProcessTxs != nil {
					require.Equal(t, tc.expectedProcessTxs, req.Txs)
				}
				return tc.process(ctx, req)
			})
			handler := proposals.NewHandler(
				passThroughPrepareProposal,
				process,
				fixture.validatorStore,
			)

			resp, err := handler.ProcessProposalHandler()(tc.ctx, tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectNilResponse {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedStatus, resp.Status)
			}
			if tc.expectedCategory != nil {
				require.ErrorIs(t, err, tc.expectedCategory)
			}
			if tc.req != nil && tc.expectedFinalTxs != nil {
				require.Equal(t, tc.expectedFinalTxs, tc.req.Txs)
			}
			require.Equal(t, tc.expectedWrappedCalls, wrappedCalls)
		})
	}
}

func newProposalValidationFixture(t *testing.T) proposalValidationFixture {
	t.Helper()

	const height = proposalTestHeight
	privateKey := cmtsecp256k1.GenPrivKey()
	consensusAddress := sdk.ConsAddress(privateKey.PubKey().Address())
	voteExtension := []byte("ve1")
	canonicalVoteExtension := cmtproto.CanonicalVoteExtension{
		Extension: voteExtension,
		Height:    height - 1,
		Round:     0,
		ChainId:   proposalTestChainID,
	}
	var signBytes bytes.Buffer
	require.NoError(t, protoio.NewDelimitedWriter(&signBytes).WriteMsg(&canonicalVoteExtension))
	extensionSignature, err := privateKey.Sign(signBytes.Bytes())
	require.NoError(t, err)

	vote := abcitestutil.NewCommitExtendedVoteInfo(consensusAddress, 1, voteExtension)
	vote.ExtensionSignature = extensionSignature
	commit := cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{vote},
	}
	lastCommit := cometabci.CommitInfo{
		Votes: []cometabci.VoteInfo{
			{
				Validator:   vote.Validator,
				BlockIdFlag: vote.BlockIdFlag,
			},
		},
	}
	ctx := abcitestutil.NewSDKContext(height, 1).
		WithHeaderInfo(header.Info{Height: height, ChainID: proposalTestChainID}).
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, lastCommit))
	publicKey := cmtprotocrypto.PublicKey{
		Sum: &cmtprotocrypto.PublicKey_Secp256K1{
			Secp256K1: privateKey.PubKey().Bytes(),
		},
	}

	return proposalValidationFixture{
		ctx:    ctx,
		commit: commit,
		validatorStore: testValidatorStore{
			string(consensusAddress): publicKey,
		},
	}
}

func passThroughPrepareProposal(_ sdk.Context, req *cometabci.RequestPrepareProposal) (*cometabci.ResponsePrepareProposal, error) {
	return &cometabci.ResponsePrepareProposal{Txs: req.Txs}, nil
}

func acceptProcessProposal(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
	return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_ACCEPT}, nil
}

func rejectUnexpectedProcessProposal(t *testing.T) sdk.ProcessProposalHandler {
	t.Helper()

	return func(_ sdk.Context, _ *cometabci.RequestProcessProposal) (*cometabci.ResponseProcessProposal, error) {
		require.Fail(t, "wrapped process handler should not be called")
		return &cometabci.ResponseProcessProposal{Status: cometabci.ResponseProcessProposal_REJECT}, nil
	}
}
