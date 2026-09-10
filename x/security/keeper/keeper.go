package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/ararat-network/ark/x/security/types"
)

// Keeper stores the security mandate and committee plan ownership. Actions dispatch through the
// message router so upstream modules enforce validation and effective authority.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string
	router       baseapp.MessageRouter

	accountKeeper types.AccountKeeper
	wasmKeeper    types.WasmKeeper
	upgradeKeeper types.UpgradeKeeper

	Schema collections.Schema

	Mandate       collections.Item[types.SecurityMandate]
	CommitteePlan collections.Item[types.CommitteePlan]
}

// NewKeeper creates a new security Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	authority string,
	router baseapp.MessageRouter,
	accountKeeper types.AccountKeeper,
	wasmKeeper types.WasmKeeper,
	upgradeKeeper types.UpgradeKeeper,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		router:        router,
		accountKeeper: accountKeeper,
		wasmKeeper:    wasmKeeper,
		upgradeKeeper: upgradeKeeper,
		Mandate: collections.NewItem(
			sb,
			types.SecurityMandateKey,
			"security_mandate",
			codec.CollValue[types.SecurityMandate](cdc),
		),
		CommitteePlan: collections.NewItem(
			sb,
			types.CommitteePlanKey,
			"committee_plan",
			codec.CollValue[types.CommitteePlan](cdc),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}
