package wasm

import (
	"encoding/json"

	marketv1 "noah/api/noah/market/v1"
	"noah/x/market/keeper"
	"noah/x/market/types"
	wasm "noah/x/wasm/exported"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
)

var (
	_ wasm.WasmQuerierInterface   = Querier{}
	_ wasm.WasmMsgParserInterface = MsgParser{}
)

// WasmMsgParser - wasm msg parser for staking msgs
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
	Swap     *marketv1.MsgSwap     `json:"swap,omitempty"`
	SwapSend *marketv1.MsgSwapSend `json:"swap_send,omitempty"`
}

// ParseCustom implements custom parser
func (MsgParser) ParseCustom(contractAddr sdk.AccAddress, data json.RawMessage) (sdk.Msg, error) {
	var sdkMsg CosmosMsg
	err := json.Unmarshal(data, &sdkMsg)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to parse market custom msg")
	}

	if sdkMsg.Swap != nil {
		sdkMsg.Swap.Trader = contractAddr.String()

		amt, ok := math.NewIntFromString(sdkMsg.Swap.OfferCoin.Amount)
		if !ok {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, sdkMsg.Swap.OfferCoin.String())
		}

		offerCoin := sdk.NewCoin(sdkMsg.Swap.OfferCoin.Denom, amt)
		if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
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

		amt, ok := math.NewIntFromString(sdkMsg.SwapSend.OfferCoin.Amount)
		if !ok {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, sdkMsg.SwapSend.OfferCoin.String())
		}

		offerCoin := sdk.NewCoin(sdkMsg.SwapSend.OfferCoin.Denom, amt)
		if offerCoin.Amount.LTE(math.ZeroInt()) || offerCoin.Amount.BigInt().BitLen() > 100 {
			return nil, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
		}
		if sdkMsg.SwapSend.OfferCoin.Denom == sdkMsg.SwapSend.AskDenom {
			return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, sdkMsg.SwapSend.AskDenom)
		}

		return sdkMsg.SwapSend, nil
	}

	return nil, sdkerrors.Wrap(wasm.ErrInvalidMsg, "Unknown variant of Market")
}

// WasmQuerier - staking query interface for wasm contract
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
	Swap *types.QuerySwapParams `json:"swap,omitempty"`
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
		return nil, sdkerrors.Wrap(errortypes.ErrJSONUnmarshal, err.Error())
	}

	q := keeper.NewQueryServerImpl(querier.k)
	if params.Swap != nil {
		res, err := q.Swap(ctx, &marketv1.QuerySwapRequest{
			OfferCoin: params.Swap.OfferCoin.String(),
			AskDenom:  params.Swap.AskDenom,
		})
		if err != nil {
			return nil, err
		}

		bz, err := json.Marshal(SwapQueryResponse{Receive: wasm.EncodeSdkCoin(res.ReturnCoin)})
		if err != nil {
			return nil, sdkerrors.Wrap(errortypes.ErrJSONMarshal, err.Error())
		}

		return bz, err
	}

	return nil, wasmvmtypes.UnsupportedRequest{Kind: "unknown Market variant"}
}
