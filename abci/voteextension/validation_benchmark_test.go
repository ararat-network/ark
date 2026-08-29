package voteextension_test

import (
	"fmt"
	"testing"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	"github.com/ararat-network/ark/abci/voteextension"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	arkencoding "github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

type benchmarkKeyType struct {
	name          string
	newPrivateKey func() cmtcrypto.PrivKey
}

type benchmarkValidator struct {
	consAddr sdk.ConsAddress
	protoKey cmtprotocrypto.PublicKey
	privKey  cmtcrypto.PrivKey
}

func BenchmarkValidateExtendedCommit(b *testing.B) {
	const validatorCount = 100

	keyTypes := []benchmarkKeyType{
		{
			name: "ed25519",
			newPrivateKey: func() cmtcrypto.PrivKey {
				return cmted25519.GenPrivKey()
			},
		},
		{
			name: "secp256k1",
			newPrivateKey: func() cmtcrypto.PrivKey {
				return cmtsecp256k1.GenPrivKey()
			},
		},
	}
	targetCounts := []int{len(oracletypes.DefaultFeedDenoms), oracletypes.MaxFeeds}
	extensions := make(map[int][]byte, len(targetCounts))
	for _, targetCount := range targetCounts {
		extensions[targetCount] = benchmarkVoteExtension(b, targetCount)
	}

	for _, keyType := range keyTypes {
		b.Run(fmt.Sprintf("validators_%d/key_%s", validatorCount, keyType.name), func(b *testing.B) {
			for _, targetCount := range targetCounts {
				b.Run(fmt.Sprintf("targets_%d", targetCount), func(b *testing.B) {
					extension := extensions[targetCount]
					ctx, store, commit := benchmarkExtendedCommit(
						b,
						validatorCount,
						keyType.newPrivateKey,
						extension,
					)
					if err := voteextension.ValidateExtendedCommit(ctx, store, commit); err != nil {
						b.Fatal(err)
					}

					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						if err := voteextension.ValidateExtendedCommit(ctx, store, commit); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(len(extension)), "extension_B/validator")
				})
			}
		})
	}
}

func benchmarkVoteExtension(b *testing.B, targetCount int) []byte {
	b.Helper()

	rates := make(map[string][]byte, targetCount)
	for i := range targetCount {
		rate, err := arkencoding.EncodeCompactLegacyDec(math.LegacyNewDec(int64(i + 1)))
		if err != nil {
			b.Fatal(err)
		}
		rates[fmt.Sprintf("uasset%03d", i)] = rate
	}

	extension, err := codec.EncodeVoteExtension(vetypes.OracleVoteExtension{
		Rates:         rates,
		TargetVersion: oracletypes.InitialFeedVersion,
	})
	if err != nil {
		b.Fatal(err)
	}

	return extension
}

func benchmarkExtendedCommit(
	b *testing.B,
	validatorCount int,
	newPrivateKey func() cmtcrypto.PrivKey,
	extension []byte,
) (sdk.Context, fakeValidatorStore, cometabci.ExtendedCommitInfo) {
	b.Helper()

	ctx := newVoteExtensionContext(3, 1)
	store := fakeValidatorStore{
		pubKeys: make(map[string]cmtprotocrypto.PublicKey, validatorCount),
		errs:    make(map[string]error),
	}
	commit := cometabci.ExtendedCommitInfo{
		Votes: make([]cometabci.ExtendedVoteInfo, validatorCount),
	}

	for i := range validatorCount {
		validator := newBenchmarkValidator(b, newPrivateKey())
		store.pubKeys[string(validator.consAddr)] = validator.protoKey
		commit.Votes[i] = cometabci.ExtendedVoteInfo{
			Validator: cometabci.Validator{
				Address: validator.consAddr,
				Power:   1,
			},
			VoteExtension:      extension,
			ExtensionSignature: signBenchmarkVoteExtension(b, validator, extension, ctx.HeaderInfo().Height-1, commit.Round),
			BlockIdFlag:        cmtproto.BlockIDFlagCommit,
		}
	}

	commit, info := extendedCommitToBlockInfo(commit)
	return ctx.WithCometInfo(info), store, commit
}

func newBenchmarkValidator(b *testing.B, privateKey cmtcrypto.PrivKey) benchmarkValidator {
	b.Helper()

	publicKey := privateKey.PubKey()
	protoKey, err := cryptoenc.PubKeyToProto(publicKey)
	if err != nil {
		b.Fatal(err)
	}

	return benchmarkValidator{
		consAddr: sdk.ConsAddress(publicKey.Address()),
		protoKey: protoKey,
		privKey:  privateKey,
	}
}

func signBenchmarkVoteExtension(
	b *testing.B,
	validator benchmarkValidator,
	extension []byte,
	height int64,
	round int32,
) []byte {
	b.Helper()

	canonical := cmtproto.CanonicalVoteExtension{
		Extension: extension,
		Height:    height,
		Round:     int64(round),
		ChainId:   testChainID,
	}
	signBytes, err := marshalDelimited(&canonical)
	if err != nil {
		b.Fatal(err)
	}
	signature, err := validator.privKey.Sign(signBytes)
	if err != nil {
		b.Fatal(err)
	}

	return signature
}
