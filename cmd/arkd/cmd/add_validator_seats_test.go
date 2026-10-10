package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
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

// TestAddValidatorSeatsCmd grants seven seats in one run on a copy of the
// launch artefact and reads the result back: each vesting account on its
// window, the founder's at half the others' sum, the balances, the fee pool,
// the targets, and an unchanged supply. The artefact carries no genesis time,
// so seating before assembly sets it is refused; so are a count outside the
// launch bounds, a founder outside the operators, a seated operator, a grant
// one of whose later seats fails, and a bad address, each with the file
// untouched, and the result validates.
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
	grant := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.SeatGrant))
	seat := grant.Add(sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatFloatNoah)))
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	contributors := authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String()

	initial := banktypes.GetGenesisStateFromAppState(cdc, load())
	poolStart := initial.Balances[slicesIndex(t, initial.Balances, pool)].Coins
	contributorsStart := initial.Balances[slicesIndex(t, initial.Balances, contributors)].Coins
	supply := initial.Supply

	newOperator := func() sdk.AccAddress { return secp256k1.GenPrivKey().PubKey().Address().Bytes() }
	operators := make([]sdk.AccAddress, chain.MinLaunchSeats)
	for i := range operators {
		operators[i] = newOperator()
	}
	founder := operators[0]
	founderSeat := chain.FounderSeat(len(operators))
	extra := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, founderSeat.Sub(chain.SeatGrant)))
	seats := int64(len(operators))
	seatArgs := func(founder string, operators ...sdk.AccAddress) []string {
		args := []string{"genesis", "add-validator-seats"}
		if founder != "" {
			args = append(args, fmt.Sprintf("--%s=%s", flagFounder, founder))
		}
		for _, operator := range operators {
			args = append(args, operator.String())
		}
		return args
	}
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
	untouched(t, func() error { return run(seatArgs(founder.String(), operators...)...) }, "genesis time must be set")

	// Assembly sets the time in the file before seating.
	appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
	require.NoError(t, err)
	appGenesis.GenesisTime = genesisTime
	require.NoError(t, appGenesis.SaveAs(genesisFile))

	// The launch bounds and the founder are checked before anything is read.
	untouched(t, func() error { return run(seatArgs(founder.String(), operators[1:]...)...) }, "launch assembly seats between")
	tooMany := append(append([]sdk.AccAddress{}, operators...), make([]sdk.AccAddress, chain.MaxLaunchSeats-chain.MinLaunchSeats+1)...)
	for i := len(operators); i < len(tooMany); i++ {
		tooMany[i] = newOperator()
	}
	untouched(t, func() error { return run(seatArgs(founder.String(), tooMany...)...) }, "launch assembly seats between")
	untouched(t, func() error { return run(seatArgs(newOperator().String(), operators...)...) }, "is not among the operators")
	untouched(t, func() error { return run(seatArgs("", operators...)...) }, "required flag")
	untouched(t, func() error { return run(seatArgs("ark1notanaddress", operators...)...) }, "parse founder address")

	require.NoError(t, run(seatArgs(founder.String(), operators...)...))
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
		wantVesting, wantBalance := grant, seat
		if operator.Equals(founder) {
			wantVesting, wantBalance = grant.Add(extra...), seat.Add(extra...)
		}
		require.Equal(t, wantVesting, vesting.OriginalVesting)
		require.Equal(t, genesisTime.AddDate(chain.SeatVestingCliffYears, 0, 0).Unix(), vesting.StartTime)
		require.Equal(t, genesisTime.AddDate(chain.SeatVestingEndYears, 0, 0).Unix(), vesting.EndTime)
		require.Equal(t, wantBalance, bankState.Balances[slicesIndex(t, bankState.Balances, operator.String())].Coins)
	}
	poolNow := bankState.Balances[slicesIndex(t, bankState.Balances, pool)].Coins
	require.Equal(t, poolStart.Sub(seat.MulInt(math.NewInt(seats))...).Sub(extra...), poolNow, "the community pool pays one seat an operator and the founder's excess")
	require.Equal(t, contributorsStart, bankState.Balances[slicesIndex(t, bankState.Balances, contributors)].Coins,
		"seats never draw on the contributor pool")
	require.Equal(t, supply, bankState.Supply, "supply is unchanged")

	var distrState distrtypes.GenesisState
	cdc.MustUnmarshalJSON(state[distrtypes.ModuleName], &distrState)
	require.Equal(t, sdk.NewDecCoinsFromCoins(poolNow...), distrState.FeePool.CommunityPool,
		"the fee pool still equals the module balance")

	var treasuryState treasurytypes.GenesisState
	cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryState)
	require.Equal(t, chain.SeatValidatorShare.MulRaw(seats), treasuryState.EconomicPolicy.ValidatorBlockRewardTarget)
	require.Equal(t, chain.SeatOracleShare.MulRaw(seats), treasuryState.EconomicPolicy.OracleBlockRewardTarget)

	var disbursementState disbursementtypes.GenesisState
	cdc.MustUnmarshalJSON(state[disbursementtypes.ModuleName], &disbursementState)
	require.Len(t, disbursementState.Beneficiaries, int(seats))
	for _, p := range disbursementState.Beneficiaries {
		require.True(t, slices.ContainsFunc(operators, func(o sdk.AccAddress) bool { return o.String() == p.Address }))
		want := chain.SeatGrant
		if p.Address == founder.String() {
			want = founderSeat
		}
		require.Equal(t, want, p.Seat)
	}

	untouched(t, func() error { return run(seatArgs(founder.String(), operators...)...) }, "seats already granted")
	fresh := make([]sdk.AccAddress, chain.MinLaunchSeats)
	for i := range fresh {
		fresh[i] = newOperator()
	}
	fresh[1] = operators[1]
	untouched(t, func() error { return run(seatArgs(fresh[0].String(), fresh...)...) }, "seats already granted")
	badArgs := seatArgs(fresh[0].String(), fresh[:chain.MinLaunchSeats-1]...)
	untouched(t, func() error { return run(append(badArgs, "ark1notanaddress")...) }, "parse operator address")

	require.NoError(t, run("genesis", "validate"))
}

