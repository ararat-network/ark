package wasm

import (
	"encoding/json"
	"fmt"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"noah/x/treasury/keeper"
	"noah/x/treasury/types"
)

// var _ wasm.WasmQuerierInterface = Querier{}

// Querier - staking query interface for wasm contract
type Querier struct {
	k keeper.Keeper
}

// NewQuerier return bank wasm query interface
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
		return nil, sdkerrors.Wrap(errortypes.ErrJSONUnmarshal, err.Error())
	}

	var bz []byte

	switch {
	case query.TaxRate != nil:
		rate, err := q.k.TaxRate.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting tax rate: %w", err)
		}
		bz, err = json.Marshal(TaxRateQueryResponse{Rate: rate.String()})
	case query.TaxCap != nil:
		cap, err := q.k.TaxCaps.Get(ctx, query.TaxCap.Denom)
		if err != nil {
			return nil, fmt.Errorf("getting tax caps: %w", err)
		}
		bz, err = json.Marshal(TaxCapQueryResponse{Cap: cap.String()})
	default:
		return nil, errortypes.ErrInvalidRequest
	}

	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrJSONMarshal, err.Error())
	}

	return bz, nil
}
