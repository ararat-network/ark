package app

import (
	"encoding/json"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
)

// DefaultGenesis returns the registered module defaults with Ark's canonical
// native denomination metadata.
func (app *ArkApp) DefaultGenesis() map[string]json.RawMessage {
	genesis := app.App.DefaultGenesis()
	bankGenesis := banktypes.GetGenesisStateFromAppState(app.appCodec, genesis)
	bankGenesis.DenomMetadata = append(bankGenesis.DenomMetadata, chain.NoahMetadata())
	genesis[banktypes.ModuleName] = app.appCodec.MustMarshalJSON(bankGenesis)

	return genesis
}
