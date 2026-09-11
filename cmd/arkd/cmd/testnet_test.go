// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/simd/cmd/testnet_test.go.
// Modified for Ark: testnet fixtures and genesis assertions.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
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

	"github.com/ararat-network/ark/app/params"
)

// makeArkTestEncodingConfig uses Ark address codecs so gentxs match the sealed SDK prefixes. The
// upstream helper's cosmos codecs cannot decode Ark validator addresses.
func makeArkTestEncodingConfig(modules ...module.AppModuleBasic) moduletestutil.TestEncodingConfig {
	interfaceRegistry := codectestutil.CodecOptions{
		AccAddressPrefix: params.Bech32PrefixAccAddr,
		ValAddressPrefix: params.Bech32PrefixValAddr,
	}.NewInterfaceRegistry()
	protoCodec := codec.NewProtoCodec(interfaceRegistry)

	encCfg := moduletestutil.TestEncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Codec:             protoCodec,
		TxConfig:          authtx.NewTxConfig(protoCodec, authtx.DefaultSignModes),
		Amino:             codec.NewLegacyAmino(),
	}

	mb := module.NewBasicManager(modules...)
	std.RegisterLegacyAminoCodec(encCfg.Amino)
	std.RegisterInterfaces(encCfg.InterfaceRegistry)
	mb.RegisterLegacyAminoCodec(encCfg.Amino)
	mb.RegisterInterfaces(encCfg.InterfaceRegistry)

	return encCfg
}

func TestTestnetCmd(t *testing.T) {
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
	encodingConfig := makeArkTestEncodingConfig(auth.AppModuleBasic{}, staking.AppModuleBasic{})
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
	cmd := newTestnetInitFilesCmd(moduleBasic, banktypes.GenesisBalancesIterator{})
	require.Equal(t, "6000000anoah", cmd.Flags().Lookup(flagMinGasPrices).DefValue)
	cmd.SetArgs([]string{
		fmt.Sprintf("--%s=test", flags.FlagKeyringBackend),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, chainID),
		fmt.Sprintf("--%s=%s", flagOutputDir, home),
		fmt.Sprintf("--%s", flagSingleHost),
	})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)

	node0ConfigDir := filepath.Join(home, "node0", "arkd", "config")
	appConfig, err := os.ReadFile(filepath.Join(node0ConfigDir, "app.toml"))
	require.NoError(t, err)
	require.Contains(t, string(appConfig), "max-txs = 5000")
	require.Contains(t, string(appConfig), `metrics-sink = "otel"`)
	require.Contains(t, string(appConfig), "prometheus-retention-time = 0")
	require.Contains(t, string(appConfig), "[prometheus]\n")
	require.Contains(t, string(appConfig), "enabled = true")
	require.Contains(t, string(appConfig), `address = "0.0.0.0:9464"`)

	node1AppConfig, err := os.ReadFile(filepath.Join(home, "node1", "arkd", "config", "app.toml"))
	require.NoError(t, err)
	require.Contains(t, string(node1AppConfig), `address = "0.0.0.0:9465"`)

	// Each home's client.toml names its chain, keyring, key, and node.
	clientConfig := readFile(t, filepath.Join(node0ConfigDir, "client.toml"))
	require.Contains(t, clientConfig, fmt.Sprintf("chain-id = %q", chainID))
	require.Contains(t, clientConfig, `keyring-backend = "test"`)
	require.Contains(t, clientConfig, `keyring-default-keyname = "node0"`)
	require.Contains(t, clientConfig, `node = "tcp://localhost:26657"`)
	node1ClientConfig := readFile(t, filepath.Join(home, "node1", "arkd", "config", "client.toml"))
	require.Contains(t, node1ClientConfig, `keyring-default-keyname = "node1"`)
	require.Contains(t, node1ClientConfig, `node = "tcp://localhost:26658"`)

	expectedAuthority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	for i := range 2 {
		nodeConfigDir := filepath.Join(home, fmt.Sprintf("node%d", i), "arkd", "config")

		// config.toml is written after the key loop; every per-node listener
		// must reach it, not the last node's.
		cometConfig, err := os.ReadFile(filepath.Join(nodeConfigDir, "config.toml"))
		require.NoError(t, err)
		require.Contains(t, string(cometConfig), fmt.Sprintf(`laddr = "tcp://0.0.0.0:%d"`, rpcPort+i))
		require.Contains(t, string(cometConfig), fmt.Sprintf(`pprof_laddr = "localhost:%d"`, pprofPort+i))
		require.Contains(t, string(cometConfig), fmt.Sprintf(`prometheus_listen_addr = ":%d"`, cometMetricsPort+i))
		require.Contains(t, string(cometConfig), `timeout_commit = "5s"`)

		// Empty selects noop telemetry; otelconf does not implement this exporter.
		otel, err := os.ReadFile(filepath.Join(nodeConfigDir, "otel.yaml"))
		require.NoError(t, err)
		require.Empty(t, otel)

		genesis, err := genutiltypes.AppGenesisFromFile(filepath.Join(nodeConfigDir, "genesis.json"))
		require.NoError(t, err)
		require.Equal(t, expectedAuthority, genesis.Consensus.Params.Authority.Authority)
		require.EqualValues(t, 1, genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
	}

	genFile := cfg.GenesisFile()
	appState, _, err := genutiltypes.GenesisStateFromGenFile(genFile)
	require.NoError(t, err)
	require.NotContains(t, appState, "mint")

	bankGenState := banktypes.GetGenesisStateFromAppState(encodingConfig.Codec, appState)
	require.NotEmpty(t, bankGenState.Supply.String())
}

