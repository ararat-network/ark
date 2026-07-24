package app

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	protoio "github.com/cosmos/gogoproto/io"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdksecp256k1 "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"

	"ark/abci/codec"
	vetypes "ark/abci/voteextension/types"
	oracletypes "ark/x/oracle/types"
)

func TestProposalHandlersAtNonstandardInitialHeight(t *testing.T) {
	const (
		chainID       = "ark-initial-height-test"
		initialHeight = int64(100)
	)

	validatorKey := cmtsecp256k1.GenPrivKey()
	validatorSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{
		cmttypes.NewValidator(validatorKey.PubKey(), 1),
	})
	accountKey := sdksecp256k1.GenPrivKey()
	account := authtypes.NewBaseAccount(accountKey.PubKey().Address().Bytes(), accountKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: account.GetAddress().String(),
		Coins: sdk.NewCoins(sdk.NewCoin(
			sdk.DefaultBondDenom,
			sdk.DefaultPowerReduction,
		)),
	}

	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	t.Cleanup(func() {
		require.NoError(t, arkApp.Close())
	})

	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		validatorSet,
		[]authtypes.GenesisAccount{account},
		balance,
	)
	require.NoError(t, err)
	consensusAddress := sdk.ConsAddress(validatorKey.PubKey().Address())
	var slashingGenesis slashingtypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(genesisState[slashingtypes.ModuleName], &slashingGenesis)
	slashingGenesis.SigningInfos = []slashingtypes.SigningInfo{{
		Address: consensusAddress.String(),
		ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
			consensusAddress,
			initialHeight,
			0,
			time.Unix(0, 0).UTC(),
			false,
			0,
		),
	}}
	genesisState[slashingtypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&slashingGenesis)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cmtproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = 1

	_, err = arkApp.InitChain(&cometabci.RequestInitChain{
		ChainId:         chainID,
		InitialHeight:   initialHeight,
		Time:            time.Unix(0, 0).UTC(),
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	prepareInitial, err := arkApp.PrepareProposal(&cometabci.RequestPrepareProposal{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		MaxTxBytes:         1_000_000,
		NextValidatorsHash: validatorSet.Hash(),
	})
	require.NoError(t, err)
	require.Empty(t, prepareInitial.Txs)

	processInitial, err := arkApp.ProcessProposal(&cometabci.RequestProcessProposal{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		Txs:                prepareInitial.Txs,
		NextValidatorsHash: validatorSet.Hash(),
	})
	require.NoError(t, err)
	require.Equal(t, cometabci.ResponseProcessProposal_ACCEPT, processInitial.Status)

	_, err = arkApp.FinalizeBlock(&cometabci.RequestFinalizeBlock{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		NextValidatorsHash: validatorSet.Hash(),
	})
	require.NoError(t, err)
	_, err = arkApp.Commit()
	require.NoError(t, err)

	voteExtension, err := codec.NewVoteExtensionCodec().Encode(vetypes.OracleVoteExtension{
		TargetVersion: oracletypes.InitialVoteTargetVersion,
	})
	require.NoError(t, err)
	canonicalVoteExtension := cmtproto.CanonicalVoteExtension{
		Extension: voteExtension,
		Height:    initialHeight,
		Round:     0,
		ChainId:   chainID,
	}
	var signBytes bytes.Buffer
	require.NoError(t, protoio.NewDelimitedWriter(&signBytes).WriteMsg(&canonicalVoteExtension))
	extensionSignature, err := validatorKey.Sign(signBytes.Bytes())
	require.NoError(t, err)

	validator := cometabci.Validator{
		Address: validatorKey.PubKey().Address(),
		Power:   1,
	}
	extendedCommit := cometabci.ExtendedCommitInfo{
		Votes: []cometabci.ExtendedVoteInfo{{
			Validator:          validator,
			VoteExtension:      voteExtension,
			ExtensionSignature: extensionSignature,
			BlockIdFlag:        cmtproto.BlockIDFlagCommit,
		}},
	}
	lastCommit := cometabci.CommitInfo{
		Votes: []cometabci.VoteInfo{{
			Validator:   validator,
			BlockIdFlag: cmtproto.BlockIDFlagCommit,
		}},
	}

	prepareNext, err := arkApp.PrepareProposal(&cometabci.RequestPrepareProposal{
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		MaxTxBytes:         1_000_000,
		LocalLastCommit:    extendedCommit,
		NextValidatorsHash: validatorSet.Hash(),
	})
	require.NoError(t, err)
	require.Len(t, prepareNext.Txs, 1)

	blockHash := []byte("height-101")
	processNextRequest := &cometabci.RequestProcessProposal{
		Hash:               blockHash,
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		Txs:                prepareNext.Txs,
		ProposedLastCommit: lastCommit,
		NextValidatorsHash: validatorSet.Hash(),
	}
	processNext, err := arkApp.ProcessProposal(processNextRequest)
	require.NoError(t, err)
	require.Equal(t, cometabci.ResponseProcessProposal_ACCEPT, processNext.Status)

	_, err = arkApp.FinalizeBlock(&cometabci.RequestFinalizeBlock{
		Hash:               blockHash,
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		Txs:                prepareNext.Txs,
		DecidedLastCommit:  lastCommit,
		NextValidatorsHash: validatorSet.Hash(),
	})
	require.NoError(t, err)
}
