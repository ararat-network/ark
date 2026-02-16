package simulation

import (
	"noah/x/market/keeper"
	"noah/x/market/types"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// RegisterStoreDecoder registers the decoder for market module store using the
// collections schema, which automatically handles decoding all collection items.
func RegisterStoreDecoder(sdr simtypes.StoreDecoderRegistry, k keeper.Keeper) {
	sdr[types.StoreKey] = simtypes.NewStoreDecoderFuncFromCollectionsSchema(k.Schema)
}
