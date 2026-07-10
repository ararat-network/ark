package wasm

import (
	"encoding/json"
	"fmt"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
)

// var _ wasm.WasmQuerierInterface = Querier{}

// Querier - staking query interface for wasm contract
type Querier struct {
	k keeper.Keeper
}

// NewWasmQuerier return bank wasm query interface
func NewWasmQuerier(k keeper.Keeper) Querier {
	return Querier{k}
}

// Query - implement query function
func (Querier) Query(_ sdk.Context, _ wasmvmtypes.QueryRequest) ([]byte, error) {
	return nil, nil
}

// CosmosQuery contains various treasury queries
type CosmosQuery struct {
	TaxRate *struct{}     `json:"tax_rate,omitempty"`
	TaxCap  *types.TaxCap `json:"tax_cap,omitempty"`
}

// TaxRateQueryResponse - tax rate query response for wasm module
type TaxRateQueryResponse struct {
	// decimal string, eg "0.02"
	Rate string `json:"rate"`
}

// TaxCapQueryResponse - tax cap query response for wasm module
type TaxCapQueryResponse struct {
	// uint64 string, eg "1000000"
	Cap string `json:"cap"`
}

// QueryCustom implements custom query interface
func (q Querier) QueryCustom(ctx sdk.Context, data json.RawMessage) ([]byte, error) {
	var query CosmosQuery
	err := json.Unmarshal(data, &query)
	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrJSONUnmarshal, "unmarshalling treasury custom query: %v", err)
	}

	var bz []byte

	switch {
	case query.TaxRate != nil:
		rate, err := q.k.TaxRate.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting tax rate: %w", err)
		}
		bz, err = json.Marshal(TaxRateQueryResponse{Rate: rate.String()})
		if err != nil {
			return nil, fmt.Errorf("marshalling response: %w", err)
		}
	case query.TaxCap != nil:
		cap, err := q.k.TaxCaps.Get(ctx, query.TaxCap.Denom)
		if err != nil {
			return nil, fmt.Errorf("getting tax caps: %w", err)
		}
		bz, err = json.Marshal(TaxCapQueryResponse{Cap: cap.String()})
		if err != nil {
			return nil, fmt.Errorf("marshalling response: %w", err)
		}
	default:
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidRequest, "unknown treasury query variant")
	}

	if err != nil {
		return nil, sdkerrors.Wrapf(errortypes.ErrJSONMarshal, "marshalling treasury custom query response: %v", err)
	}

	return bz, nil
}
