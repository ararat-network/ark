package ve_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/core/comet"
	"cosmossdk.io/core/header"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	abcitestutil "ark/abci/testutil"
	"ark/abci/ve"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

const testChainID = "test-chain"

type testValidator struct {
	consAddr sdk.ConsAddress
	protoKey cmtprotocrypto.PublicKey
	privKey  cmtsecp256k1.PrivKey
}

type fakeValidatorStore struct {
	pubKeys map[string]cmtprotocrypto.PublicKey
	errs    map[string]error
}

func TestValidateOracleVoteExtension(t *testing.T) {
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
			err := ve.ValidateOracleVoteExtension(sdk.Context{}, tc.voteExt)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVoteExtensionsEnabled(t *testing.T) {
	testCases := []struct {
		name         string
		height       int64
		enableHeight int64
		withAbci     bool
		expected     bool
	}{
		{
			name:     "missing ABCI params",
			height:   3,
			expected: false,
		},
		{
			name:         "zero enable height",
			height:       3,
			enableHeight: 0,
			withAbci:     true,
			expected:     false,
		},
		{
			name:         "first block is disabled",
			height:       1,
			enableHeight: 0,
			withAbci:     true,
			expected:     false,
		},
		{
			name:         "enable height equal current height is disabled",
			height:       2,
			enableHeight: 2,
			withAbci:     true,
			expected:     false,
		},
		{
			name:         "enable height below current height is enabled",
			height:       3,
			enableHeight: 2,
			withAbci:     true,
			expected:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithBlockHeight(tc.height)
			params := cmtproto.ConsensusParams{}
			if tc.withAbci {
				params.Abci = &cmtproto.ABCIParams{VoteExtensionsEnableHeight: tc.enableHeight}
			}
			ctx = ctx.WithConsensusParams(params)

			require.Equal(t, tc.expected, ve.VoteExtensionsEnabled(ctx))
		})
	}
}

