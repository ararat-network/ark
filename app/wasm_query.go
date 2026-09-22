package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	gogoproto "github.com/cosmos/gogoproto/proto"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// acceptedQueries lists the consensus-safe gRPC paths available to contracts and their response
// types. Every entry requires module_query_safe. The SDK's own annotated set, auth, bank, and
// staking, is admitted whole: the annotation is the review, and the hand-written entries are for
// Ark's modules (D85). Adding a path expands the contract API; removing one can break deployed
// contracts. See README.md for the accepted surface.
func acceptedQueries() wasmkeeper.AcceptedQueries {
	return wasmkeeper.AcceptedQueries{
		"/cosmos.auth.v1beta1.Query/Account":             func() gogoproto.Message { return &authtypes.QueryAccountResponse{} },
		"/cosmos.auth.v1beta1.Query/AccountInfo":         func() gogoproto.Message { return &authtypes.QueryAccountInfoResponse{} },
		"/cosmos.auth.v1beta1.Query/AccountAddressByID":  func() gogoproto.Message { return &authtypes.QueryAccountAddressByIDResponse{} },
		"/cosmos.auth.v1beta1.Query/Accounts":            func() gogoproto.Message { return &authtypes.QueryAccountsResponse{} },
		"/cosmos.auth.v1beta1.Query/ModuleAccounts":      func() gogoproto.Message { return &authtypes.QueryModuleAccountsResponse{} },
		"/cosmos.auth.v1beta1.Query/ModuleAccountByName": func() gogoproto.Message { return &authtypes.QueryModuleAccountByNameResponse{} },
		"/cosmos.auth.v1beta1.Query/Params":              func() gogoproto.Message { return &authtypes.QueryParamsResponse{} },

		"/cosmos.bank.v1beta1.Query/Balance":                    func() gogoproto.Message { return &banktypes.QueryBalanceResponse{} },
		"/cosmos.bank.v1beta1.Query/AllBalances":                func() gogoproto.Message { return &banktypes.QueryAllBalancesResponse{} },
		"/cosmos.bank.v1beta1.Query/SpendableBalances":          func() gogoproto.Message { return &banktypes.QuerySpendableBalancesResponse{} },
		"/cosmos.bank.v1beta1.Query/SpendableBalanceByDenom":    func() gogoproto.Message { return &banktypes.QuerySpendableBalanceByDenomResponse{} },
		"/cosmos.bank.v1beta1.Query/TotalSupply":                func() gogoproto.Message { return &banktypes.QueryTotalSupplyResponse{} },
		"/cosmos.bank.v1beta1.Query/SupplyOf":                   func() gogoproto.Message { return &banktypes.QuerySupplyOfResponse{} },
		"/cosmos.bank.v1beta1.Query/DenomMetadata":              func() gogoproto.Message { return &banktypes.QueryDenomMetadataResponse{} },
		"/cosmos.bank.v1beta1.Query/DenomMetadataByQueryString": func() gogoproto.Message { return &banktypes.QueryDenomMetadataByQueryStringResponse{} },
		"/cosmos.bank.v1beta1.Query/DenomsMetadata":             func() gogoproto.Message { return &banktypes.QueryDenomsMetadataResponse{} },
		"/cosmos.bank.v1beta1.Query/DenomOwners":                func() gogoproto.Message { return &banktypes.QueryDenomOwnersResponse{} },
		"/cosmos.bank.v1beta1.Query/DenomOwnersByQuery":         func() gogoproto.Message { return &banktypes.QueryDenomOwnersByQueryResponse{} },
		"/cosmos.bank.v1beta1.Query/SendEnabled":                func() gogoproto.Message { return &banktypes.QuerySendEnabledResponse{} },
		"/cosmos.bank.v1beta1.Query/Params":                     func() gogoproto.Message { return &banktypes.QueryParamsResponse{} },

		"/cosmos.staking.v1beta1.Query/Pool":                          func() gogoproto.Message { return &stakingtypes.QueryPoolResponse{} },
		"/cosmos.staking.v1beta1.Query/Params":                        func() gogoproto.Message { return &stakingtypes.QueryParamsResponse{} },
		"/cosmos.staking.v1beta1.Query/Validator":                     func() gogoproto.Message { return &stakingtypes.QueryValidatorResponse{} },
		"/cosmos.staking.v1beta1.Query/Validators":                    func() gogoproto.Message { return &stakingtypes.QueryValidatorsResponse{} },
		"/cosmos.staking.v1beta1.Query/Delegation":                    func() gogoproto.Message { return &stakingtypes.QueryDelegationResponse{} },
		"/cosmos.staking.v1beta1.Query/DelegatorDelegations":          func() gogoproto.Message { return &stakingtypes.QueryDelegatorDelegationsResponse{} },
		"/cosmos.staking.v1beta1.Query/DelegatorValidator":            func() gogoproto.Message { return &stakingtypes.QueryDelegatorValidatorResponse{} },
		"/cosmos.staking.v1beta1.Query/DelegatorValidators":           func() gogoproto.Message { return &stakingtypes.QueryDelegatorValidatorsResponse{} },
		"/cosmos.staking.v1beta1.Query/ValidatorDelegations":          func() gogoproto.Message { return &stakingtypes.QueryValidatorDelegationsResponse{} },
		"/cosmos.staking.v1beta1.Query/UnbondingDelegation":           func() gogoproto.Message { return &stakingtypes.QueryUnbondingDelegationResponse{} },
		"/cosmos.staking.v1beta1.Query/DelegatorUnbondingDelegations": func() gogoproto.Message { return &stakingtypes.QueryDelegatorUnbondingDelegationsResponse{} },
		"/cosmos.staking.v1beta1.Query/ValidatorUnbondingDelegations": func() gogoproto.Message { return &stakingtypes.QueryValidatorUnbondingDelegationsResponse{} },
		"/cosmos.staking.v1beta1.Query/Redelegations":                 func() gogoproto.Message { return &stakingtypes.QueryRedelegationsResponse{} },
		"/cosmos.staking.v1beta1.Query/HistoricalInfo":                func() gogoproto.Message { return &stakingtypes.QueryHistoricalInfoResponse{} },

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
