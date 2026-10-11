package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

// Keeper owns the Claims mandate, the permanent claim record, and the
// Insurance reservation. Custody itself remains in Bank.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string

	accountKeeper types.AccountKeeper
	wasmKeeper    types.WasmKeeper
	bankKeeper    types.BankKeeper
	oracleKeeper  types.OracleKeeper
	assetKeeper   types.AssetKeeper
	// marketKeeper is set after construction: Treasury injects Claims and Market injects Treasury.
	marketKeeper types.MarketKeeper

	// insuranceAddress is resolved and checked against the registered module account at
	// construction. SendRestriction uses this verified address rather than deriving an unchecked
	// one by name.
	insuranceAddress sdk.AccAddress

	Schema              collections.Schema
	Params              collections.Item[types.Params]
	ClaimsMandate       collections.Item[types.ClaimsMandate]
	ClaimsAllowanceUsed collections.Item[math.Int]
	// InsuranceReserved sums pending claims by denomination, positive entries only.
	InsuranceReserved collections.Map[string, math.Int]
	NextClaimID       collections.Sequence
	Claims            collections.Map[uint64, types.Claim]
	// DueClaims indexes pending claims by (closing time, claim ID) so
	// EndBlocker can settle the ones that have come due without walking the
	// permanent record. A claim leaves the index the block it is paid or
	// cancelled; the record it points at is never removed.
	DueClaims collections.KeySet[collections.Pair[uint64, uint64]]
}

