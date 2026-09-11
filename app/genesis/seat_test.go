package genesis_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app/genesis"
	"github.com/ararat-network/ark/app/params"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func seatCodec() codec.Codec {
	registry := codectypes.NewInterfaceRegistry()
	authtypes.RegisterInterfaces(registry)
	vestingtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	distrtypes.RegisterInterfaces(registry)
	treasurytypes.RegisterInterfaces(registry)
	return codec.NewProtoCodec(registry)
}

// artefactState loads the launch artefact's app state from this directory.
func artefactState(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	appGenesis, err := genutiltypes.AppGenesisFromFile("genesis.json")
	require.NoError(t, err)
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))
	return state
}

func operatorAddress() sdk.AccAddress {
	return secp256k1.GenPrivKey().PubKey().Address().Bytes()
}

func TestAddValidatorSeat(t *testing.T) {
	cdc := seatCodec()
	state := artefactState(t)
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	seat := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(chain.SeatGrantNoah+chain.SeatFloatNoah)))
	first, second := operatorAddress(), operatorAddress()

	require.NoError(t, genesis.AddValidatorSeat(cdc, state, first))
	require.NoError(t, genesis.AddValidatorSeat(cdc, state, second))

	var authState authtypes.GenesisState
	cdc.MustUnmarshalJSON(state[authtypes.ModuleName], &authState)
	accounts, err := authtypes.UnpackAccounts(authState.Accounts)
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	for _, account := range accounts {
		locked, ok := account.(*vestingtypes.PermanentLockedAccount)
		require.True(t, ok)
		require.Equal(t, chain.NativeBaseAmount(chain.SeatGrantNoah), locked.OriginalVesting.AmountOf(chain.NoahBaseDenom))
		require.Nil(t, locked.GetPubKey(), "the gentx supplies the key")
	}

	var bankState banktypes.GenesisState
	cdc.MustUnmarshalJSON(state[banktypes.ModuleName], &bankState)
	total := sdk.NewCoins()
	for _, balance := range bankState.Balances {
		total = total.Add(balance.Coins...)
		switch balance.Address {
		case first.String(), second.String():
			require.Equal(t, seat, balance.Coins)
		case pool:
			require.Equal(t,
				chain.NativeBaseAmount(835_000_000).Sub(seat.AmountOf(chain.NoahBaseDenom).MulRaw(2)),
				balance.Coins.AmountOf(chain.NoahBaseDenom),
			)
		}
	}
	require.Equal(t, bankState.Supply, total, "supply still equals the balances")

	var distrState distrtypes.GenesisState
	cdc.MustUnmarshalJSON(state[distrtypes.ModuleName], &distrState)
	require.Equal(t,
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(835_000_000).Sub(seat.AmountOf(chain.NoahBaseDenom).MulRaw(2))),
		distrState.FeePool.CommunityPool.AmountOf(chain.NoahBaseDenom),
	)

	var treasuryState treasurytypes.GenesisState
	cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryState)
	require.Equal(t, chain.SeatValidatorShare.MulRaw(2), treasuryState.EconomicPolicy.ValidatorBlockRewardTarget)
	require.Equal(t, chain.SeatOracleShare.MulRaw(2), treasuryState.EconomicPolicy.OracleBlockRewardTarget)
}

func TestAddValidatorSeatRefusals(t *testing.T) {
	cdc := seatCodec()
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	taken := operatorAddress()

	tests := []struct {
		name      string
		mutate    func(state map[string]json.RawMessage)
		operator  sdk.AccAddress
		errPhrase string
	}{
		{
			name: "operator already seated",
			mutate: func(state map[string]json.RawMessage) {
				require.NoError(t, genesis.AddValidatorSeat(cdc, state, taken))
			},
			operator:  taken,
			errPhrase: "already holds an account",
		},
		{
			name: "operator already holds a balance",
			mutate: func(state map[string]json.RawMessage) {
				var bankState banktypes.GenesisState
				cdc.MustUnmarshalJSON(state[banktypes.ModuleName], &bankState)
				bankState.Balances = append(bankState.Balances, banktypes.Balance{
					Address: taken.String(),
					Coins:   sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.OneInt())),
				})
				bankState.Supply = bankState.Supply.Add(sdk.NewCoin(chain.NoahBaseDenom, math.OneInt()))
				state[banktypes.ModuleName] = cdc.MustMarshalJSON(&bankState)
			},
			operator:  taken,
			errPhrase: "already holds a balance",
		},
		{
			name: "pool short of a seat",
			mutate: func(state map[string]json.RawMessage) {
				var bankState banktypes.GenesisState
				cdc.MustUnmarshalJSON(state[banktypes.ModuleName], &bankState)
				for i := range bankState.Balances {
					if bankState.Balances[i].Address == pool {
						bankState.Balances[i].Coins = sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1)))
					}
				}
				bankState.Supply = nil
				for _, balance := range bankState.Balances {
					bankState.Supply = bankState.Supply.Add(balance.Coins...)
				}
				state[banktypes.ModuleName] = cdc.MustMarshalJSON(&bankState)
			},
			operator:  operatorAddress(),
			errPhrase: "short of the",
		},
		{
			name:      "missing module state",
			mutate:    func(state map[string]json.RawMessage) { delete(state, treasurytypes.ModuleName) },
			operator:  operatorAddress(),
			errPhrase: "carries no treasury state",
		},
		{
			name:      "empty operator",
			mutate:    func(map[string]json.RawMessage) {},
			operator:  nil,
			errPhrase: "operator address must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := artefactState(t)
			tt.mutate(state)
			before := map[string]string{}
			for module, raw := range state {
				before[module] = string(raw)
			}

			err := genesis.AddValidatorSeat(cdc, state, tt.operator)
			require.ErrorContains(t, err, tt.errPhrase)
			for module, raw := range state {
				require.Equal(t, before[module], string(raw), "%s is untouched by a refused seat", module)
			}
		})
	}
}

// TestMain gives the SDK's global address config the Ark prefixes the
// artefact is written in, which the app's own init does everywhere else.
func TestMain(m *testing.M) {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(params.Bech32PrefixAccAddr, params.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(params.Bech32PrefixValAddr, params.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(params.Bech32PrefixConsAddr, params.Bech32PrefixConsPub)
	os.Exit(m.Run())
}
