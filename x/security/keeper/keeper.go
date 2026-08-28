package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/ararat-network/ark/x/security/types"
)

// Keeper of the security store. It owns almost no state — the powers it
// delegates land in x/upgrade and IBC core — so it stores only the appointment
// and which pending upgrade plan the committee scheduled. Writes travel back
// out through the message router, so each target applies its own validation
// and re-checks the chain authority.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	authority    string
	router       baseapp.MessageRouter

	accountKeeper types.AccountKeeper
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
	upgradeKeeper types.UpgradeKeeper,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	k := &Keeper{
		cdc:           cdc,
		storeService:  storeService,
		authority:     authority,
		router:        router,
		accountKeeper: accountKeeper,
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
