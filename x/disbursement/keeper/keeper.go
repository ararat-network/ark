package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdmath "math"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// Keeper owns funded commitments and their permanent audit record. Bank owns custody.
type Keeper struct {
	authority           string
	address             sdk.AccAddress
	bank                types.BankKeeper
	distribution        types.DistributionKeeper
	staking             types.StakingKeeper
	market              types.MarketKeeper
	assets              types.AssetReader
	account             types.AccountKeeper
	wasm                types.WasmKeeper
	Schema              collections.Schema
	Params              collections.Item[types.Params]
	OwnershipPolicy     collections.Item[types.OwnershipPolicy]
	Grants              collections.Map[uint64, types.Grant]
	Beneficiaries       collections.Map[sdk.AccAddress, types.Beneficiary]
	Members             collections.Map[sdk.AccAddress, uint64]
	Totals              collections.Map[string, types.DenomTotals]
	Issuance            collections.Item[types.Issuance]
	GrantsMandate       collections.Item[types.GrantsMandate]
	Journal             collections.Map[uint64, types.JournalEntry]
	NextGrantID         collections.Sequence
	NextJournalID       collections.Sequence
	GrantsByBeneficiary collections.Map[collections.Pair[sdk.AccAddress, uint64], bool]
	JournalByGrant      collections.Map[collections.Pair[uint64, uint64], bool]
	FoundingStake       collections.Item[math.Int]
	FounderPaid         collections.Item[math.Int]
	FounderRemaining    collections.Item[math.Int]
	VoidedTerms         collections.KeySet[uint64]
	MemberPool          collections.Item[types.PoolBalance]
	ContributorPool     collections.Item[types.PoolBalance]
	ConversionOrders    collections.Map[string, types.ConversionOrder]
	// TermGrants indexes committee awards by term with their amounts, so usage never decodes a grant.
	TermGrants collections.Map[collections.Pair[uint64, uint64], sdk.Coin]
}

// NewKeeper constructs the disbursement module with narrowly scoped capabilities.
func NewKeeper(cdc codec.BinaryCodec, service store.KVStoreService, authority string, account types.AccountKeeper, wasm types.WasmKeeper, bank types.BankKeeper, distribution types.DistributionKeeper, staking types.StakingKeeper, market types.MarketKeeper, assets types.AssetReader) *Keeper {
	address := account.GetModuleAddress(types.ModuleName)
	if address == nil {
		panic("disbursement module account is not registered")
	}
	sb := collections.NewSchemaBuilder(service)
	k := &Keeper{
		authority: authority, address: address, bank: bank, distribution: distribution, staking: staking, market: market, assets: assets, account: account, wasm: wasm,
		Params:              collections.NewItem(sb, collections.NewPrefix(0), "params", codec.CollValue[types.Params](cdc)),
		OwnershipPolicy:     collections.NewItem(sb, collections.NewPrefix(1), "ownership_policy", codec.CollValue[types.OwnershipPolicy](cdc)),
		Grants:              collections.NewMap(sb, collections.NewPrefix(2), "grants", collections.Uint64Key, codec.CollValue[types.Grant](cdc)),
		Beneficiaries:       collections.NewMap(sb, collections.NewPrefix(3), "beneficiaries", sdk.AccAddressKey, codec.CollValue[types.Beneficiary](cdc)),
		Members:             collections.NewMap(sb, collections.NewPrefix(4), "members", sdk.AccAddressKey, collections.Uint64Value),
		Totals:              collections.NewMap(sb, collections.NewPrefix(6), "totals", collections.StringKey, codec.CollValue[types.DenomTotals](cdc)),
		Issuance:            collections.NewItem(sb, collections.NewPrefix(7), "issuance", codec.CollValue[types.Issuance](cdc)),
		GrantsMandate:       collections.NewItem(sb, collections.NewPrefix(8), "grants_mandate", codec.CollValue[types.GrantsMandate](cdc)),
		Journal:             collections.NewMap(sb, collections.NewPrefix(9), "journal", collections.Uint64Key, codec.CollValue[types.JournalEntry](cdc)),
		NextGrantID:         collections.NewSequence(sb, collections.NewPrefix(10), "next_grant_id"),
		NextJournalID:       collections.NewSequence(sb, collections.NewPrefix(11), "next_journal_id"),
		GrantsByBeneficiary: collections.NewMap(sb, collections.NewPrefix(12), "grants_by_beneficiary", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key), collections.BoolValue),
		JournalByGrant:      collections.NewMap(sb, collections.NewPrefix(13), "journal_by_grant", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key), collections.BoolValue),
		FoundingStake:       collections.NewItem(sb, collections.NewPrefix(14), "founding_stake", sdk.IntValue),
		FounderPaid:         collections.NewItem(sb, collections.NewPrefix(15), "founder_paid", sdk.IntValue),
		FounderRemaining:    collections.NewItem(sb, collections.NewPrefix(16), "founder_remaining", sdk.IntValue),
		VoidedTerms:         collections.NewKeySet(sb, collections.NewPrefix(17), "voided_terms", collections.Uint64Key),
		MemberPool:          collections.NewItem(sb, collections.NewPrefix(18), "member_pool", codec.CollValue[types.PoolBalance](cdc)),
		ContributorPool:     collections.NewItem(sb, collections.NewPrefix(19), "contributor_pool", codec.CollValue[types.PoolBalance](cdc)),
		ConversionOrders:    collections.NewMap(sb, collections.NewPrefix(20), "conversion_orders", collections.StringKey, codec.CollValue[types.ConversionOrder](cdc)),
		TermGrants:          collections.NewMap(sb, collections.NewPrefix(21), "term_grants", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key), codec.CollValue[sdk.Coin](cdc)),
	}
	var err error
	k.Schema, err = sb.Build()
	if err != nil {
		panic(err)
	}
	return k
}

