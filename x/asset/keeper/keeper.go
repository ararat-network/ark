package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"

	"ark/x/asset/types"
)

// Keeper stores asset registry and lifecycle state.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string
	bankKeeper   types.BankKeeper
	oracleKeeper types.OracleKeeper

	Schema               collections.Schema
	Params               collections.Item[types.Params]
	Assets               collections.Map[string, types.Asset]
	SettlementPlans      collections.Map[string, types.SettlementPlan]
	ResolutionRecords    collections.Map[collections.Pair[string, uint64], types.ResolutionRecord]
	EmergencyMandate     collections.Item[types.EmergencyMandate]
	EmergencySuspensions collections.KeySet[string]
}

// NewKeeper constructs an asset keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	bankKeeper types.BankKeeper,
	oracleKeeper types.OracleKeeper,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		bankKeeper:   bankKeeper,
		oracleKeeper: oracleKeeper,
		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		Assets: collections.NewMap(
			sb,
			types.AssetsKey,
			"assets",
			collections.StringKey,
			codec.CollValue[types.Asset](cdc),
		),
		SettlementPlans: collections.NewMap(
			sb,
			types.SettlementPlansKey,
			"settlement_plans",
			collections.StringKey,
			codec.CollValue[types.SettlementPlan](cdc),
		),
		ResolutionRecords: collections.NewMap(
			sb,
			types.ResolutionRecordsKey,
			"resolution_records",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
			codec.CollValue[types.ResolutionRecord](cdc),
		),
		EmergencyMandate: collections.NewItem(
			sb,
			types.EmergencyMandateKey,
			"emergency_mandate",
			codec.CollValue[types.EmergencyMandate](cdc),
		),
		EmergencySuspensions: collections.NewKeySet(
			sb,
			types.EmergencySuspensionsKey,
			"emergency_suspensions",
			collections.StringKey,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}
