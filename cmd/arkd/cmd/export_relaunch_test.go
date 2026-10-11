package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/abci/codec"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// TestExportRelaunchBoots runs the relaunch an export exists for: a chain with vote extensions
// commits priced blocks and exports, the export command's continuation genesis takes the
// runbook's new chain ID and restart time, and the dry run boots it with its first block at the
// exported height.
func TestExportRelaunchBoots(t *testing.T) {
	const (
		chainID         = "ark-export-source"
		relaunchChainID = "ark-export-relaunch"
		blocks          = 5
	)
	validators := apptestutil.NewValidators(t, 1)
	funder := apptestutil.NewFunder(t, sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, chain.NativeBaseAmount(1_000_000))))
	source := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(chainID))
	t.Cleanup(func() { require.NoError(t, source.Close()) })

	genesisState, err := simtestutil.GenesisStateWithValSet(source.AppCodec(), source.DefaultGenesis(), validators.Set, funder.Accounts(), funder.Balance)
	require.NoError(t, err)
	apptestutil.SeedSigningInfos(t, source.AppCodec(), genesisState, validators.ConsAddresses(), 1)
	appState, err := json.Marshal(genesisState)
	require.NoError(t, err)
	params := cmttypes.DefaultConsensusParams()
	params.ABCI.VoteExtensionsEnableHeight = 1
	consensusParams := params.ToProto()
	genesisTime := time.Unix(1_700_000_000, 0).UTC()
	_, err = source.InitChain(&cmtabci.RequestInitChain{
		ChainId: chainID, Time: genesisTime, InitialHeight: 1, ConsensusParams: &consensusParams, AppStateBytes: appState,
	})
	require.NoError(t, err)
	base := &genutiltypes.AppGenesis{
		ChainID: chainID, GenesisTime: genesisTime, InitialHeight: 1, AppState: appState,
		Consensus: &genutiltypes.ConsensusGenesis{Params: params},
	}

	// From height two each block carries the validator's report on every launch feed, as the
	// proposal handler injects it.
	rates := make(map[string][]byte, len(oracletypes.DefaultFeedDenoms))
	for _, denom := range oracletypes.DefaultFeedDenoms {
		rates[denom], err = encoding.EncodeCompactLegacyDec(math.LegacyNewDecWithPrec(150, 2))
		require.NoError(t, err)
	}
	report, err := codec.EncodeVoteExtension(vetypes.OracleVoteExtension{TargetVersion: oracletypes.InitialFeedVersion, Rates: rates})
	require.NoError(t, err)
	validator := cmtabci.Validator{Address: validators.ConsAddress(0), Power: 1}
	blockTime := genesisTime
	for height := int64(1); height <= blocks; height++ {
		blockTime = genesisTime.Add(time.Duration(height) * time.Second)
		req := &cmtabci.RequestFinalizeBlock{Height: height, Time: blockTime, NextValidatorsHash: validators.Set.Hash()}
		if height > 1 {
			commit, err := codec.EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{Votes: []cmtabci.ExtendedVoteInfo{{
				Validator: validator, VoteExtension: report, BlockIdFlag: cmtproto.BlockIDFlagCommit,
			}}})
			require.NoError(t, err)
			req.Txs = [][]byte{commit}
			req.DecidedLastCommit = cmtabci.CommitInfo{Votes: []cmtabci.VoteInfo{{Validator: validator, BlockIdFlag: cmtproto.BlockIDFlagCommit}}}
		}
		_, err := source.FinalizeBlock(req)
		require.NoError(t, err)
		_, err = source.Commit()
		require.NoError(t, err)
	}
	stored, err := source.OracleKeeper.ExchangeRate.Get(source.NewContext(true), chain.USDBaseDenom)
	require.NoError(t, err)
	require.Equal(t, math.LegacyNewDecWithPrec(150, 2), stored.Rate, "the exported chain applied its reports")

	exported, err := source.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int64(blocks+1), exported.Height)

	tests := []struct {
		name        string
		genesisTime time.Time
		wantErr     string
	}{
		{name: "a restart time after the export boots", genesisTime: blockTime.Add(time.Hour)},
		{name: "the export's own genesis time predates its records", genesisTime: genesisTime, wantErr: "after genesis block time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := continuationGenesis(cloneAppGenesis(t, base), exported)
			genesis.ChainID = relaunchChainID
			genesis.GenesisTime = tc.genesisTime
			// The relaunch boots from the published file.
			file := filepath.Join(t.TempDir(), "genesis.json")
			require.NoError(t, genesis.SaveAs(file))
			loaded, err := genutiltypes.AppGenesisFromFile(file)
			require.NoError(t, err)
			doc, err := loaded.ToGenesisDoc()
			require.NoError(t, err)
			require.NoError(t, doc.ValidateAndComplete())

			relaunched := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(relaunchChainID))
			t.Cleanup(func() { require.NoError(t, relaunched.Close()) })
			err = bootGenesis(relaunched, doc)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, exported.Height, relaunched.LastBlockHeight(), "the first block is the exported height")
			ctx := relaunched.NewContext(true)
			require.Equal(t,
				source.BankKeeper.GetAllBalances(source.NewContext(true), funder.Address()),
				relaunched.BankKeeper.GetAllBalances(ctx, funder.Address()))
			relaunchedRate, err := relaunched.OracleKeeper.ExchangeRate.Get(ctx, chain.USDBaseDenom)
			require.NoError(t, err)
			require.Equal(t, stored, relaunchedRate)
			require.Equal(t, int64(1), relaunched.GetConsensusParams(ctx).Abci.VoteExtensionsEnableHeight)

			var out bytes.Buffer
			require.NoError(t, runDryRun(&out, file))
			require.Contains(t, out.String(), fmt.Sprintf("%s boots at height %d, %s; bonded validators: 1",
				relaunchChainID, exported.Height, tc.genesisTime.Format(time.RFC3339)))
		})
	}
}

// cloneAppGenesis copies a genesis through its JSON, as a second operator reading the same file would.
func cloneAppGenesis(t *testing.T, genesis *genutiltypes.AppGenesis) *genutiltypes.AppGenesis {
	t.Helper()
	bz, err := json.Marshal(genesis)
	require.NoError(t, err)
	clone, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(bz))
	require.NoError(t, err)
	return clone
}