// Both testnet commands take --commit-timeout: start read it without
// registering it and ran the in-process network with none. init-files hands
// nodes the node default; the in-process network needs its first block
// inside network.New's five-second budget.
func TestTestnetCommandsRegisterCommitTimeout(t *testing.T) {
	for cmd, want := range map[*cobra.Command]string{
		newTestnetStartCmd(): "1s",
		newTestnetInitFilesCmd(nil, banktypes.GenesisBalancesIterator{}): "5s",
	} {
		flag := cmd.Flags().Lookup(flagCommitTimeout)
		require.NotNil(t, flag, cmd.Name())
		require.Equal(t, want, flag.DefValue, cmd.Name())
	}
}

// The root pre-run binds every flag to the home's app.toml under the flag's
// name, both ways: init-files used to write the home's minimum-gas-prices into
// every node and a fresh home used to take the flag's default. No testnet
// flag may share a key with app.toml.
func TestTestnetFlagsShareNoAppConfigKey(t *testing.T) {
	keys := map[string]bool{}
	for _, key := range viperFromTOML(t, renderAppTOML(t, appConfigTemplate)).AllKeys() {
		keys[key] = true
	}
	require.True(t, keys["minimum-gas-prices"])
	require.True(t, keys["api.address"])
	for _, cmd := range []*cobra.Command{newTestnetStartCmd(), newTestnetInitFilesCmd(nil, banktypes.GenesisBalancesIterator{})} {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			require.Falsef(t, keys[f.Name], "%s --%s shares its name with an app.toml key", cmd.Name(), f.Name)
		})
	}
}

// The in-process network refuses a commit timeout its first block cannot
// beat, and renders the SDK's app.toml again after the root pre-run has
// installed Ark's template.
func TestInProcessNetworkConfig(t *testing.T) {
	t.Cleanup(func() { serverconfig.SetConfigTemplate(appConfigTemplate) })

	_, err := inProcessNetworkConfig(startArgs{numValidators: 1, timeoutCommit: 5 * time.Second})
	require.ErrorContains(t, err, "within five seconds")

	serverconfig.SetConfigTemplate(appConfigTemplate)
	cfg, err := inProcessNetworkConfig(startArgs{numValidators: 1, timeoutCommit: time.Second, chainID: "in-process"})
	require.NoError(t, err)
	require.Equal(t, "in-process", cfg.ChainID)
	require.Equal(t, time.Second, cfg.TimeoutCommit)
	// What network.New does for each validator, and what panicked before.
	require.NotPanics(t, func() {
		serverconfig.WriteConfigFile(filepath.Join(t.TempDir(), "app.toml"), serverconfig.DefaultConfig())
	})
}