// NewKeeper creates a Claims keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	wasmKeeper types.WasmKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	assetKeeper types.AssetKeeper,
) *Keeper {
	insuranceAddress := accountKeeper.GetModuleAddress(types.InsuranceName)
	if insuranceAddress == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.InsuranceName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:              cdc,
		storeService:     storeService,
		authority:        authority,
		accountKeeper:    accountKeeper,
		wasmKeeper:       wasmKeeper,
		bankKeeper:       bankKeeper,
		oracleKeeper:     oracleKeeper,
		assetKeeper:      assetKeeper,
		insuranceAddress: insuranceAddress,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		ClaimsMandate: collections.NewItem(
			sb,
			types.ClaimsMandateKey,
			"claims_mandate",
			codec.CollValue[types.ClaimsMandate](cdc),
		),
		ClaimsAllowanceUsed: collections.NewItem(
			sb,
			types.ClaimsAllowanceUsedKey,
			"claims_allowance_used",
			sdk.IntValue,
		),
		InsuranceReserved: collections.NewMap(
			sb,
			types.InsuranceReservedKey,
			"insurance_reserved",
			collections.StringKey,
			sdk.IntValue,
		),
		NextClaimID: collections.NewSequence(
			sb,
			types.NextClaimIDKey,
			"next_claim_id",
		),
		Claims: collections.NewMap(
			sb,
			types.ClaimsKey,
			"claims",
			collections.Uint64Key,
			codec.CollValue[types.Claim](cdc),
		),
		DueClaims: collections.NewKeySet(
			sb,
			types.DueClaimsKey,
			"due_claims",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetMarketKeeper wires Market after construction, closing the loop depinject cannot: Treasury
// injects Claims and Market injects Treasury.
func (k *Keeper) SetMarketKeeper(market types.MarketKeeper) {
	k.marketKeeper = market
}

// RecognisedCapital returns the Insurance NOAH balance less NOAH reservations. Other custody is Ark
// paper, a liability until burned, and counts for nothing (D28). Submission, import, and settlement
// preserve reserved <= balance; the checked error rejects corrupt state before Treasury can fund a
// false gap. See x/claims/README.md for the invariant.
func (k *Keeper) RecognisedCapital(ctx context.Context) (math.Int, error) {
	return k.Uncommitted(ctx, chain.NoahBaseDenom)
}

// Uncommitted is Insurance custody in denom outside pending reservations. A balance below them is a
// backing failure.
func (k *Keeper) Uncommitted(ctx context.Context, denom string) (math.Int, error) {
	reserved, err := k.reserved(ctx, denom)
	if err != nil {
		return math.Int{}, err
	}
	balance := k.balance(ctx, denom)
	if reserved.GT(balance) {
		return math.Int{}, fmt.Errorf(
			"insurance reservation %s exceeds the Insurance balance %s",
			sdk.NewCoin(denom, reserved),
			sdk.NewCoin(denom, balance),
		)
	}
	return balance.Sub(reserved), nil
}

// reserved reads pending reservations in denom, zero when there are none.
func (k *Keeper) reserved(ctx context.Context, denom string) (math.Int, error) {
	reserved, err := k.InsuranceReserved.Get(ctx, denom)
	if errors.Is(err, collections.ErrNotFound) {
		return math.ZeroInt(), nil
	}
	if err != nil {
		return math.Int{}, fmt.Errorf("getting %s Insurance reservation: %w", denom, err)
	}
	return reserved, nil
}

// reserve adds a pending claim to its denomination's reservation, refusing one Insurance cannot
// cover, so the reservation never exceeds the balance it encumbers.
func (k *Keeper) reserve(ctx context.Context, amount sdk.Coin) error {
	reserved, err := k.reserved(ctx, amount.Denom)
	if err != nil {
		return err
	}
	next, err := reserved.SafeAdd(amount.Amount)
	if err != nil {
		return fmt.Errorf("adding %s Insurance reservation: %w", amount.Denom, err)
	}
	if balance := k.balance(ctx, amount.Denom); balance.LT(next) {
		return fmt.Errorf(
			"insurance balance %s cannot cover Insurance reservation %s",
			sdk.NewCoin(amount.Denom, balance),
			sdk.NewCoin(amount.Denom, next),
		)
	}
	if err := k.InsuranceReserved.Set(ctx, amount.Denom, next); err != nil {
		return fmt.Errorf("setting %s Insurance reservation: %w", amount.Denom, err)
	}
	return nil
}

// release takes a claim out of its denomination's reservation, removing the entry at zero.
func (k *Keeper) release(ctx context.Context, amount sdk.Coin) error {
	reserved, err := k.reserved(ctx, amount.Denom)
	if err != nil {
		return err
	}
	// Unreachable: import requires each denomination's reservation to equal its pending claims' sum,
	// and every claim releases once. Kept because settlement would otherwise store a negative figure.
	next, err := reserved.SafeSub(amount.Amount)
	if err != nil || next.IsNegative() {
		return fmt.Errorf("%s Insurance reservation %s is below %s", amount.Denom, reserved, amount.Amount)
	}
	if next.IsZero() {
		if err := k.InsuranceReserved.Remove(ctx, amount.Denom); err != nil {
			return fmt.Errorf("clearing %s Insurance reservation: %w", amount.Denom, err)
		}
		return nil
	}
	if err := k.InsuranceReserved.Set(ctx, amount.Denom, next); err != nil {
		return fmt.Errorf("setting %s Insurance reservation: %w", amount.Denom, err)
	}
	return nil
}

// reservations lists pending reservations as coins, one per denomination.
func (k *Keeper) reservations(ctx context.Context) (sdk.Coins, error) {
	reserved := sdk.NewCoins()
	if err := k.InsuranceReserved.Walk(ctx, nil, func(denom string, amount math.Int) (bool, error) {
		reserved = reserved.Add(sdk.NewCoin(denom, amount))
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("getting Insurance reservations: %w", err)
	}
	return reserved, nil
}

// balance reads the live Insurance balance in denom from Bank.
func (k *Keeper) balance(ctx context.Context, denom string) math.Int {
	return k.bankKeeper.GetBalance(ctx, k.insuranceAddress, denom).Amount
}

// Logger returns a module-specific logger.
func (k *Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
