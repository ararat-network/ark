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
	assets              types.AssetReader
	Schema              collections.Schema
	Params              collections.Item[types.Params]
	OwnershipPolicy     collections.Item[types.OwnershipPolicy]
	Grants              collections.Map[uint64, types.Grant]
	Beneficiaries       collections.Map[sdk.AccAddress, types.Beneficiary]
	Members             collections.Map[sdk.AccAddress, uint64]
	Founders            collections.Map[sdk.ValAddress, types.Founder]
	Totals              collections.Map[string, types.DenomTotals]
	Issuance            collections.Item[types.Issuance]
	RegistrarEpochs     collections.Map[sdk.AccAddress, uint64]
	Journal             collections.Map[uint64, types.JournalEntry]
	NextGrantID         collections.Sequence
	NextJournalID       collections.Sequence
	GrantsByBeneficiary collections.Map[collections.Pair[sdk.AccAddress, uint64], bool]
	JournalByGrant      collections.Map[collections.Pair[uint64, uint64], bool]
	FoundingStake       collections.Item[math.Int]
	FounderPaid         collections.Item[math.Int]
	FounderRemaining    collections.Item[math.Int]
}

// NewKeeper constructs the disbursement module with narrowly scoped capabilities.
func NewKeeper(cdc codec.BinaryCodec, service store.KVStoreService, authority string, account types.AccountKeeper, bank types.BankKeeper, distribution types.DistributionKeeper, staking types.StakingKeeper, assets types.AssetReader) *Keeper {
	address := account.GetModuleAddress(types.ModuleName)
	if address == nil {
		panic("disbursement module account is not registered")
	}
	sb := collections.NewSchemaBuilder(service)
	k := &Keeper{
		authority: authority, address: address, bank: bank, distribution: distribution, staking: staking, assets: assets,
		Params:              collections.NewItem(sb, collections.NewPrefix(0), "params", codec.CollValue[types.Params](cdc)),
		OwnershipPolicy:     collections.NewItem(sb, collections.NewPrefix(1), "ownership_policy", codec.CollValue[types.OwnershipPolicy](cdc)),
		Grants:              collections.NewMap(sb, collections.NewPrefix(2), "grants", collections.Uint64Key, codec.CollValue[types.Grant](cdc)),
		Beneficiaries:       collections.NewMap(sb, collections.NewPrefix(3), "beneficiaries", sdk.AccAddressKey, codec.CollValue[types.Beneficiary](cdc)),
		Members:             collections.NewMap(sb, collections.NewPrefix(4), "members", sdk.AccAddressKey, collections.Uint64Value),
		Founders:            collections.NewMap(sb, collections.NewPrefix(5), "founders", sdk.ValAddressKey, codec.CollValue[types.Founder](cdc)),
		Totals:              collections.NewMap(sb, collections.NewPrefix(6), "totals", collections.StringKey, codec.CollValue[types.DenomTotals](cdc)),
		Issuance:            collections.NewItem(sb, collections.NewPrefix(7), "issuance", codec.CollValue[types.Issuance](cdc)),
		RegistrarEpochs:     collections.NewMap(sb, collections.NewPrefix(8), "registrar_epochs", sdk.AccAddressKey, collections.Uint64Value),
		Journal:             collections.NewMap(sb, collections.NewPrefix(9), "journal", collections.Uint64Key, codec.CollValue[types.JournalEntry](cdc)),
		NextGrantID:         collections.NewSequence(sb, collections.NewPrefix(10), "next_grant_id"),
		NextJournalID:       collections.NewSequence(sb, collections.NewPrefix(11), "next_journal_id"),
		GrantsByBeneficiary: collections.NewMap(sb, collections.NewPrefix(12), "grants_by_beneficiary", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key), collections.BoolValue),
		JournalByGrant:      collections.NewMap(sb, collections.NewPrefix(13), "journal_by_grant", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key), collections.BoolValue),
		FoundingStake:       collections.NewItem(sb, collections.NewPrefix(14), "founding_stake", sdk.IntValue),
		FounderPaid:         collections.NewItem(sb, collections.NewPrefix(15), "founder_paid", sdk.IntValue),
		FounderRemaining:    collections.NewItem(sb, collections.NewPrefix(16), "founder_remaining", sdk.IntValue),
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

func (k *Keeper) unallocated(ctx context.Context, denom string) (math.Int, error) {
	t, err := k.totals(ctx, denom)
	if err != nil {
		return math.Int{}, err
	}
	balance := k.bank.GetBalance(ctx, k.address, denom).Amount
	if balance.LT(t.Reserved) {
		return math.Int{}, errors.New("disbursement custody is below its reservation")
	}
	return balance.Sub(t.Reserved), nil
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

func (k *Keeper) epoch(ctx context.Context, address sdk.AccAddress) (uint64, error) {
	e, err := k.RegistrarEpochs.Get(ctx, address)
	if errors.Is(err, collections.ErrNotFound) {
		return 0, nil
	}
	return e, err
}

func (k *Keeper) suspended(ctx context.Context, g types.Grant) (bool, error) {
	if g.Suspension == nil {
		return false, nil
	}
	addr, err := chain.ParseCanonicalAccountAddress("suspending registrar", g.Suspension.Registrar)
	if err != nil {
		return false, err
	}
	epoch, err := k.epoch(ctx, addr)
	return epoch == g.Suspension.Epoch, err
}

func (k *Keeper) founder(ctx context.Context, beneficiary sdk.AccAddress) (*types.Founder, error) {
	f, err := k.Founders.Get(ctx, sdk.ValAddress(beneficiary))
	if errors.Is(err, collections.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
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
