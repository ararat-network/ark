package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// TestTestnetInitFilesFromArtefact generates a two-validator testnet from the
// testnet artefact through the root command and reads back what the localnet
// relies on: two seats granted from the pool, the artefact's consensus block
// kept through collection, and gentxs that boot the generated genesis with
// both seats bonded at the full grant.
func TestTestnetInitFilesFromArtefact(t *testing.T) {
	const chainID = "ark-test"
	home := t.TempDir()
	output := filepath.Join(home, "testnet")
	artefact := filepath.Join("..", "..", "..", "app", "genesis", "testnet.json")

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{
		"testnet", "init-files",
		fmt.Sprintf("--%s=%s", flagGenesis, artefact),
		fmt.Sprintf("--%s=2", flagNumValidators),
		fmt.Sprintf("--%s=%s", flagOutputDir, output),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, chainID),
		fmt.Sprintf("--%s=test", flags.FlagKeyringBackend),
		fmt.Sprintf("--%s", flagSingleHost),
		fmt.Sprintf("--%s=%s", flags.FlagHome, home),
	})
	require.NoError(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome))

	cdc := clientCodec(t)
	appGenesis, err := genutiltypes.AppGenesisFromFile(filepath.Join(output, "node0", "arkd", "config", "genesis.json"))
	require.NoError(t, err)
	require.Equal(t, chainID, appGenesis.ChainID)
	require.EqualValues(t, chain.BlocksPerDay, appGenesis.Consensus.Params.Evidence.MaxAgeNumBlocks,
		"the artefact's consensus block survives collection")
	require.EqualValues(t, 100_000_000, appGenesis.Consensus.Params.Block.MaxGas)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), appGenesis.Consensus.Params.Authority.Authority)
	require.EqualValues(t, 1, appGenesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)

	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))

	var authState authtypes.GenesisState
	cdc.MustUnmarshalJSON(state[authtypes.ModuleName], &authState)
	accounts, err := authtypes.UnpackAccounts(authState.Accounts)
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	grant := chain.NativeBaseAmount(chain.SeatGrantNoah)
	for _, account := range accounts {
		locked, ok := account.(*vestingtypes.PermanentLockedAccount)
		require.True(t, ok, "every validator is a locked seat")
		require.Equal(t, grant, locked.OriginalVesting.AmountOf(chain.NoahBaseDenom))
	}

	bankState := banktypes.GetGenesisStateFromAppState(cdc, state)
	seat := chain.NativeBaseAmount(chain.SeatGrantNoah + chain.SeatFloatNoah)
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	for _, balance := range bankState.Balances {
		if balance.Address == pool {
			require.Equal(t, chain.NativeBaseAmount(835_000_000).Sub(seat.MulRaw(2)), balance.Coins.AmountOf(chain.NoahBaseDenom))
		}
	}
	require.Equal(t, chain.NativeBaseAmount(1_000_000_000), bankState.Supply.AmountOf(chain.NoahBaseDenom), "supply is unchanged")

	var treasuryState treasurytypes.GenesisState
	cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryState)
	require.Equal(t, chain.SeatValidatorShare.MulRaw(2), treasuryState.EconomicPolicy.ValidatorBlockRewardTarget)
	require.Equal(t, chain.SeatOracleShare.MulRaw(2), treasuryState.EconomicPolicy.OracleBlockRewardTarget)

	genutilState := genutiltypes.GetGenesisStateFromAppState(cdc, state)
	require.Len(t, genutilState.GenTxs, 2)

	// Boot what was written: the gentxs self-delegate the grants at InitChain.
	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	consensusParams := appGenesis.Consensus.Params.ToProto()
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         chainID,
		Validators:      []cmtabci.ValidatorUpdate{},
		ConsensusParams: &consensusParams,
		AppStateBytes:   appGenesis.AppState,
	})
	require.NoError(t, err)
	_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height: arkApp.LastBlockHeight() + 1,
		Hash:   arkApp.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = arkApp.Commit()
	require.NoError(t, err)

	ctx := arkApp.NewContext(true)
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Len(t, validators, 2)
	for _, validator := range validators {
		require.True(t, validator.IsBonded(), validator.OperatorAddress)
		require.Equal(t, grant, validator.Tokens)
	}
}
