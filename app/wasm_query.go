package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	gogoproto "github.com/cosmos/gogoproto/proto"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// acceptedQueries lists the consensus-safe gRPC paths available to contracts and their response
// types. Every entry requires module_query_safe. Adding a path expands the contract API; removing
// one can break deployed contracts. See README.md for the accepted surface.
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