// totals supplies zero counters for a denomination without prior commitments.
func (k *Keeper) totals(ctx context.Context, denom string) (types.DenomTotals, error) {
	t, err := k.Totals.Get(ctx, denom)
	if errors.Is(err, collections.ErrNotFound) {
		return types.EmptyTotals(denom), nil
	}
	return t, err
}

// unallocated is custody outside reservations, pools, and conversion orders.
func (k *Keeper) unallocated(ctx context.Context, denom string) (math.Int, error) {
	t, err := k.totals(ctx, denom)
	if err != nil {
		return math.Int{}, err
	}
	held := t.Reserved
	if denom == chain.NoahBaseDenom {
		committed, err := k.committed(ctx)
		if err != nil {
			return math.Int{}, err
		}
		if held, err = held.SafeAdd(committed); err != nil {
			return math.Int{}, err
		}
	}
	balance := k.bank.GetBalance(ctx, k.address, denom).Amount
	if balance.LT(held) {
		return math.Int{}, errors.New("disbursement custody is below its commitments")
	}
	return balance.Sub(held), nil
}

// committed is the NOAH held in both pools and in conversion orders.
func (k *Keeper) committed(ctx context.Context) (math.Int, error) {
	total, err := k.converting(ctx)
	if err != nil {
		return math.Int{}, err
	}
	for _, item := range []collections.Item[types.PoolBalance]{k.MemberPool, k.ContributorPool} {
		p, err := item.Get(ctx)
		if err != nil {
			return math.Int{}, err
		}
		if total, err = total.SafeAdd(p.Unallocated); err != nil {
			return math.Int{}, err
		}
	}
	return total, nil
}

// converting sums the NOAH conversion orders still hold.
func (k *Keeper) converting(ctx context.Context) (math.Int, error) {
	total := math.ZeroInt()
	err := k.ConversionOrders.Walk(ctx, nil, func(_ string, o types.ConversionOrder) (bool, error) {
		var err error
		total, err = total.SafeAdd(o.Remaining)
		return err != nil, err
	})
	return total, err
}

// pool returns the store item behind a distribution pool.
func (k *Keeper) pool(p types.Pool) (collections.Item[types.PoolBalance], error) {
	switch p {
	case types.Pool_POOL_MEMBERS:
		return k.MemberPool, nil
	case types.Pool_POOL_CONTRIBUTORS:
		return k.ContributorPool, nil
	}
	return collections.Item[types.PoolBalance]{}, errors.New("unknown distribution pool")
}

