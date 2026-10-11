package proposals_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/core/header"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/proposals"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	abcitypes "github.com/ararat-network/ark/abci/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const (
	proposalTestChainID = "proposal-test-chain"
	proposalTestHeight  = int64(101)
)

type proposalValidationFixture struct {
	ctx            sdk.Context
	commit         cmtabci.ExtendedCommitInfo
	validatorStore testValidatorStore
}

type testValidatorStore map[string]cmtprotocrypto.PublicKey

// testOracleParams serves a fixed oracle parameter set, or err.
type testOracleParams struct {
	params oracletypes.Params
	err    error
}

func (p testOracleParams) GetParams(context.Context) (oracletypes.Params, error) {
	return p.params, p.err
}

var defaultOracleParams = testOracleParams{params: oracletypes.DefaultParams()}

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
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, cmtabci.CommitInfo{}))
	appTx1 := []byte("tx1")
	appTx2 := []byte("tx2")
	commitInfo := fixture.commit
	commitBz := abcitestutil.MustEncodeExtendedCommit(t, commitInfo)
	commitBzSize := cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{commitBz})
	maxTxBytes := int64(len(commitBz) + 100)
	largeAppTx := make([]byte, maxTxBytes-int64(len(commitBz)))
	prepareErr := errors.New("prepare proposal failed")
	now := time.Now()
	limit := time.Duration(oracletypes.DefaultParams().MaxExchangeRateAge) * time.Second
	invalidCommitInfo := commitInfo
	invalidCommitInfo.Votes = slices.Clone(commitInfo.Votes)
	invalidCommitInfo.Votes[0].ExtensionSignature = []byte("invalid-signature")
	oversizedCtx, oversizedCommit := oversizedCommitFixture(fixture)

	testCases := []struct {
		name                 string
		ctx                  sdk.Context
		req                  *cmtabci.RequestPrepareProposal
		oracleParams         abcitypes.OracleParamsReader
		prepare              sdk.PrepareProposalHandler
		expectErr            bool
		expectNilResponse    bool
		expectedTxs          [][]byte
		expectedCategory     error
		expectedErrIs        error
		expectedErrMsg       string
		expectedPrepareTxs   [][]byte
		expectedMaxTxBytes   int64
		expectedWrappedCalls int
		// at, when set, dates the request as the handler is called, so time the test spends
		// before the call cannot carry it across the freshness limit.
		at func() time.Time
	}{
		{
			name: "nil request returns error",
			ctx:  fixture.ctx,
			req:  nil,
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:         true,
			expectNilResponse: true,
			expectedCategory:  abcitypes.ErrNilRequest,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cmtabci.RequestPrepareProposal{
				Height:     1,
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions disabled returns wrapped handler error",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cmtabci.RequestPrepareProposal{
				Height:     1,
				Txs:        [][]byte{appTx1},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, _ *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return nil, prepareErr
			},
			expectErr:            true,
			expectedTxs:          [][]byte{},
			expectedCategory:     abcitypes.ErrWrappedHandler,
			expectedErrIs:        prepareErr,
			expectedPrepareTxs:   [][]byte{appTx1},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "nonstandard initial height passes app txs without commit info",
			ctx:  initialCtx,
			req: &cmtabci.RequestPrepareProposal{
				Height:     100,
				Txs:        [][]byte{appTx1, appTx2},
				MaxTxBytes: 100,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   100,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled injects encoded commit info",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1, appTx2},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{commitBz, appTx1, appTx2},
			expectedPrepareTxs:   [][]byte{appTx1, appTx2},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled falls back to commit-only proposal on wrapped handler error",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, _ *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
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
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{largeAppTx},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{}, nil
			},
			expectedTxs:          [][]byte{commitBz},
			expectedPrepareTxs:   [][]byte{largeAppTx},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "encoded commit info larger than max tx bytes returns error",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      commitBzSize - 1,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:      true,
			expectedTxs:    [][]byte{},
			expectedErrMsg: fmt.Sprintf("extended commit transaction size %d exceeds MaxTxBytes %d", commitBzSize, commitBzSize-1),
		},
		{
			name: "invalid local commit returns error before selection",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: invalidCommitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:        true,
			expectedTxs:      [][]byte{},
			expectedCategory: proposals.ErrExtendedCommitValidation,
			expectedErrMsg:   "failed to verify validator",
		},
		{
			name: "unencodable local commit returns error before selection",
			ctx:  oversizedCtx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: oversizedCommit,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectErr:        true,
			expectedTxs:      [][]byte{},
			expectedCategory: abcitypes.ErrCodec,
			expectedErrMsg:   fmt.Sprintf("extended commit vote count %d exceeds maximum %d", cmttypes.MaxVotesCount+1, cmttypes.MaxVotesCount),
		},
		{
			name: "prices past the freshness limit yield a commit-only proposal",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			at: func() time.Time { return time.Now().Add(-limit - 5*time.Second) },
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs: [][]byte{commitBz},
		},
		{
			name: "prices inside the freshness limit select transactions",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			at: func() time.Time { return time.Now().Add(-limit + 5*time.Second) },
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{commitBz, appTx1},
			expectedPrepareTxs:   [][]byte{appTx1},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "a clock behind the proposal reads fresh prices",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			at: func() time.Time { return time.Now().Add(time.Minute) },
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs:          [][]byte{commitBz, appTx1},
			expectedPrepareTxs:   [][]byte{appTx1},
			expectedMaxTxBytes:   maxTxBytes - commitBzSize,
			expectedWrappedCalls: 1,
		},
		{
			name: "unreadable oracle params yield a commit-only proposal",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestPrepareProposal{
				Height:          proposalTestHeight,
				Time:            now,
				LocalLastCommit: commitInfo,
				Txs:             [][]byte{appTx1},
				MaxTxBytes:      maxTxBytes,
			},
			oracleParams: testOracleParams{err: errors.New("params unavailable")},
			prepare: func(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				require.Fail(t, "wrapped prepare handler should not be called")
				return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
			},
			expectedTxs: [][]byte{commitBz},
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
			prepare := sdk.PrepareProposalHandler(func(ctx sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
				wrappedCalls++
				if tc.expectedPrepareTxs != nil {
					require.Equal(t, tc.expectedPrepareTxs, req.Txs)
				}
				if tc.expectedMaxTxBytes != 0 {
					require.Equal(t, tc.expectedMaxTxBytes, req.MaxTxBytes)
				}
				return tc.prepare(ctx, req)
			})

			oracleParams := tc.oracleParams
			if oracleParams == nil {
				oracleParams = defaultOracleParams
			}
			handler := proposals.NewHandler(
				prepare,
				acceptProcessProposal,
				fixture.validatorStore,
				oracleParams,
			)

			if tc.at != nil {
				tc.req.Time = tc.at()
			}
			resp, err := handler.PrepareProposalHandler()(tc.ctx, tc.req)
			if tc.expectErr {
				require.True(t, tc.expectedErrIs != nil || tc.expectedCategory != nil || tc.expectedErrMsg != "",
					"an error case names the error it expects")
			} else {
				require.NoError(t, err)
			}
			if tc.expectedErrIs != nil {
				require.ErrorIs(t, err, tc.expectedErrIs)
			}
			if tc.expectedCategory != nil {
				require.ErrorIs(t, err, tc.expectedCategory)
			}
			if tc.expectedErrMsg != "" {
				require.ErrorContains(t, err, tc.expectedErrMsg)
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
		WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, cmtabci.CommitInfo{}))
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
		req                  *cmtabci.RequestProcessProposal
		process              sdk.ProcessProposalHandler
		expectErr            bool
		expectNilResponse    bool
		expectedStatus       cmtabci.ResponseProcessProposal_ProposalStatus
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
			expectedCategory:  abcitypes.ErrNilRequest,
		},
		{
			name: "vote extensions disabled passes app txs through",
			ctx:  abcitestutil.NewSDKContext(1, 2),
			req: &cmtabci.RequestProcessProposal{
				Height: 1,
				Txs:    [][]byte{appTx},
			},
			process: func(_ sdk.Context, _ *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
				return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cmtabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "nonstandard initial height accepts app txs without commit info",
			ctx:  initialCtx,
			req: &cmtabci.RequestProcessProposal{
				Height: 100,
				Txs:    [][]byte{appTx},
			},
			process: func(_ sdk.Context, _ *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
				return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cmtabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "vote extensions enabled rejects missing injected commit info",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    nil,
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: abcitypes.ErrMissingCommitInfo,
			expectedStatus:   cmtabci.ResponseProcessProposal_REJECT,
		},
		{
			name: "vote extensions enabled removes injected commit info before wrapped handler and restores it after",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{commitBz, appTx},
			},
			process: func(_ sdk.Context, _ *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
				return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_ACCEPT}, nil
			},
			expectedStatus:       cmtabci.ResponseProcessProposal_ACCEPT,
			expectedProcessTxs:   [][]byte{appTx},
			expectedFinalTxs:     [][]byte{commitBz, appTx},
			expectedWrappedCalls: 1,
		},
		{
			name: "invalid vote extension signature rejects proposal before wrapped handler",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{invalidCommitBz, appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: proposals.ErrExtendedCommitValidation,
			expectedStatus:   cmtabci.ResponseProcessProposal_REJECT,
			expectedFinalTxs: [][]byte{
				invalidCommitBz,
				appTx,
			},
		},
		{
			name: "malformed commit info rejects proposal before wrapped handler",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{[]byte("not-protobuf"), appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: abcitypes.ErrCodec,
			expectedStatus:   cmtabci.ResponseProcessProposal_REJECT,
		},
		{
			name: "excess commit votes reject proposal before protobuf unmarshal",
			ctx:  fixture.ctx,
			req: &cmtabci.RequestProcessProposal{
				Height: proposalTestHeight,
				Txs:    [][]byte{excessVotesBz, appTx},
			},
			process:          rejectUnexpectedProcessProposal(t),
			expectErr:        true,
			expectedCategory: abcitypes.ErrCodec,
			expectedStatus:   cmtabci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wrappedCalls := 0
			process := sdk.ProcessProposalHandler(func(ctx sdk.Context, req *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
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
				defaultOracleParams,
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
	commit := cmtabci.ExtendedCommitInfo{
		Votes: []cmtabci.ExtendedVoteInfo{vote},
	}
	lastCommit := cmtabci.CommitInfo{
		Votes: []cmtabci.VoteInfo{
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

// oversizedCommitFixture extends the fixture's signed commit with absent votes until it holds one
// vote more than CometBFT allows: the commit still authenticates, but cannot be encoded.
func oversizedCommitFixture(fixture proposalValidationFixture) (sdk.Context, cmtabci.ExtendedCommitInfo) {
	signed := fixture.commit.Votes[0]
	// The signed vote leads the power order and carries quorum alone.
	signed.Validator.Power = 3 * cmttypes.MaxVotesCount
	commit := cmtabci.ExtendedCommitInfo{Votes: []cmtabci.ExtendedVoteInfo{signed}}
	for i := range cmttypes.MaxVotesCount {
		address := make([]byte, 20)
		binary.BigEndian.PutUint32(address[16:], uint32(i))
		commit.Votes = append(commit.Votes, cmtabci.ExtendedVoteInfo{
			Validator:   cmtabci.Validator{Address: address, Power: 1},
			BlockIdFlag: cmtproto.BlockIDFlagAbsent,
		})
	}
	lastCommit := cmtabci.CommitInfo{Votes: make([]cmtabci.VoteInfo, len(commit.Votes))}
	for i, vote := range commit.Votes {
		lastCommit.Votes[i] = cmtabci.VoteInfo{Validator: vote.Validator, BlockIdFlag: vote.BlockIdFlag}
	}

	return fixture.ctx.WithCometInfo(baseapp.NewBlockInfo(nil, nil, nil, lastCommit)), commit
}

func passThroughPrepareProposal(_ sdk.Context, req *cmtabci.RequestPrepareProposal) (*cmtabci.ResponsePrepareProposal, error) {
	return &cmtabci.ResponsePrepareProposal{Txs: req.Txs}, nil
}

func acceptProcessProposal(_ sdk.Context, _ *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
	return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_ACCEPT}, nil
}

func rejectUnexpectedProcessProposal(t *testing.T) sdk.ProcessProposalHandler {
	t.Helper()

	return func(_ sdk.Context, _ *cmtabci.RequestProcessProposal) (*cmtabci.ResponseProcessProposal, error) {
		require.Fail(t, "wrapped process handler should not be called")
		return &cmtabci.ResponseProcessProposal{Status: cmtabci.ResponseProcessProposal_REJECT}, nil
	}
}