// TestAddValidatorSeatsCmdAnyCount seats two for a testnet: the bounds lift
// and the founder's seat is one seat at that count.
func TestAddValidatorSeatsCmdAnyCount(t *testing.T) {
	home := t.TempDir()
	genesisFile := filepath.Join(home, "config", "genesis.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(genesisFile), 0o755))
	artefact, err := os.ReadFile(filepath.Join("..", "..", "..", "app", "genesis", "testnet.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(genesisFile, artefact, 0o600)) //nolint:gosec // a copy of the artefact under t.TempDir()
	appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
	require.NoError(t, err)
	appGenesis.GenesisTime = time.Date(2027, time.January, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, appGenesis.SaveAs(genesisFile))

	operators := []sdk.AccAddress{
		secp256k1.GenPrivKey().PubKey().Address().Bytes(),
		secp256k1.GenPrivKey().PubKey().Address().Bytes(),
	}
	run := func(args ...string) error {
		rootCmd := NewRootCmd()
		rootCmd.SetArgs(append(args, fmt.Sprintf("--%s=%s", flags.FlagHome, home)))
		return svrcmd.Execute(rootCmd, "", app.DefaultNodeHome)
	}
	args := []string{"genesis", "add-validator-seats", fmt.Sprintf("--%s=%s", flagFounder, operators[0]), operators[0].String(), operators[1].String()}
	require.ErrorContains(t, run(args...), "launch assembly seats between")
	require.NoError(t, run(append(args, fmt.Sprintf("--%s", flagAnyCount))...))

	cdc := clientCodec(t)
	appGenesis, err = genutiltypes.AppGenesisFromFile(genesisFile)
	require.NoError(t, err)
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))
	var disbursementState disbursementtypes.GenesisState
	cdc.MustUnmarshalJSON(state[disbursementtypes.ModuleName], &disbursementState)
	require.Len(t, disbursementState.Beneficiaries, 2)
	for _, p := range disbursementState.Beneficiaries {
		require.Equal(t, chain.SeatGrant, p.Seat, "at two seats the founder's seat is one seat")
	}
}

// TestLaunchSeatBounds pins the count bounds to what derives them: the
// founder's seat reaches 3% of supply at the top.
func TestLaunchSeatBounds(t *testing.T) {
	threePercent := chain.NativeBaseAmount(1_000_000_000).MulRaw(3).QuoRaw(100)
	require.True(t, chain.FounderSeat(chain.MaxLaunchSeats).LTE(threePercent), "the founder's seat holds at 3% of supply at the top count")
	require.True(t, chain.FounderSeat(chain.MaxLaunchSeats+1).GT(threePercent), "one more seat would put it over")
	// Genesis bonded stake, the other seats and the founder's third, clears the 40M a
	// buyer of the first year's liquid NOAH could take a third against.
	floor := chain.NativeBaseAmount(40_000_000)
	stake := func(count int) math.Int {
		return chain.SeatGrant.MulRaw(int64(count - 1)).Add(chain.FounderSeat(count))
	}
	require.True(t, stake(chain.MinLaunchSeats).GTE(floor))
	require.True(t, stake(chain.MinLaunchSeats-1).LT(floor), "one seat fewer would fall under it")
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
