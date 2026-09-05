package app_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	protoio "github.com/cosmos/gogoproto/io"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestProposalHandlersAtNonstandardInitialHeight(t *testing.T) {
	const (
		chainID       = "ark-initial-height-test"
		initialHeight = int64(100)
	)

	validators := apptestutil.NewValidators(t, 1)
	funder := apptestutil.NewFunder(t, sdk.NewCoins(sdk.NewCoin(
		sdk.DefaultBondDenom,
		sdk.DefaultPowerReduction,
	)))

	arkApp := app.NewArkApp(
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
		validators.Set,
		funder.Accounts(),
		funder.Balance,
	)
	require.NoError(t, err)
	apptestutil.SeedSigningInfos(t, arkApp.AppCodec(), genesisState, validators.ConsAddresses(), initialHeight)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	if consensusParams.Abci == nil {
		consensusParams.Abci = &cmtproto.ABCIParams{}
	}
	consensusParams.Abci.VoteExtensionsEnableHeight = 1

	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         chainID,
		InitialHeight:   initialHeight,
		Time:            time.Unix(0, 0).UTC(),
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	prepareInitial, err := arkApp.PrepareProposal(&cmtabci.RequestPrepareProposal{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		MaxTxBytes:         1_000_000,
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(t, err)
	require.Empty(t, prepareInitial.Txs)

	processInitial, err := arkApp.ProcessProposal(&cmtabci.RequestProcessProposal{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		Txs:                prepareInitial.Txs,
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(t, err)
	require.Equal(t, cmtabci.ResponseProcessProposal_ACCEPT, processInitial.Status)

	_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             initialHeight,
		Time:               time.Unix(1, 0).UTC(),
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(t, err)
	_, err = arkApp.Commit()
	require.NoError(t, err)

	voteExtension, err := codec.EncodeVoteExtension(vetypes.OracleVoteExtension{
		TargetVersion: oracletypes.InitialFeedVersion,
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
	extensionSignature, err := validators.Keys[0].Sign(signBytes.Bytes())
	require.NoError(t, err)

	validator := cmtabci.Validator{
		Address: validators.Keys[0].PubKey().Address(),
		Power:   1,
	}
	extendedCommit := cmtabci.ExtendedCommitInfo{
		Votes: []cmtabci.ExtendedVoteInfo{{
			Validator:          validator,
			VoteExtension:      voteExtension,
			ExtensionSignature: extensionSignature,
			BlockIdFlag:        cmtproto.BlockIDFlagCommit,
		}},
	}
	lastCommit := cmtabci.CommitInfo{
		Votes: []cmtabci.VoteInfo{{
			Validator:   validator,
			BlockIdFlag: cmtproto.BlockIDFlagCommit,
		}},
	}

	prepareNext, err := arkApp.PrepareProposal(&cmtabci.RequestPrepareProposal{
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		MaxTxBytes:         1_000_000,
		LocalLastCommit:    extendedCommit,
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(t, err)
	require.Len(t, prepareNext.Txs, 1)

	blockHash := []byte("height-101")
	processNextRequest := &cmtabci.RequestProcessProposal{
		Hash:               blockHash,
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		Txs:                prepareNext.Txs,
		ProposedLastCommit: lastCommit,
		NextValidatorsHash: validators.Set.Hash(),
	}
	processNext, err := arkApp.ProcessProposal(processNextRequest)
	require.NoError(t, err)
	require.Equal(t, cmtabci.ResponseProcessProposal_ACCEPT, processNext.Status)

	_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Hash:               blockHash,
		Height:             initialHeight + 1,
		Time:               time.Unix(2, 0).UTC(),
		Txs:                prepareNext.Txs,
		DecidedLastCommit:  lastCommit,
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(t, err)
}
