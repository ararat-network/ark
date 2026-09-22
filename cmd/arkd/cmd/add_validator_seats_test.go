package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestAddValidatorSeatsCmd grants two seats in one run on a copy of the
// launch artefact and reads the result back: each vesting account on its
// window, the balances, the fee pool, the targets, and an unchanged supply.
// The artefact carries no genesis time, so seating before assembly sets it is
// refused; so are a seated operator, a grant one of whose later seats fails,
// and a bad address, each with the file untouched, and the result validates.
func TestAddValidatorSeatsCmd(t *testing.T) {
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
	seats := int64(len(operators))
	genesisTime := time.Date(2027, time.January, 4, 12, 0, 0, 0, time.UTC)
	untouched := func(t *testing.T, run func() error, errPhrase string) {
		t.Helper()
		before, err := os.ReadFile(genesisFile)
		require.NoError(t, err)
		require.ErrorContains(t, run(), errPhrase)
		after, err := os.ReadFile(genesisFile)
		require.NoError(t, err)
		require.Equal(t, before, after, "a refused grant leaves the file untouched")
	}
	untouched(t, func() error { return run("genesis", "add-validator-seats", operators[0].String()) }, "genesis time must be set")

	// Assembly sets the time in the file before seating.
	appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
	require.NoError(t, err)
	appGenesis.GenesisTime = genesisTime
	require.NoError(t, appGenesis.SaveAs(genesisFile))

	require.NoError(t, run("genesis", "add-validator-seats", operators[0].String(), operators[1].String()))
	state := load()
	appGenesis, err = genutiltypes.AppGenesisFromFile(genesisFile)
	require.NoError(t, err)
	require.True(t, genesisTime.Equal(appGenesis.GenesisTime), "seating keeps the time")

	var authState authtypes.GenesisState
	cdc.MustUnmarshalJSON(state[authtypes.ModuleName], &authState)
	accounts, err := authtypes.UnpackAccounts(authState.Accounts)
	require.NoError(t, err)
	require.Len(t, accounts, int(seats))
	bankState := banktypes.GetGenesisStateFromAppState(cdc, state)
	for _, operator := range operators {
		vesting, ok := accounts[slicesIndexAccount(t, accounts, operator)].(*vestingtypes.ContinuousVestingAccount)
		require.True(t, ok, "the seat is a continuous vesting account")
		require.Equal(t, grant, vesting.OriginalVesting)
		require.Equal(t, genesisTime.AddDate(chain.SeatVestingCliffYears, 0, 0).Unix(), vesting.StartTime)
		require.Equal(t, genesisTime.AddDate(chain.SeatVestingEndYears, 0, 0).Unix(), vesting.EndTime)
		require.Equal(t, seat, bankState.Balances[slicesIndex(t, bankState.Balances, operator.String())].Coins)
	}
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

	third := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address().Bytes())
	untouched(t, func() error { return run("genesis", "add-validator-seats", operators[0].String()) }, "already holds an account")
	untouched(t, func() error {
		return run("genesis", "add-validator-seats", third.String(), operators[1].String())
	}, "already holds an account")
	untouched(t, func() error { return run("genesis", "add-validator-seats", third.String(), "ark1notanaddress") }, "parse operator address")

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
