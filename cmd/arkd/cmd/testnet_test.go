package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltest "github.com/cosmos/cosmos-sdk/x/genutil/client/testutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

func Test_TestnetCmd(t *testing.T) {
	const chainID = "ark-test"

	moduleBasic := module.NewBasicManager(
		auth.AppModuleBasic{},
		genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
		bank.AppModuleBasic{},
		staking.AppModuleBasic{},
		distribution.AppModuleBasic{},
		consensus.AppModuleBasic{},
	)

	home := t.TempDir()
	encodingConfig := moduletestutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, staking.AppModuleBasic{})
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultCometConfig(home)
	require.NoError(t, err)

	err = genutiltest.ExecInitCmd(moduleBasic, home, encodingConfig.Codec)
	require.NoError(t, err)

	serverCtx := server.NewContext(viper.New(), cfg, logger)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithHomeDir(home).
		WithTxConfig(encodingConfig.TxConfig)

	ctx := context.Background()
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	cmd := testnetInitFilesCmd(moduleBasic, banktypes.GenesisBalancesIterator{})
	require.Equal(t, "6000000anoah", cmd.Flags().Lookup(server.FlagMinGasPrices).DefValue)
	cmd.SetArgs([]string{
		fmt.Sprintf("--%s=test", flags.FlagKeyringBackend),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, chainID),
		fmt.Sprintf("--%s=%s", flagOutputDir, home),
		fmt.Sprintf("--%s", flagSingleHost),
	})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)

	node0ConfigDir := filepath.Join(home, "node0", "simd", "config")
	appConfig, err := os.ReadFile(filepath.Join(node0ConfigDir, "app.toml"))
	require.NoError(t, err)
	require.Contains(t, string(appConfig), `metrics-sink = "otel"`)
	require.Contains(t, string(appConfig), "prometheus-retention-time = 0")

	node0Otel, err := os.ReadFile(filepath.Join(node0ConfigDir, "otel.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(node0Otel), `value: "ark-test/node0"`)
	require.Contains(t, string(node0Otel), `value: "ark-test"`)
	require.Contains(t, string(node0Otel), "port: 9464")

	node1Otel, err := os.ReadFile(filepath.Join(home, "node1", "simd", "config", "otel.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(node1Otel), "port: 9465")

	expectedAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	for i := range 2 {
		genesis, err := genutiltypes.AppGenesisFromFile(
			filepath.Join(home, fmt.Sprintf("node%d", i), "simd", "config", "genesis.json"),
		)
		require.NoError(t, err)
		require.Equal(t, expectedAuthority, genesis.Consensus.Params.Authority.Authority)
	}

	genFile := cfg.GenesisFile()
	appState, _, err := genutiltypes.GenesisStateFromGenFile(genFile)
	require.NoError(t, err)
	require.NotContains(t, appState, "mint")

	bankGenState := banktypes.GetGenesisStateFromAppState(encodingConfig.Codec, appState)
	require.NotEmpty(t, bankGenState.Supply.String())
}
