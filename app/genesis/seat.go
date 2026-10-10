// Package genesis owns the launch artefact beside it, genesis.json, and the
// one edit assembly makes to it: granting the validator seats.
package genesis

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/ararat-network/ark/pkg/chain"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// AddValidatorSeats grants one validator seat per operator in appState: a
// continuous vesting account at the operator holding chain.SeatGrant, vesting
// from chain.SeatVestingCliffYears to chain.SeatVestingEndYears after
// genesisTime, a liquid float of chain.SeatFloatNoah beside it, both moved out
// of the community pool, and both reward targets raised by one seat's share.
// The founder, one of the operators, holds chain.FounderSeat for the count
// instead, half the other seats' sum, on the same account and schedule and
// from the same pool. Supply is unchanged. It refuses an operator named twice,
// a founder outside the operators, an unset genesis time, a file already
// seated, since the founding set is seated in one run, an operator already
// holding an account or balance, and a pool that cannot fund the seats, and
// changes appState only once every edited module passes its own validation.
// The launch count bounds are the command's. Each seat is recorded on the
// operator's Disbursement beneficiary, a record nothing reads (D115).
func AddValidatorSeats(cdc codec.Codec, appState map[string]json.RawMessage, operators []sdk.AccAddress, founder sdk.AccAddress, genesisTime time.Time) error {
	if len(operators) == 0 {
		return errors.New("at least one operator address must be set")
	}
	named := make(map[string]bool, len(operators))
	for _, operator := range operators {
		if len(operator) == 0 {
			return errors.New("operator address must be set")
		}
		if named[operator.String()] {
			return fmt.Errorf("operator %s named twice", operator)
		}
		named[operator.String()] = true
	}
	if len(founder) == 0 {
		return errors.New("founder address must be set")
	}
	if !named[founder.String()] {
		return fmt.Errorf("founder %s is not among the operators", founder)
	}
	if genesisTime.IsZero() {
		return errors.New("genesis time must be set before the seats are granted: they vest from it")
	}

	s, err := decodeSeatStates(cdc, appState)
	if err != nil {
		return err
	}
	if err := s.refuseSeated(); err != nil {
		return err
	}
	founderSeat := chain.FounderSeat(len(operators))
	drawn := sdk.NewCoins()
	for _, operator := range operators {
		grant := chain.SeatGrant
		if operator.Equals(founder) {
			grant = founderSeat
		}
		seat, err := s.addSeat(operator, grant, genesisTime)
		if err != nil {
			return fmt.Errorf("seat %s: %w", operator, err)
		}
		drawn = drawn.Add(seat...)
	}
	if err := s.drawFromPool(drawn); err != nil {
		return err
	}
	s.raiseTargets(len(operators))
	return s.encodeInto(cdc, appState)
}

// seatStates is every module genesis seating edits, decoded once: the seats
// are typed edits, and appState changes only in encodeInto.
type seatStates struct {
	auth         authtypes.GenesisState
	accounts     authtypes.GenesisAccounts
	bank         banktypes.GenesisState
	distr        distrtypes.GenesisState
	treasury     treasurytypes.GenesisState
	disbursement disbursementtypes.GenesisState
}

type seatModule struct {
	name     string
	state    proto.Message
	validate func() error
}

// modules lists the genesis in s. Validate goes through closures: bank's,
// Treasury's, and Disbursement's take value receivers, so a method value
// would check the state as decoded.
func (s *seatStates) modules() []seatModule {
	return []seatModule{
		{authtypes.ModuleName, &s.auth, func() error { return authtypes.ValidateGenesis(s.auth) }},
		{banktypes.ModuleName, &s.bank, func() error { return s.bank.Validate() }},
		{distrtypes.ModuleName, &s.distr, func() error { return distrtypes.ValidateGenesis(&s.distr) }},
		{treasurytypes.ModuleName, &s.treasury, func() error { return s.treasury.Validate() }},
		{disbursementtypes.ModuleName, &s.disbursement, func() error { return s.disbursement.Validate() }},
	}
}

func decodeSeatStates(cdc codec.Codec, appState map[string]json.RawMessage) (*seatStates, error) {
	s := &seatStates{}
	for _, m := range s.modules() {
		raw, present := appState[m.name]
		if !present || len(raw) == 0 {
			return nil, fmt.Errorf("genesis carries no %s state", m.name)
		}
		if err := cdc.UnmarshalJSON(raw, m.state); err != nil {
			return nil, fmt.Errorf("%s: %w", m.name, err)
		}
	}
	var err error
	if s.accounts, err = authtypes.UnpackAccounts(s.auth.Accounts); err != nil {
		return nil, fmt.Errorf("%s: %w", authtypes.ModuleName, err)
	}
	return s, nil
}

