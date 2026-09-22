// Package genesis owns the launch artefact beside it, genesis.json, and the
// one edit assembly makes to it: granting the validator seats.
package genesis

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// AddValidatorSeats grants one equal validator seat per operator in appState:
// a continuous vesting account at the operator holding chain.SeatGrantNoah,
// vesting from chain.SeatVestingCliffYears to chain.SeatVestingEndYears after
// genesisTime, a liquid float of chain.SeatFloatNoah beside it, both moved out
// of the community pool, and both reward targets raised by one seat's share.
// Supply is unchanged. It refuses an unset genesis time, an operator already
// holding an account or balance, and a pool that cannot fund the seats, and
// writes nothing unless every seat and every edited module passes its own
// validation.
func AddValidatorSeats(cdc codec.Codec, appState map[string]json.RawMessage, operators []sdk.AccAddress, genesisTime time.Time) error {
	if len(operators) == 0 {
		return errors.New("at least one operator address must be set")
	}
	if genesisTime.IsZero() {
		return errors.New("genesis time must be set before the seats are granted: they vest from it")
	}
	seated := maps.Clone(appState)
	for _, operator := range operators {
		if err := addValidatorSeat(cdc, seated, operator, genesisTime); err != nil {
			return fmt.Errorf("seat %s: %w", operator, err)
		}
	}
	maps.Copy(appState, seated)
	return nil
}

// addValidatorSeat grants one seat, writing nothing unless every edited
// module passes its own validation.
func addValidatorSeat(cdc codec.Codec, appState map[string]json.RawMessage, operator sdk.AccAddress, genesisTime time.Time) error {
	if len(operator) == 0 {
		return errors.New("operator address must be set")
	}
	grant := noahCoins(chain.SeatGrantNoah)
	seat := grant.Add(noahCoins(chain.SeatFloatNoah)...)

	edits := []struct {
		module string
		edit   func(json.RawMessage) (json.RawMessage, error)
	}{
		{authtypes.ModuleName, func(raw json.RawMessage) (json.RawMessage, error) {
			return addSeatAccount(cdc, raw, operator, grant, genesisTime)
		}},
		{banktypes.ModuleName, func(raw json.RawMessage) (json.RawMessage, error) {
			return fundSeat(cdc, raw, operator, seat)
		}},
		{distrtypes.ModuleName, func(raw json.RawMessage) (json.RawMessage, error) {
			return drawSeatFromPool(cdc, raw, seat)
		}},
		{treasurytypes.ModuleName, func(raw json.RawMessage) (json.RawMessage, error) {
			return raiseSeatTargets(cdc, raw)
		}},
	}
	edited := make(map[string]json.RawMessage, len(edits))
	for _, e := range edits {
		raw, present := appState[e.module]
		if !present || len(raw) == 0 {
			return fmt.Errorf("genesis carries no %s state", e.module)
		}
		out, err := e.edit(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", e.module, err)
		}
		edited[e.module] = out
	}
	for module, raw := range edited {
		appState[module] = raw
	}
	return nil
}

func noahCoins(whole int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(whole)))
}

// addSeatAccount appends the vesting account, its window counted in calendar
// years from genesisTime. It carries no public key; the gentx's signature sets
// it, as for any genesis account.
func addSeatAccount(cdc codec.Codec, raw json.RawMessage, operator sdk.AccAddress, grant sdk.Coins, genesisTime time.Time) (json.RawMessage, error) {
	var state authtypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &state); err != nil {
		return nil, err
	}
	accounts, err := authtypes.UnpackAccounts(state.Accounts)
	if err != nil {
		return nil, err
	}
	if accounts.Contains(operator) {
		return nil, fmt.Errorf("operator %s already holds an account", operator)
	}
	seat, err := vestingtypes.NewContinuousVestingAccount(
		authtypes.NewBaseAccount(operator, nil, 0, 0),
		grant,
		genesisTime.AddDate(chain.SeatVestingCliffYears, 0, 0).Unix(),
		genesisTime.AddDate(chain.SeatVestingEndYears, 0, 0).Unix(),
	)
	if err != nil {
		return nil, err
	}
	accounts = append(accounts, seat)
	if state.Accounts, err = authtypes.PackAccounts(authtypes.SanitizeGenesisAccounts(accounts)); err != nil {
		return nil, err
	}
	if err := authtypes.ValidateGenesis(state); err != nil {
		return nil, err
	}
	return cdc.MarshalJSON(&state)
}

// fundSeat moves the seat's coins from the community pool's balance to the
// operator's, leaving supply untouched.
func fundSeat(cdc codec.Codec, raw json.RawMessage, operator sdk.AccAddress, seat sdk.Coins) (json.RawMessage, error) {
	var state banktypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &state); err != nil {
		return nil, err
	}
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	poolIndex := -1
	for i, balance := range state.Balances {
		switch balance.Address {
		case operator.String():
			return nil, fmt.Errorf("operator %s already holds a balance", operator)
		case pool:
			poolIndex = i
		}
	}
	if poolIndex < 0 {
		return nil, errors.New("the community pool holds no balance to grant from")
	}
	held := state.Balances[poolIndex].Coins
	if !held.IsAllGTE(seat) {
		return nil, fmt.Errorf("the community pool holds %s, short of the %s seat", held, seat)
	}
	state.Balances[poolIndex].Coins = held.Sub(seat...)
	state.Balances = append(state.Balances, banktypes.Balance{Address: operator.String(), Coins: seat})
	state.Balances = banktypes.SanitizeGenesisBalances(state.Balances)
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return cdc.MarshalJSON(&state)
}

// drawSeatFromPool lowers the fee pool's record of the community pool by the
// same coins, so it still equals the module balance Distribution checks at
// InitGenesis.
func drawSeatFromPool(cdc codec.Codec, raw json.RawMessage, seat sdk.Coins) (json.RawMessage, error) {
	var state distrtypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &state); err != nil {
		return nil, err
	}
	remaining, short := state.FeePool.CommunityPool.SafeSub(sdk.NewDecCoinsFromCoins(seat...))
	if short {
		return nil, fmt.Errorf("the fee pool records %s, short of the %s seat", state.FeePool.CommunityPool, seat)
	}
	state.FeePool.CommunityPool = remaining
	if err := distrtypes.ValidateGenesis(&state); err != nil {
		return nil, err
	}
	return cdc.MarshalJSON(&state)
}

// raiseSeatTargets adds one seat's share to both reward targets.
func raiseSeatTargets(cdc codec.Codec, raw json.RawMessage) (json.RawMessage, error) {
	var state treasurytypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &state); err != nil {
		return nil, err
	}
	policy := &state.EconomicPolicy
	policy.ValidatorBlockRewardTarget = policy.ValidatorBlockRewardTarget.Add(chain.SeatValidatorShare)
	policy.OracleBlockRewardTarget = policy.OracleBlockRewardTarget.Add(chain.SeatOracleShare)
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return cdc.MarshalJSON(&state)
}
