package exported

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var ErrInvalidMsg = errorsmod.Register("wasm", 8, "invalid Msg from the contract")

type WasmMsgParserInterface interface {
	Parse(contractAddr sdk.AccAddress, msg wasmvmtypes.CosmosMsg) (sdk.Msg, error)
	ParseCustom(contractAddr sdk.AccAddress, data json.RawMessage) (sdk.Msg, error)
}

type WasmQuerierInterface interface {
	Query(ctx sdk.Context, request wasmvmtypes.QueryRequest) ([]byte, error)
	QueryCustom(ctx sdk.Context, data json.RawMessage) ([]byte, error)
}

func EncodeSdkCoin(coin sdk.Coin) wasmvmtypes.Coin {
	return wasmvmtypes.Coin{
		Denom:  coin.Denom,
		Amount: coin.Amount.String(),
	}
}
