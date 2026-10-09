package keeper

import (
	"context"
	"errors"
	"fmt"
	stdmath "math"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/types"
)

// Keeper owns funded commitments and their permanent audit record. Bank owns custody, in the member
// and contributor accounts.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string
	// poolAddresses names each verified pool account by its raw address bytes, for the send restriction
	// and the payee checks.
	poolAddresses map[string]string

	accountKeeper      types.AccountKeeper
	wasmKeeper         types.WasmKeeper
	bankKeeper         types.BankKeeper
	distributionKeeper types.DistributionKeeper
	marketKeeper       types.MarketKeeper
	assetKeeper        types.AssetKeeper
	oracleKeeper       types.OracleKeeper

	Schema                 collections.Schema
	Params                 collections.Item[types.Params]
	GrantsMandate          collections.Item[types.GrantsMandate]
	RegistrarMandate       collections.Item[types.RegistrarMandate]
	CompensationUsed       collections.Item[math.Int]
	MemberTranche          collections.Item[math.Int]
	Beneficiaries          collections.Map[sdk.AccAddress, types.Beneficiary]
	Grants                 collections.Map[uint64, types.Grant]
	Books                  collections.Map[string, types.AccountBook]
	GrantJournal           collections.Map[collections.Pair[uint64, uint64], types.GrantEntry]
	BeneficiaryJournal     collections.Map[collections.Pair[sdk.AccAddress, uint64], types.BeneficiaryEntry]
	NextGrantID            collections.Sequence
	NextGrantEntryID       collections.Sequence
	NextBeneficiaryEntryID collections.Sequence

	// Indexes over the grants, rebuilt at import and never exported.
	// GrantsByBeneficiary lists each beneficiary's grants.
	GrantsByBeneficiary collections.Map[collections.Pair[sdk.AccAddress, uint64], bool]
	// Members maps each member to its member grant.
	Members collections.Map[sdk.AccAddress, uint64]
	// CommitteeTermGrants indexes awards by committee term and RegistrarTermGrants registrations by
	// registrar term, for the term queries.
	CommitteeTermGrants collections.KeySet[collections.Pair[uint64, uint64]]
	RegistrarTermGrants collections.KeySet[collections.Pair[uint64, uint64]]
	// Due orders each grant that owes principal by (due_time, grant ID), the order the EndBlocker pays in.
	Due collections.KeySet[collections.Pair[uint64, uint64]]
}

// NewKeeper constructs the disbursement module with narrowly scoped capabilities.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	accountKeeper types.AccountKeeper,
	wasmKeeper types.WasmKeeper,
	bankKeeper types.BankKeeper,
	distributionKeeper types.DistributionKeeper,
	marketKeeper types.MarketKeeper,
	assetKeeper types.AssetKeeper,
	oracleKeeper types.OracleKeeper,
) *Keeper {
	poolAddresses := make(map[string]string, 2)
	for _, moduleName := range types.PoolAccountNames() {
		address := accountKeeper.GetModuleAddress(moduleName)
		if address == nil {
			panic(fmt.Sprintf("%s module account has not been set", moduleName))
		}
		poolAddresses[string(address)] = moduleName
	}
	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:                cdc,
		storeService:       storeService,
		authority:          authority,
		poolAddresses:      poolAddresses,
		accountKeeper:      accountKeeper,
		wasmKeeper:         wasmKeeper,
		bankKeeper:         bankKeeper,
		distributionKeeper: distributionKeeper,
		marketKeeper:       marketKeeper,
		assetKeeper:        assetKeeper,
		oracleKeeper:       oracleKeeper,

		Params:                 collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		GrantsMandate:          collections.NewItem(sb, types.GrantsMandateKey, "grants_mandate", codec.CollValue[types.GrantsMandate](cdc)),
		RegistrarMandate:       collections.NewItem(sb, types.RegistrarMandateKey, "registrar_mandate", codec.CollValue[types.RegistrarMandate](cdc)),
		CompensationUsed:       collections.NewItem(sb, types.CompensationUsedKey, "compensation_used", sdk.IntValue),
		MemberTranche:          collections.NewItem(sb, types.MemberTrancheKey, "member_tranche", sdk.IntValue),
		Beneficiaries:          collections.NewMap(sb, types.BeneficiariesKey, "beneficiaries", sdk.AccAddressKey, codec.CollValue[types.Beneficiary](cdc)),
		Grants:                 collections.NewMap(sb, types.GrantsKey, "grants", collections.Uint64Key, codec.CollValue[types.Grant](cdc)),
		Books:                  collections.NewMap(sb, types.BooksKey, "books", collections.StringKey, codec.CollValue[types.AccountBook](cdc)),
		GrantJournal:           collections.NewMap(sb, types.GrantJournalKey, "grant_journal", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key), codec.CollValue[types.GrantEntry](cdc)),
		BeneficiaryJournal:     collections.NewMap(sb, types.BeneficiaryJournalKey, "beneficiary_journal", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key), codec.CollValue[types.BeneficiaryEntry](cdc)),
		NextGrantID:            collections.NewSequence(sb, types.NextGrantIDKey, "next_grant_id"),
		NextGrantEntryID:       collections.NewSequence(sb, types.NextGrantEntryIDKey, "next_grant_entry_id"),
		NextBeneficiaryEntryID: collections.NewSequence(sb, types.NextBeneficiaryEntryIDKey, "next_beneficiary_entry_id"),

		GrantsByBeneficiary: collections.NewMap(sb, types.GrantsByBeneficiaryKey, "grants_by_beneficiary", collections.PairKeyCodec(sdk.AccAddressKey, collections.Uint64Key), collections.BoolValue),
		Members:             collections.NewMap(sb, types.MembersKey, "members", sdk.AccAddressKey, collections.Uint64Value),
		CommitteeTermGrants: collections.NewKeySet(sb, types.CommitteeTermGrantsKey, "committee_term_grants", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		RegistrarTermGrants: collections.NewKeySet(sb, types.RegistrarTermGrantsKey, "registrar_term_grants", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		Due:                 collections.NewKeySet(sb, types.DueKey, "due", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
	}
	var err error
	k.Schema, err = sb.Build()
	if err != nil {
		panic(err)
	}
	return k
}

// Logger returns a module-specific logger.
func (k *Keeper) Logger(ctx context.Context) log.Logger {
	return sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/"+types.ModuleName)
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