// A count below one is refused before anything is read or written; a
// negative one used to panic the slice allocation.
func TestTestnetCommandsRefuseCountsBelowOne(t *testing.T) {
	for _, count := range []string{"0", "-1"} {
		for _, cmd := range []*cobra.Command{newTestnetStartCmd(), newTestnetInitFilesCmd(nil, banktypes.GenesisBalancesIterator{})} {
			t.Run(cmd.Name()+"/"+count, func(t *testing.T) {
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{"--" + flagNumValidators, count})
				require.ErrorContains(t, cmd.Execute(), "must be at least 1")
			})
		}
	}
}

func TestCalculateIP(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		offset      int
		want        string
		errContains string
	}{
		{name: "zero offset returns the starting address", ip: "192.168.0.1", want: "192.168.0.1"},
		{name: "offset increments the last octet", ip: "192.168.0.1", offset: 3, want: "192.168.0.4"},
		{name: "the last usable octet", ip: "192.168.0.1", offset: 254, want: "192.168.0.255"},
		{name: "a zero starting octet", ip: "10.0.0.0", offset: 255, want: "10.0.0.255"},
		{
			// Incrementing in a loop wrapped here, handing this node the same
			// address as the one three past the start.
			name:        "carry past the last octet is refused",
			ip:          "192.168.0.250",
			offset:      10,
			errContains: "overflows the last octet",
		},
		{
			name:        "one past the broadcast address",
			ip:          "192.168.0.255",
			offset:      1,
			errContains: "overflows the last octet",
		},
		{
			name:        "an offset wider than an octet",
			ip:          "192.168.0.1",
			offset:      256,
			errContains: "overflows the last octet",
		},
		{
			name:        "a negative offset",
			ip:          "192.168.0.10",
			offset:      -1,
			errContains: "overflows the last octet",
		},
		{name: "not an address", ip: "not-an-ip", errContains: "non ipv4 address"},
		{name: "an ipv6 address", ip: "::1", offset: 1, errContains: "non ipv4 address"},
		{name: "an ipv4 address in ipv6 form", ip: "::ffff:192.168.0.1", offset: 1, want: "192.168.0.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateIP(tt.ip, tt.offset)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				require.Empty(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCalculateIPAssignsDistinctAddresses checks that validator IP assignments remain distinct and
// refuse overflow.
func TestCalculateIPAssignsDistinctAddresses(t *testing.T) {
	const start = "192.168.0.1"

	seen := make(map[string]int, maxOctet)
	for i := range maxOctet {
		ip, err := calculateIP(start, i)
		require.NoError(t, err)
		if first, clash := seen[ip]; clash {
			t.Fatalf("offset %d reuses the address issued to offset %d: %s", i, first, ip)
		}
		seen[ip] = i
	}
	require.Len(t, seen, maxOctet)

	// One past the last usable offset is refused rather than reissued.
	_, err := calculateIP(start, maxOctet)
	require.ErrorContains(t, err, "overflows the last octet")
}

// TestGetIPUsesTheStartingAddress covers the branch that does not consult the
// host's external interface.
func TestGetIPUsesTheStartingAddress(t *testing.T) {
	got, err := getIP(2, "10.1.2.3")
	require.NoError(t, err)
	require.Equal(t, "10.1.2.5", got)

	_, err = getIP(1, "10.1.2.255")
	require.ErrorContains(t, err, "overflows the last octet")
}
