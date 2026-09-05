package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	gogoproto "github.com/cosmos/gogoproto/proto"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// acceptedQueries is the set of gRPC query paths contracts may reach, each with
// the response it decodes into. Hand-written, as Osmosis, Neutron, Juno, and
// Archway keep theirs: a contract reading a value makes it an input to
// consensus and freezes the response shape for the life of the chain, so
// listing is a review rather than an annotation. Every entry must also be
// annotated module_query_safe, which the tests check.
//
// The launch list is what a contract needs to price and route a conversion or
// a transfer: the tax estimate D43 asks for (D74) and the caps and gas prices
// behind it, the Oracle rates and reference unit, Market's quote, pool, and
// policy, and the asset registry. Mandates, ledgers, positions, fund status,
// exposure, rewards, attendance, feeds, and settlement plans stay unlisted:
// widening later is additive, narrowing breaks every contract that read the
// path.
func acceptedQueries() wasmkeeper.AcceptedQueries {
	return wasmkeeper.AcceptedQueries{
		"/ark.treasury.v1.Query/ComputeTax": func() gogoproto.Message { return &treasurytypes.QueryComputeTaxResponse{} },
		"/ark.treasury.v1.Query/TaxCap":     func() gogoproto.Message { return &treasurytypes.QueryTaxCapResponse{} },
		"/ark.treasury.v1.Query/TaxCaps":    func() gogoproto.Message { return &treasurytypes.QueryTaxCapsResponse{} },
		"/ark.treasury.v1.Query/GasPrice":   func() gogoproto.Message { return &treasurytypes.QueryGasPriceResponse{} },
		"/ark.treasury.v1.Query/GasPrices":  func() gogoproto.Message { return &treasurytypes.QueryGasPricesResponse{} },

		"/ark.oracle.v1.Query/ExchangeRate":   func() gogoproto.Message { return &oracletypes.QueryExchangeRateResponse{} },
		"/ark.oracle.v1.Query/ExchangeRates":  func() gogoproto.Message { return &oracletypes.QueryExchangeRatesResponse{} },
		"/ark.oracle.v1.Query/ReferenceDenom": func() gogoproto.Message { return &oracletypes.QueryReferenceDenomResponse{} },

		"/ark.market.v1.Query/Swap":             func() gogoproto.Message { return &markettypes.QuerySwapResponse{} },
		"/ark.market.v1.Query/Pool":             func() gogoproto.Message { return &markettypes.QueryPoolResponse{} },
		"/ark.market.v1.Query/ConversionPolicy": func() gogoproto.Message { return &markettypes.QueryConversionPolicyResponse{} },
		"/ark.market.v1.Query/TobinTax":         func() gogoproto.Message { return &markettypes.QueryTobinTaxResponse{} },

		"/ark.asset.v1.Query/Asset":  func() gogoproto.Message { return &assettypes.QueryAssetResponse{} },
		"/ark.asset.v1.Query/Assets": func() gogoproto.Message { return &assettypes.QueryAssetsResponse{} },
	}
}

// wasmQueryPlugins supplies the two queriers Wasmd refuses by default. Both
// read through the accept list; every other plugin stays Wasmd's own, Custom
// included, which refuses every request.
func (app *ArkApp) wasmQueryPlugins(accepted wasmkeeper.AcceptedQueries) *wasmkeeper.QueryPlugins {
	return &wasmkeeper.QueryPlugins{
		Stargate: wasmkeeper.AcceptListStargateQuerier(accepted, app.GRPCQueryRouter(), app.appCodec),
		Grpc:     wasmkeeper.AcceptListGrpcQuerier(accepted, app.GRPCQueryRouter(), app.appCodec),
	}
}