func TestValidateExtendedCommitAgainstLastCommit(t *testing.T) {
	vals := []testValidator{newTestValidator(), newTestValidator(), newTestValidator()}
	validCommit := sortExtendedCommit(cometabci.ExtendedCommitInfo{
		Round: 1,
		Votes: []cometabci.ExtendedVoteInfo{
			newExtendedVote(vals[0], 30, cmtproto.BlockIDFlagCommit, []byte("ve"), []byte("sig")),
			newExtendedVote(vals[1], 20, cmtproto.BlockIDFlagCommit, []byte("ve"), []byte("sig")),
			newExtendedVote(vals[2], 10, cmtproto.BlockIDFlagAbsent, nil, nil),
		},
	})
	validLastCommit := lastCommitFromExtendedCommit(validCommit)
	prunedCommit := cloneExtendedCommit(validCommit)
	prunedCommit.Votes[1].BlockIdFlag = cmtproto.BlockIDFlagAbsent
	prunedCommit.Votes[1].VoteExtension = nil
	prunedCommit.Votes[1].ExtensionSignature = nil

	testCases := []struct {
		name      string
		extCommit cometabci.ExtendedCommitInfo
		last      comet.CommitInfo
		expectErr bool
	}{
		{
			name:      "valid commit",
			extCommit: validCommit,
			last:      validLastCommit,
		},
		{
			name:      "round mismatch",
			extCommit: validCommit,
			last: lastCommitFromExtendedCommit(cometabci.ExtendedCommitInfo{
				Round: 2,
				Votes: validCommit.Votes,
			}),
			expectErr: true,
		},
		{
			name:      "length mismatch",
			extCommit: validCommit,
			last: lastCommitFromExtendedCommit(cometabci.ExtendedCommitInfo{
				Round: validCommit.Round,
				Votes: validCommit.Votes[:2],
			}),
			expectErr: true,
		},
		{
			name: "duplicate validator address",
			extCommit: cometabci.ExtendedCommitInfo{
				Round: validCommit.Round,
				Votes: []cometabci.ExtendedVoteInfo{
					validCommit.Votes[0],
					validCommit.Votes[0],
					validCommit.Votes[2],
				},
			},
			last:      validLastCommit,
			expectErr: true,
		},
		{
			name: "incorrect order",
			extCommit: cometabci.ExtendedCommitInfo{
				Round: validCommit.Round,
				Votes: []cometabci.ExtendedVoteInfo{
					validCommit.Votes[1],
					validCommit.Votes[0],
					validCommit.Votes[2],
				},
			},
			last:      validLastCommit,
			expectErr: true,
		},
		{
			name: "power mismatch",
			extCommit: func() cometabci.ExtendedCommitInfo {
				extCommit := cloneExtendedCommit(validCommit)
				extCommit.Votes[0].Validator.Power++
				return extCommit
			}(),
			last:      validLastCommit,
			expectErr: true,
		},
		{
			name: "address mismatch",
			extCommit: func() cometabci.ExtendedCommitInfo {
				extCommit := cloneExtendedCommit(validCommit)
				extCommit.Votes[0].Validator.Address = vals[2].consAddr
				return extCommit
			}(),
			last:      validLastCommit,
			expectErr: true,
		},
		{
			name:      "pruned absent vote may clear extension and signature",
			extCommit: prunedCommit,
			last:      validLastCommit,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ve.ValidateExtendedCommitAgainstLastCommit(tc.extCommit, tc.last)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateVoteExtensions(t *testing.T) {
	vals := []testValidator{newTestValidator(), newTestValidator(), newTestValidator()}
	ext := []byte("vote-extension")
	enabledCtx := newVoteExtensionContext(3, 1)
	disabledCtx := newVoteExtensionContext(2, 2)
	validCommit := signedExtendedCommit(t, enabledCtx, vals, []int64{30, 20, 10}, ext)
	validCommit, validInfo := extendedCommitToBlockInfo(validCommit)
	enabledCtx = enabledCtx.WithCometInfo(validInfo)
	validStore := fakeValidatorStoreFrom(vals)

	testCases := []struct {
		name      string
		ctx       sdk.Context
		store     fakeValidatorStore
		commit    cometabci.ExtendedCommitInfo
		expectErr bool
	}{
		{
			name:   "happy path verifies signatures",
			ctx:    enabledCtx,
			store:  validStore,
			commit: validCommit,
		},
		{
			name:  "disabled vote extensions reject present extension",
			ctx:   disabledCtx.WithCometInfo(validInfo),
			store: validStore,
			commit: func() cometabci.ExtendedCommitInfo {
				commit := cloneExtendedCommit(validCommit)
				commit.Votes[0].ExtensionSignature = nil
				return commit
			}(),
			expectErr: true,
		},
		{
			name:  "enabled commit vote missing signature rejects",
			ctx:   enabledCtx,
			store: validStore,
			commit: func() cometabci.ExtendedCommitInfo {
				commit := cloneExtendedCommit(validCommit)
				commit.Votes[0].ExtensionSignature = nil
				return commit
			}(),
			expectErr: true,
		},
		{
			name:  "non-commit vote with extension rejects",
			ctx:   enabledCtx,
			store: validStore,
			commit: func() cometabci.ExtendedCommitInfo {
				commit := cloneExtendedCommit(validCommit)
				commit.Votes[0].BlockIdFlag = cmtproto.BlockIDFlagAbsent
				return commit
			}(),
			expectErr: true,
		},
		{
			name:  "insufficient signed voting power rejects",
			ctx:   enabledCtx,
			store: validStore,
			commit: func() cometabci.ExtendedCommitInfo {
				commit := cloneExtendedCommit(validCommit)
				commit.Votes[0].BlockIdFlag = cmtproto.BlockIDFlagAbsent
				commit.Votes[0].VoteExtension = nil
				commit.Votes[0].ExtensionSignature = nil
				commit.Votes[1].BlockIdFlag = cmtproto.BlockIDFlagAbsent
				commit.Votes[1].VoteExtension = nil
				commit.Votes[1].ExtensionSignature = nil
				return commit
			}(),
			expectErr: true,
		},
		{
			name: "missing validator pubkey is skipped but voting power still counts",
			ctx:  enabledCtx,
			store: func() fakeValidatorStore {
				store := fakeValidatorStoreFrom(vals)
				delete(store.pubKeys, string(vals[1].consAddr))
				store.errs[string(vals[1].consAddr)] = errors.New("validator not found")
				return store
			}(),
			commit: validCommit,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ve.ValidateVoteExtensions(tc.ctx, tc.store, tc.commit)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newTestValidator() testValidator {
	privKey := cmtsecp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	protoKey := cmtprotocrypto.PublicKey{
		Sum: &cmtprotocrypto.PublicKey_Secp256K1{
			Secp256K1: pubKey.Bytes(),
		},
	}

	return testValidator{
		consAddr: sdk.ConsAddress(pubKey.Address()),
		protoKey: protoKey,
		privKey:  privKey,
	}
}

func (v testValidator) toCometValidator(power int64) cometabci.Validator {
	return cometabci.Validator{
		Address: v.consAddr,
		Power:   power,
	}
}

func (s fakeValidatorStore) GetPubKeyByConsAddr(_ context.Context, consAddr sdk.ConsAddress) (cmtprotocrypto.PublicKey, error) {
	if err, ok := s.errs[string(consAddr)]; ok {
		return cmtprotocrypto.PublicKey{}, err
	}
	pubKey, ok := s.pubKeys[string(consAddr)]
	if !ok {
		return cmtprotocrypto.PublicKey{}, errors.New("validator not found")
	}

	return pubKey, nil
}

func fakeValidatorStoreFrom(vals []testValidator) fakeValidatorStore {
	store := fakeValidatorStore{
		pubKeys: make(map[string]cmtprotocrypto.PublicKey, len(vals)),
		errs:    make(map[string]error),
	}
	for _, val := range vals {
		store.pubKeys[string(val.consAddr)] = val.protoKey
	}

	return store
}

func newVoteExtensionContext(height int64, enableHeight int64) sdk.Context {
	return sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(height).
		WithHeaderInfo(header.Info{Height: height, ChainID: testChainID}).
		WithConsensusParams(cmtproto.ConsensusParams{
			Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: enableHeight},
		})
}

func signedExtendedCommit(
	t *testing.T,
	ctx sdk.Context,
	vals []testValidator,
	powers []int64,
	extension []byte,
) cometabci.ExtendedCommitInfo {
	t.Helper()

	require.Len(t, vals, len(powers))
	round := int32(0)
	commit := cometabci.ExtendedCommitInfo{
		Round: round,
		Votes: make([]cometabci.ExtendedVoteInfo, len(vals)),
	}
	for i, val := range vals {
		signature := signVoteExtension(t, val, extension, ctx.HeaderInfo().Height-1, round)
		commit.Votes[i] = newExtendedVote(val, powers[i], cmtproto.BlockIDFlagCommit, extension, signature)
	}

	return commit
}

func signVoteExtension(t *testing.T, val testValidator, extension []byte, height int64, round int32) []byte {
	t.Helper()

	canonical := cmtproto.CanonicalVoteExtension{
		Extension: extension,
		Height:    height,
		Round:     int64(round),
		ChainId:   testChainID,
	}
	signBytes, err := marshalDelimited(&canonical)
	require.NoError(t, err)

	signature, err := val.privKey.Sign(signBytes)
	require.NoError(t, err)

	return signature
}

func newExtendedVote(
	val testValidator,
	power int64,
	blockIDFlag cmtproto.BlockIDFlag,
	extension []byte,
	signature []byte,
) cometabci.ExtendedVoteInfo {
	return cometabci.ExtendedVoteInfo{
		Validator:          val.toCometValidator(power),
		VoteExtension:      extension,
		ExtensionSignature: signature,
		BlockIdFlag:        blockIDFlag,
	}
}

func extendedCommitToBlockInfo(commit cometabci.ExtendedCommitInfo) (cometabci.ExtendedCommitInfo, comet.BlockInfo) {
	commit = sortExtendedCommit(commit)
	lastCommit := cometabci.CommitInfo{
		Round: commit.Round,
		Votes: make([]cometabci.VoteInfo, len(commit.Votes)),
	}
	for i, vote := range commit.Votes {
		lastCommit.Votes[i] = cometabci.VoteInfo{
			Validator: cometabci.Validator{
				Address: vote.Validator.Address,
				Power:   vote.Validator.Power,
			},
			BlockIdFlag: vote.BlockIdFlag,
		}
	}

	return commit, baseapp.NewBlockInfo(nil, nil, nil, lastCommit)
}

func sortExtendedCommit(commit cometabci.ExtendedCommitInfo) cometabci.ExtendedCommitInfo {
	sort.Slice(commit.Votes, func(i, j int) bool {
		if commit.Votes[i].Validator.Power == commit.Votes[j].Validator.Power {
			return bytes.Compare(commit.Votes[i].Validator.Address, commit.Votes[j].Validator.Address) < 0
		}
		return commit.Votes[i].Validator.Power > commit.Votes[j].Validator.Power
	})

	return commit
}

func cloneExtendedCommit(commit cometabci.ExtendedCommitInfo) cometabci.ExtendedCommitInfo {
	clone := commit
	clone.Votes = make([]cometabci.ExtendedVoteInfo, len(commit.Votes))
	copy(clone.Votes, commit.Votes)

	return clone
}

func lastCommitFromExtendedCommit(commit cometabci.ExtendedCommitInfo) comet.CommitInfo {
	_, blockInfo := extendedCommitToBlockInfo(commit)
	return blockInfo.GetLastCommit()
}

func makeRateMap(t *testing.T, count int) map[string][]byte {
	t.Helper()

	rates := make(map[string][]byte, count)
	for i := range count {
		rates[fmt.Sprintf("u%03d", i)] = abcitestutil.MustEncodeRate(t, math.LegacyNewDec(int64(i+1)))
	}

	return rates
}

func marshalDelimited(msg proto.Message) ([]byte, error) {
	var buf bytes.Buffer
	if err := protoio.NewDelimitedWriter(&buf).WriteMsg(msg); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