// source names the pool a grant draws from; POOL_UNSPECIFIED is custody outside the pools.
func source(kind types.GrantKind, denom string) types.Pool {
	switch {
	case denom != chain.NoahBaseDenom:
		return types.Pool_POOL_UNSPECIFIED
	case kind == types.GrantKind_GRANT_KIND_MEMBER:
		return types.Pool_POOL_MEMBERS
	default:
		return types.Pool_POOL_CONTRIBUTORS
	}
}

// draw commits amount from its source: a pool's open tranche, or unallocated custody otherwise.
func (k *Keeper) draw(ctx context.Context, from types.Pool, amount sdk.Coin) error {
	if from == types.Pool_POOL_UNSPECIFIED {
		available, err := k.unallocated(ctx, amount.Denom)
		if err != nil {
			return err
		}
		if available.LT(amount.Amount) {
			return errors.New("insufficient unallocated disbursement funds")
		}
		return nil
	}
	item, err := k.pool(from)
	if err != nil {
		return err
	}
	p, err := item.Get(ctx)
	if err != nil {
		return err
	}
	if p.Open.LT(amount.Amount) {
		return fmt.Errorf("%s exceeds the open %s tranche", amount, from)
	}
	p.Unallocated, p.Open = p.Unallocated.Sub(amount.Amount), p.Open.Sub(amount.Amount)
	return item.Set(ctx, p)
}

// refund returns unearned NOAH to its pool's open tranche, the step the plan charged it to.
func (k *Keeper) refund(ctx context.Context, to types.Pool, amount math.Int) error {
	item, err := k.pool(to)
	if err != nil {
		return err
	}
	p, err := item.Get(ctx)
	if err != nil {
		return err
	}
	if p.Unallocated, err = p.Unallocated.SafeAdd(amount); err != nil {
		return err
	}
	if p.Open, err = p.Open.SafeAdd(amount); err != nil {
		return err
	}
	return item.Set(ctx, p)
}

func blockTime(ctx context.Context) (uint64, error) {
	at := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if at < 0 {
		return 0, errors.New("negative disbursement block time")
	}
	return uint64(at), nil
}

func nextID(ctx context.Context, seq collections.Sequence) (uint64, error) {
	id, err := seq.Peek(ctx)
	if err != nil {
		return 0, err
	}
	if id == 0 || id == stdmath.MaxUint64 {
		return 0, errors.New("disbursement sequence exhausted")
	}
	return id, seq.Set(ctx, id+1)
}

// suspended reports whether a recorded suspension is effective: one made under a voided term is not.
func (k *Keeper) suspended(ctx context.Context, g types.Grant) (bool, error) {
	if g.Suspension == nil {
		return false, nil
	}
	voided, err := k.VoidedTerms.Has(ctx, g.Suspension.Term)
	return !voided, err
}

// record is the only journal writer; callers commit it with the state and bank movement.
func (k *Keeper) record(ctx context.Context, grantID uint64, action, actor string, amount sdk.Coin, details any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if len(raw) > types.MaxJournalDetailsBytes {
		return errors.New("journal details exceed bound")
	}
	at, err := blockTime(ctx)
	if err != nil {
		return err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return errors.New("disbursement operations require a positive block height")
	}
	id, err := nextID(ctx, k.NextJournalID)
	if err != nil {
		return err
	}
	entry := types.JournalEntry{Id: id, GrantId: grantID, Action: action, Actor: actor, Height: uint64(height), Time: at, Amount: amount, Details: string(raw)}
	if err := k.Journal.Set(ctx, id, entry); err != nil {
		return err
	}
	if grantID != 0 {
		if err := k.JournalByGrant.Set(ctx, collections.Join(grantID, id), true); err != nil {
			return err
		}
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventOperation{Entry: entry}); err != nil {
		return fmt.Errorf("disbursement journal event: %w", err)
	}
	return nil
}
