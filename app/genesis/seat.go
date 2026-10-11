// Package genesis owns the launch artefact beside it, genesis.json, and the
// one edit assembly makes to it: granting the validator seats and the
// founder's awards.
package genesis

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
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
// operator's Disbursement beneficiary, a record nothing reads (D115). The
// founder's grant and stream are written beside the seats as governance
// awards from the contributor pool (D136).
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
	if err := s.awardFounder(founder, genesisTime); err != nil {
		return fmt.Errorf("founder's awards: %w", err)
	}
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
	// reference is Oracle's reference denomination, read to value the
	// founder's stream and never written.
	reference string
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
	var oracle oracletypes.GenesisState
	if raw := appState[oracletypes.ModuleName]; len(raw) > 0 {
		if err := cdc.UnmarshalJSON(raw, &oracle); err != nil {
			return nil, fmt.Errorf("%s: %w", oracletypes.ModuleName, err)
		}
	}
	s.reference = oracle.ReferenceDenom
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

// founderAwardHeight stamps the founder's awards at the chain's first block,
// which import admits though InitChain runs at height zero.
const founderAwardHeight = 1

// awardFounder writes the founder's grant and stream as governance awards
// made in the first block, clocked from genesisTime on the file's params,
// reserved in the contributor book and journaled as the keeper's create
// would (D136). Genesis validation reads neither cap nor Bank, so both caps
// and the contributor account's backing are checked here.
func (s *seatStates) awardFounder(founder sdk.AccAddress, genesisTime time.Time) error {
	start := genesisTime.Unix()
	if start < 0 {
		return fmt.Errorf("genesis time %s precedes the Unix epoch", genesisTime)
	}
	params := s.disbursement.Params
	grant := chain.NativeBaseAmount(chain.FounderGrantNoah)
	if grant.GT(params.OwnershipCap) {
		return fmt.Errorf("the grant of %s exceeds the ownership cap of %s", grant, params.OwnershipCap)
	}
	if s.reference != chain.XDRBaseDenom {
		return fmt.Errorf("the stream is valued at the bootstrap price in %s, not the file's reference %q", chain.XDRBaseDenom, s.reference)
	}
	stream, err := disbursementtypes.CompensationSchedule(params.CompensationPeriod, chain.FounderStreamPeriods)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	pay := chain.NativeBaseAmount(chain.FounderStreamNoah)
	// NOAH per XDR, rounded up as an award's value is.
	value := math.LegacyNewDecFromInt(pay).Quo(math.LegacyMustNewDecFromStr(chain.BootstrapNoahPerXDR)).Ceil().TruncateInt()
	// The clock starts at the award, so the first window carries no backdate.
	charge, err := disbursementtypes.CompensationCharge(value, stream.Duration(), params.CompensationWindow)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	if charge.GT(params.CompensationCap) {
		return fmt.Errorf("the stream charges %s a window, past the compensation cap of %s", charge, params.CompensationCap)
	}
	for _, g := range []disbursementtypes.Grant{
		{
			Kind:       disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP,
			Amount:     chain.NoahCoin(grant),
			Reference:  chain.FounderGrantReference,
			Schedule:   params.OwnershipSchedule,
			AwardValue: math.ZeroInt(),
		},
		{
			Kind:       disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION,
			Amount:     chain.NoahCoin(pay),
			Reference:  chain.FounderStreamReference,
			Schedule:   stream,
			AwardValue: value,
		},
	} {
		if err := s.award(founder, g, uint64(start)); err != nil {
			return err
		}
	}
	return s.backContributors()
}

// award stamps g as governance's award to beneficiary at the first block,
// queues it for its first boundary, and records it in the journal, the
// contributor book, and the beneficiary's committed ownership.
func (s *seatStates) award(beneficiary sdk.AccAddress, g disbursementtypes.Grant, at uint64) error {
	d := &s.disbursement
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	g.Id = d.NextGrantId
	g.Beneficiary, g.Payee = beneficiary.String(), beneficiary.String()
	g.StartTime, g.CreatedTime, g.CreatedHeight, g.CreatedBy = at, at, founderAwardHeight, authority
	g.Paid, g.Remaining = math.ZeroInt(), g.Amount.Amount
	_, next, err := g.Accrued(at)
	if err != nil {
		return err
	}
	g.DueTime = next
	d.Grants = append(d.Grants, g)
	d.GrantJournal = append(d.GrantJournal, disbursementtypes.GrantEntry{
		Id:      d.NextGrantEntryId,
		GrantId: g.Id,
		Action:  disbursementtypes.GrantAction_GRANT_ACTION_CREATED,
		Actor:   authority,
		Height:  founderAwardHeight,
		Time:    at,
		Amount:  g.Amount,
		Offer:   math.ZeroInt(),
	})
	d.NextGrantId++
	d.NextGrantEntryId++
	account := disbursementtypes.Account(g.Kind)
	i := slices.IndexFunc(d.Books, func(b disbursementtypes.AccountBook) bool { return b.Account == account })
	if i < 0 {
		d.Books = append(d.Books, disbursementtypes.EmptyBook(account))
		slices.SortFunc(d.Books, func(a, b disbursementtypes.AccountBook) int { return strings.Compare(a.Account, b.Account) })
		i = slices.IndexFunc(d.Books, func(b disbursementtypes.AccountBook) bool { return b.Account == account })
	}
	if err := d.Books[i].Reserve(g.Amount); err != nil {
		return err
	}
	if g.Kind != disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP {
		return nil
	}
	j := slices.IndexFunc(d.Beneficiaries, func(p disbursementtypes.Beneficiary) bool { return p.Address == g.Beneficiary })
	if j < 0 {
		return fmt.Errorf("%s holds no beneficiary record", g.Beneficiary)
	}
	d.Beneficiaries[j].OwnershipCommitted = d.Beneficiaries[j].OwnershipCommitted.Add(g.Amount.Amount)
	return nil
}

// backContributors requires the contributor account's balance to cover its
// reservations, which import checks against Bank.
func (s *seatStates) backContributors() error {
	i := slices.IndexFunc(s.disbursement.Books, func(b disbursementtypes.AccountBook) bool {
		return b.Account == disbursementtypes.ContributorsName
	})
	if i < 0 {
		return nil
	}
	reserved := s.disbursement.Books[i].Reserved
	address := authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String()
	held := sdk.NewCoins()
	if j := slices.IndexFunc(s.bank.Balances, func(b banktypes.Balance) bool { return b.Address == address }); j >= 0 {
		held = s.bank.Balances[j].Coins
	}
	if !held.IsAllGTE(reserved) {
		return fmt.Errorf("the contributor account holds %s, short of the %s its grants reserve", held, reserved)
	}
	return nil
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
