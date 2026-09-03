package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// Keeper owns the strategic Reserve custody account, the committee mandate
// governing deployment, and the accounting ledger recording what the Reserve
// holds off chain.
type Keeper struct {
	cdc            codec.BinaryCodec
	authority      string
	reserveAddress sdk.AccAddress

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	oracleKeeper  types.OracleKeeper
	assetKeeper   types.AssetKeeper
	// treasuryReader is set after construction because Treasury injects this
	// module. It is nil-checked at its call sites, so a chain that never wires
	// it still runs every other Reserve path.
	treasuryReader types.TreasuryCapitalReader

	Schema            collections.Schema
	Mandate           collections.Item[types.ReserveMandate]
	AllowanceUsed     collections.Item[math.Int]
	NextPositionID    collections.Sequence
	OpenPositions     collections.Map[uint64, types.Position]
	ClosedPositions   collections.Map[uint64, types.Position]
	NextEntryID       collections.Sequence
	Ledger            collections.Map[uint64, types.AccountingEntry]
	RecognitionPolicy collections.Map[string, types.EligibilityEntry]
	ReversedReturns   collections.KeySet[uint64]
}

// NewKeeper creates a Reserve keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
	assetKeeper types.AssetKeeper,
) *Keeper {
	reserveAddress := accountKeeper.GetModuleAddress(types.StrategicReserveName)
	if reserveAddress == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.StrategicReserveName))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:            cdc,
		authority:      authority,
		reserveAddress: reserveAddress,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		oracleKeeper:   oracleKeeper,
		assetKeeper:    assetKeeper,
		Mandate: collections.NewItem(
			sb,
			types.MandateKey,
			"mandate",
			codec.CollValue[types.ReserveMandate](cdc),
		),
		AllowanceUsed: collections.NewItem(
			sb,
			types.AllowanceUsedKey,
			"allowance_used",
			sdk.IntValue,
		),
		NextPositionID: collections.NewSequence(
			sb,
			types.NextPositionIDKey,
			"next_position_id",
		),
		OpenPositions: collections.NewMap(
			sb,
			types.OpenPositionsKey,
			"open_positions",
			collections.Uint64Key,
			codec.CollValue[types.Position](cdc),
		),
		ClosedPositions: collections.NewMap(
			sb,
			types.ClosedPositionsKey,
			"closed_positions",
			collections.Uint64Key,
			codec.CollValue[types.Position](cdc),
		),
		NextEntryID: collections.NewSequence(
			sb,
			types.NextEntryIDKey,
			"next_entry_id",
		),
		Ledger: collections.NewMap(
			sb,
			types.LedgerKey,
			"ledger",
			collections.Uint64Key,
			codec.CollValue[types.AccountingEntry](cdc),
		),
		RecognitionPolicy: collections.NewMap(
			sb,
			types.RecognitionPolicyKey,
			"recognition_policy",
			collections.StringKey,
			codec.CollValue[types.EligibilityEntry](cdc),
		),
		ReversedReturns: collections.NewKeySet(
			sb,
			types.ReversedReturnsKey,
			"reversed_returns",
			collections.Uint64Key,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// RecognisedCapital reports the Reserve capital counting toward its target,
// satisfying Treasury's expected ReserveKeeper: the on-chain NOAH balance at
// par, plus one credit per eligibility-listed asset, solved jointly against
// every asset's cap ratio by SolveRecognition. A dark or stale feed, an
// impairment, a zero haircut, or an absent entry each zero one asset's credit
// and touch nothing else.
func (k Keeper) RecognisedCapital(ctx context.Context) (math.Int, error) {
	assets, err := k.AssetRecognitions(ctx)
	if err != nil {
		return math.Int{}, err
	}
	return sumRecognised(k.balance(ctx), assets)
}

// SetTreasuryCapitalReader wires the Treasury reader after construction,
// closing the loop depinject cannot: Treasury injects this module, so this
// module cannot inject Treasury.
func (k *Keeper) SetTreasuryCapitalReader(reader types.TreasuryCapitalReader) {
	k.treasuryReader = reader
}

// balance reads the live Reserve NOAH balance from Bank.
func (k Keeper) balance(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, k.reserveAddress, chain.NoahBaseDenom).Amount
}

// ReserveBalance returns the Reserve's NOAH balance.
func (k Keeper) ReserveBalance(ctx context.Context) math.Int {
	return k.balance(ctx)
}
