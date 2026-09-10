package keeper

import (
	"context"
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

	// insuranceAddress is the custody account's address, captured once at
	// construction from the account keeper that asserted its registration.
	// SendRestriction matches against it rather than re-deriving it by name: a
	// name the account keeper does not know is a panic at startup, where
	// deriving it would silently produce an address matching no account.
	insuranceAddress sdk.AccAddress

	Schema              collections.Schema
	Params              collections.Item[types.Params]
	ClaimsMandate       collections.Item[types.ClaimsMandate]
	ClaimsAllowanceUsed collections.Item[math.Int]
	InsuranceReserved   collections.Item[math.Int]
	NextClaimID         collections.Sequence
	Claims              collections.Map[uint64, types.Claim]
	// DueClaims indexes pending claims by (closing height, claim ID) so
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
		InsuranceReserved: collections.NewItem(
			sb,
			types.InsuranceReservedKey,
			"insurance_reserved",
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

// RecognisedCapital reports the Insurance capital available to cover new loss,
// satisfying Treasury's expected ClaimsKeeper. Approved pending claims are
// encumbered and cannot simultaneously cover another loss, so they are
// excluded (ECONOMIC_DESIGN.md §7.3).
//
// Treasury owns the requirement this answers against; this module owns only
// what the fund is currently worth toward it.
//
// A reservation above the balance is refused rather than reported as negative
// capital. It cannot arise: submission refuses a claim the balance cannot cover
// on top of what is already reserved, genesis refuses an import where it does
// not, payment releases exactly what it sends, failure and cancellation release
// without sending, and the account holds no Burner permission while no other
// module spends from it — so every transition preserves reserved <= balance
// from the point submission establishes it.
//
// Checked because of where the figure goes. Treasury sizes the Insurance gap
// from it during settlement, so a reservation the fund cannot back would route
// real expansion principal into a fund whose own books are already known to be
// wrong, and it would do so silently: a negative merely widens the gap, which
// looks like an underfunded fund rather than a broken one. Refusing halts the
// block instead, which is the outcome corrupt state deserves over a settlement
// that proceeds on it.
func (k Keeper) RecognisedCapital(ctx context.Context) (math.Int, error) {
	reserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	balance := k.balance(ctx)
	if reserved.GT(balance) {
		return math.Int{}, fmt.Errorf(
			"insurance reservation %s exceeds the Insurance balance %s",
			reserved,
			balance,
		)
	}

	return balance.Sub(reserved), nil
}

// balance reads the live Insurance NOAH balance from Bank.
func (k Keeper) balance(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, k.insuranceAddress, chain.NoahBaseDenom).Amount
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// InsuranceBalance returns the Insurance fund's NOAH balance.
func (k Keeper) InsuranceBalance(ctx context.Context) math.Int {
	return k.balance(ctx)
}