// refuseSeated rejects a file that already carries a seat: the founding set
// is seated in one run, which is what fixes the reward targets and the
// beneficiary records assembly then confirms by hand.
func (s *seatStates) refuseSeated() error {
	for _, p := range s.disbursement.Beneficiaries {
		if !p.Seat.IsNil() && p.Seat.IsPositive() {
			return fmt.Errorf("seats already granted, %s holds one: the founding set is seated in one run", p.Address)
		}
	}
	return nil
}

// addSeat opens a seat of grant at operator: a continuous vesting account,
// its window counted in calendar years from genesisTime, a balance of the
// grant and the float, and the seat on the operator's beneficiary. The
// account carries no public key; the gentx's signature sets it, as for any
// genesis account. It returns the balance, which the pool has yet to pay.
func (s *seatStates) addSeat(operator sdk.AccAddress, grant math.Int, genesisTime time.Time) (sdk.Coins, error) {
	if s.accounts.Contains(operator) {
		return nil, fmt.Errorf("operator %s already holds an account", operator)
	}
	if slices.ContainsFunc(s.bank.Balances, func(b banktypes.Balance) bool { return b.Address == operator.String() }) {
		return nil, fmt.Errorf("operator %s already holds a balance", operator)
	}
	vesting := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, grant))
	account, err := vestingtypes.NewContinuousVestingAccount(
		authtypes.NewBaseAccount(operator, nil, 0, 0),
		vesting,
		genesisTime.AddDate(chain.SeatVestingCliffYears, 0, 0).Unix(),
		genesisTime.AddDate(chain.SeatVestingEndYears, 0, 0).Unix(),
	)
	if err != nil {
		return nil, err
	}
	seat := vesting.Add(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(chain.SeatFloatNoah)))
	s.accounts = append(s.accounts, account)
	s.bank.Balances = append(s.bank.Balances, banktypes.Balance{Address: operator.String(), Coins: seat})
	s.disbursement.Beneficiaries = append(s.disbursement.Beneficiaries, disbursementtypes.Beneficiary{
		Address:            operator.String(),
		Controller:         operator.String(),
		OwnershipCommitted: math.ZeroInt(),
		Seat:               grant,
	})
	return seat, nil
}

// drawFromPool takes coins from the community pool's balance and from the
// fee pool's record of it, which InitGenesis requires equal. The seats'
// balances already hold the coins, so supply is unchanged.
func (s *seatStates) drawFromPool(coins sdk.Coins) error {
	pool := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	i := slices.IndexFunc(s.bank.Balances, func(b banktypes.Balance) bool { return b.Address == pool })
	if i < 0 {
		return errors.New("the community pool holds no balance to grant from")
	}
	held := s.bank.Balances[i].Coins
	if !held.IsAllGTE(coins) {
		return fmt.Errorf("the community pool holds %s, short of the %s the seats need", held, coins)
	}
	recorded, short := s.distr.FeePool.CommunityPool.SafeSub(sdk.NewDecCoinsFromCoins(coins...))
	if short {
		return fmt.Errorf("the fee pool records %s, short of the %s the seats need", s.distr.FeePool.CommunityPool, coins)
	}
	s.bank.Balances[i].Coins = held.Sub(coins...)
	s.distr.FeePool.CommunityPool = recorded
	return nil
}

// raiseTargets adds one share per seat to both reward targets; the
// founder's larger seat adds one share like any other.
func (s *seatStates) raiseTargets(seats int) {
	policy := &s.treasury.EconomicPolicy
	policy.ValidatorBlockRewardTarget = policy.ValidatorBlockRewardTarget.Add(chain.SeatValidatorShare.MulRaw(int64(seats)))
	policy.OracleBlockRewardTarget = policy.OracleBlockRewardTarget.Add(chain.SeatOracleShare.MulRaw(int64(seats)))
}

// encodeInto validates and marshals every module before writing any to
// appState, so a refusal leaves it as it was.
func (s *seatStates) encodeInto(cdc codec.Codec, appState map[string]json.RawMessage) error {
	var err error
	if s.auth.Accounts, err = authtypes.PackAccounts(authtypes.SanitizeGenesisAccounts(s.accounts)); err != nil {
		return fmt.Errorf("%s: %w", authtypes.ModuleName, err)
	}
	s.bank.Balances = banktypes.SanitizeGenesisBalances(s.bank.Balances)
	disbursementtypes.SortBeneficiaries(s.disbursement.Beneficiaries)
	encoded := make(map[string]json.RawMessage)
	for _, m := range s.modules() {
		if err := m.validate(); err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		raw, err := cdc.MarshalJSON(m.state)
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		encoded[m.name] = raw
	}
	maps.Copy(appState, encoded)
	return nil
}
