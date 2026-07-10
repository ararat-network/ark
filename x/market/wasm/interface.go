package wasm

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/x/market/keeper"
	"ark/x/market/types"
	wasm "ark/x/wasm/exported"
)

var (
	_ wasm.WasmQuerierInterface   = Querier{}
	_ wasm.WasmMsgParserInterface = MsgParser{}
)

// MsgParser - wasm msg parser for staking msgs
type MsgParser struct{}

// NewWasmMsgParser returns bank wasm msg parser
func NewWasmMsgParser() MsgParser {
	return MsgParser{}
}

// Parse implements wasm staking msg parser
func (MsgParser) Parse(_ sdk.AccAddress, _ wasmvmtypes.CosmosMsg) (sdk.Msg, error) {
	return nil, nil
}

// CosmosMsg only contains swap msg
type CosmosMsg struct {
	Swap     *types.MsgSwap     `json:"swap,omitempty"`
	SwapSend *types.MsgSwapSend `json:"swap_send,omitempty"`
}

// ParseCustom implements custom parser
func (MsgParser) ParseCustom(contractAddr sdk.AccAddress, data json.RawMessage) (sdk.Msg, error) {
	var sdkMsg CosmosMsg
	err := json.Unmarshal(data, &sdkMsg)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrJSONUnmarshal, "unmarshalling market custom message: %v", err)
	}

	// TODO: call validateInputs here instead of inline validation
	if sdkMsg.Swap != nil {
		sdkMsg.Swap.Trader = contractAddr.String()
		if sdkMsg.Swap.OfferCoin.Amount.LTE(math.ZeroInt()) || sdkMsg.Swap.OfferCoin.Amount.BigInt().BitLen() > 100 {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, sdkMsg.Swap.OfferCoin.String())
		}
		if sdkMsg.Swap.OfferCoin.Denom == sdkMsg.Swap.AskDenom {
			return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, sdkMsg.Swap.AskDenom)
		}
		return sdkMsg.Swap, nil
	} else if sdkMsg.SwapSend != nil {
		sdkMsg.SwapSend.FromAddress = contractAddr.String()
		if _, err := sdk.AccAddressFromBech32(sdkMsg.SwapSend.ToAddress); err != nil {
			return nil, sdkerrors.Wrapf(errortypes.ErrInvalidAddress, "Invalid to address (%s)", err)
		}
		if sdkMsg.SwapSend.OfferCoin.Amount.LTE(math.ZeroInt()) || sdkMsg.SwapSend.OfferCoin.Amount.BigInt().BitLen() > 100 {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, sdkMsg.SwapSend.OfferCoin.String())
		}
		if sdkMsg.SwapSend.OfferCoin.Denom == sdkMsg.SwapSend.AskDenom {
			return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, sdkMsg.SwapSend.AskDenom)
		}
		return sdkMsg.SwapSend, nil
	}

	return nil, sdkerrors.Wrap(wasm.ErrInvalidMsg, "unknown market message variant")
}

// Querier - staking query interface for wasm contract
type Querier struct {
	k *keeper.Keeper
}

// NewWasmQuerier return bank wasm query interface
func NewWasmQuerier(k *keeper.Keeper) Querier {
	return Querier{k}
}

// Query - implement query function
func (Querier) Query(_ sdk.Context, _ wasmvmtypes.QueryRequest) ([]byte, error) {
	return nil, nil
}

// CosmosQuery only contains swap simulation
type CosmosQuery struct {
	Swap *types.QuerySwapRequest `json:"swap,omitempty"`
}

// SwapQueryResponse - swap simulation query response for wasm module
type SwapQueryResponse struct {
	Receive wasmvmtypes.Coin `json:"receive"`
}

// QueryCustom implements custom query interface
func (querier Querier) QueryCustom(ctx sdk.Context, data json.RawMessage) ([]byte, error) {
	var params CosmosQuery
	err := json.Unmarshal(data, &params)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrJSONUnmarshal, "unmarshalling market custom query: %v", err)
	}

	q := keeper.NewQueryServerImpl(querier.k)
	if params.Swap != nil {
		res, err := q.Swap(ctx, &types.QuerySwapRequest{
			OfferCoin: params.Swap.OfferCoin,
			AskDenom:  params.Swap.AskDenom,
		})
		if err != nil {
			return nil, err
		}

		bz, err := json.Marshal(SwapQueryResponse{Receive: wasm.EncodeSdkCoin(res.SwapCoin)})
		if err != nil {
			return nil, sdkerrors.Wrapf(errortypes.ErrJSONMarshal, "marshalling market custom query response: %v", err)
		}

		return bz, err
	}

	return nil, wasmvmtypes.UnsupportedRequest{Kind: "unknown Market variant"}
}
