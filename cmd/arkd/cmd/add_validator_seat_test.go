package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// clientCodec resolves the codec the root command decodes genesis with.
func clientCodec(t *testing.T) codec.Codec {
	t.Helper()

	var clientCtx client.Context
	require.NoError(t, depinject.Inject(
		depinject.Configs(app.AppConfig,
			depinject.Supply(log.NewNopLogger(), unwiredWasmKeeper{}),
			depinject.Provide(ProvideClientContext),
		),
		&clientCtx,
	))
	return clientCtx.Codec
}

// TestAddValidatorSeatCmd grants two seats on a copy of the launch artefact
// and reads each result back: the locked account, the balances, the fee pool,
// the targets, and an unchanged supply. A repeated operator is refused with
// the file untouched, and the result still validates.
func TestAddValidatorSeatCmd(t *testing.T) {
	home := t.TempDir()
	genesisFile := filepath.Join(home, "config", "genesis.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(genesisFile), 0o755))
	artefact, err := os.ReadFile(filepath.Join("..", "..", "..", "app", "genesis", "genesis.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(genesisFile, artefact, 0o600)) //nolint:gosec // a copy of the artefact under t.TempDir()

	cdc := clientCodec(t)
	run := func(args ...string) error {
		rootCmd := NewRootCmd()
		rootCmd.SetArgs(append(args, fmt.Sprintf("--%s=%s", flags.FlagHome, home)))
		return svrcmd.Execute(rootCmd, "", app.DefaultNodeHome)
	}
	load := func() map[string]json.RawMessage {
		t.Helper()
		appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
		require.NoError(t, err)
		var state map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))
		return state
	}
	noah := chain.NativeBaseAmount
	grant := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatGrantNoah)))
	seat := grant.Add(sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatFloatNoah)))
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()

	initial := banktypes.GetGenesisStateFromAppState(cdc, load())
	poolStart := initial.Balances[slicesIndex(t, initial.Balances, pool)].Coins
	supply := initial.Supply

	operators := []sdk.AccAddress{
		secp256k1.GenPrivKey().PubKey().Address().Bytes(),
		secp256k1.GenPrivKey().PubKey().Address().Bytes(),
	}
	for i, operator := range operators {
		require.NoError(t, run("genesis", "add-validator-seat", operator.String()))
		state := load()
		seats := int64(i + 1)

		var authState authtypes.GenesisState
		cdc.MustUnmarshalJSON(state[authtypes.ModuleName], &authState)
		accounts, err := authtypes.UnpackAccounts(authState.Accounts)
		require.NoError(t, err)
		require.Len(t, accounts, int(seats))
		locked, ok := accounts[slicesIndexAccount(t, accounts, operator)].(*vestingtypes.PermanentLockedAccount)
		require.True(t, ok, "the seat is a permanently locked account")
		require.Equal(t, grant, locked.OriginalVesting)

		bankState := banktypes.GetGenesisStateFromAppState(cdc, state)
		require.Equal(t, seat, bankState.Balances[slicesIndex(t, bankState.Balances, operator.String())].Coins)
		poolNow := bankState.Balances[slicesIndex(t, bankState.Balances, pool)].Coins
		require.Equal(t, poolStart.Sub(seat.MulInt(math.NewInt(seats))...), poolNow)
		require.Equal(t, supply, bankState.Supply, "supply is unchanged")

		var distrState distrtypes.GenesisState
		cdc.MustUnmarshalJSON(state[distrtypes.ModuleName], &distrState)
		require.Equal(t, sdk.NewDecCoinsFromCoins(poolNow...), distrState.FeePool.CommunityPool,
			"the fee pool still equals the module balance")

		var treasuryState treasurytypes.GenesisState
		cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryState)
		require.Equal(t, chain.SeatValidatorShare.MulRaw(seats), treasuryState.EconomicPolicy.ValidatorBlockRewardTarget)
		require.Equal(t, chain.SeatOracleShare.MulRaw(seats), treasuryState.EconomicPolicy.OracleBlockRewardTarget)
	}

	before, err := os.ReadFile(genesisFile)
	require.NoError(t, err)
	require.ErrorContains(t, run("genesis", "add-validator-seat", operators[0].String()), "already holds an account")
	after, err := os.ReadFile(genesisFile)
	require.NoError(t, err)
	require.Equal(t, before, after, "a refused seat leaves the file untouched")

	require.NoError(t, run("genesis", "validate"))
}

func slicesIndex(t *testing.T, balances []banktypes.Balance, address string) int {
	t.Helper()
	for i, balance := range balances {
		if balance.Address == address {
			return i
		}
	}
	t.Fatalf("no balance for %s", address)
	return -1
}

func slicesIndexAccount(t *testing.T, accounts authtypes.GenesisAccounts, address sdk.AccAddress) int {
	t.Helper()
	for i, account := range accounts {
		if account.GetAddress().Equals(address) {
			return i
		}
	}
	t.Fatalf("no account for %s", address)
	return -1
}
