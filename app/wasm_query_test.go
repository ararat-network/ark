package app_test

import (
	"encoding/json"
	"testing"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// Every listed path must carry the annotation, route, and construct its
// response. Listing is a review the annotation does not replace, and the
// annotation is a claim listing does not restate; both are required.
func TestAcceptedQueriesAreAnnotatedRouteAndConstruct(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	annotated := moduleQuerySafePaths()
	require.Len(t, app.AcceptedQueries(), 14, "the launch list is fourteen paths; widening is a review")

	for path, response := range app.AcceptedQueries() {
		t.Run(path, func(t *testing.T) {
			require.Contains(t, annotated, path, "a listed path must be annotated module_query_safe")
			require.NotNil(t, arkApp.GRPCQueryRouter().Route(path), "path must route")
			require.NotNil(t, response(), "response type must construct")
		})
	}
}

// An annotated path that is not listed is refused by path on both
// transports: the annotation says a read is deterministic, listing says its
// shape is frozen, and only the second admits a contract.
func TestUnlistedQueriesAreRefused(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})
	plugins := arkApp.WasmQueryPlugins(app.AcceptedQueries())

	const path = "/ark.treasury.v1.Query/FundStatus"
	require.Contains(t, moduleQuerySafePaths(), path, "the case is an annotated, unlisted path")
	require.NotContains(t, app.AcceptedQueries(), path)

	_, err := plugins.Stargate(ctx, &wasmvmtypes.StargateQuery{Path: path})
	require.ErrorContains(t, err, "not allowed from the contract")

	_, err = plugins.Grpc(ctx, &wasmvmtypes.GrpcQuery{Path: path})
	require.ErrorContains(t, err, "not allowed from the contract")
}

// ComputeTax is the estimate D43 asks for, reached through the accept list
// rather than a custom querier (D74). Listed as the gate will list it, the gRPC
// querier must answer with Treasury's own figure for the message a contract
// would dispatch.
func TestComputeTaxThroughTheAcceptListMatchesTheCharge(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)

	const path = "/ark.treasury.v1.Query/ComputeTax"
	plugins := arkApp.WasmQueryPlugins(app.AcceptedQueries())

	send := &banktypes.MsgSend{
		FromAddress: contract.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}
	packed, err := codectypes.NewAnyWithValue(send)
	require.NoError(t, err)
	data, err := arkApp.AppCodec().Marshal(&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{packed}})
	require.NoError(t, err)

	response, err := plugins.Grpc(ctx, &wasmvmtypes.GrpcQuery{Path: path, Data: data})
	require.NoError(t, err)
	estimate, ok := response.(*treasurytypes.QueryComputeTaxResponse)
	require.True(t, ok, "response must be Treasury's own type, got %T", response)

	charged, _, err := arkApp.TreasuryKeeper.ComputeTax(ctx, []sdk.Msg{send})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), charged, "ten percent of the principal")
	require.Equal(t, charged, estimate.Tax)
}

// taxQuery runs a ComputeTax request for msgs through the gRPC querier a
// contract reaches, returning the estimate.
func taxQuery(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, msgs ...sdk.Msg) (*treasurytypes.QueryComputeTaxResponse, error) {
	t.Helper()

	packed := make([]*codectypes.Any, 0, len(msgs))
	for _, msg := range msgs {
		a, err := codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		packed = append(packed, a)
	}
	data, err := arkApp.AppCodec().Marshal(&treasurytypes.QueryComputeTaxRequest{Messages: packed})
	require.NoError(t, err)

	response, err := arkApp.WasmQueryPlugins(app.AcceptedQueries()).Grpc(ctx, &wasmvmtypes.GrpcQuery{
		Path: "/ark.treasury.v1.Query/ComputeTax", Data: data,
	})
	if err != nil {
		return nil, err
	}
	estimate, ok := response.(*treasurytypes.QueryComputeTaxResponse)
	require.True(t, ok, "response must be Treasury's own type, got %T", response)
	return estimate, nil
}

// The estimate a contract reads must be the charge for every message shape
// it can dispatch, top-level or attached: the same calculator answers both,
// and the query decodes for itself the Any a contract would send.
func TestTaxQueryMatchesTheChargeForEveryShape(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)
	recipient := authtypes.NewModuleAddress("recipient").String()
	stable := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000))
	send := &banktypes.MsgSend{FromAddress: contract.String(), ToAddress: recipient, Amount: stable}
	exec := authz.NewMsgExec(contract, []sdk.Msg{send})

	shapes := map[string]sdk.Msg{
		"bank send": send,
		"multisend with two inputs": &banktypes.MsgMultiSend{
			Inputs:  []banktypes.Input{{Address: contract.String(), Coins: stable}, {Address: recipient, Coins: stable}},
			Outputs: []banktypes.Output{{Address: recipient, Coins: stable.Add(stable...)}},
		},
		"ibc transfer": ibctransfertypes.NewMsgTransfer(ibctransfertypes.PortID, "channel-0", stable[0],
			contract.String(), recipient, clienttypes.NewHeight(1, 1), 0, ""),
		"market swap send": &markettypes.MsgSwapSend{
			FromAddress: contract.String(), ToAddress: recipient, OfferCoin: stable[0], AskDenom: chain.NoahBaseDenom,
			MinimumReceive: sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
		},
		"execute with funds": &wasmtypes.MsgExecuteContract{
			Sender: contract.String(), Contract: recipient, Msg: []byte("{}"), Funds: stable,
		},
		"instantiate with funds": &wasmtypes.MsgInstantiateContract{
			Sender: contract.String(), CodeID: 1, Label: "child", Msg: []byte("{}"), Funds: stable,
		},
		"authz exec around a send": &exec,
	}
	for name, msg := range shapes {
		t.Run(name, func(t *testing.T) {
			estimate, err := taxQuery(t, arkApp, ctx, msg)
			require.NoError(t, err)
			charged, _, err := arkApp.TreasuryKeeper.ComputeTax(ctx, []sdk.Msg{msg})
			require.NoError(t, err)
			require.False(t, charged.IsZero(), "the shape is taxable, so equality is not vacuous")
			require.Equal(t, charged, estimate.Tax)
		})
	}
}

// A request the calculator cannot read fails closed rather than estimating
// zero: an unknown message type and unreadable bytes under a known one.
func TestTaxQueryFailsClosedOnMalformedInput(t *testing.T) {
	arkApp, ctx, _ := setupExecutionTaxFixture(t)
	plugins := arkApp.WasmQueryPlugins(app.AcceptedQueries())

	for name, any := range map[string]*codectypes.Any{
		"unknown type":              {TypeUrl: "/ark.nowhere.v1.MsgNothing", Value: []byte{1}},
		"garbage under a known one": {TypeUrl: sdk.MsgTypeURL(&banktypes.MsgSend{}), Value: []byte{0xff, 0xff}},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := arkApp.AppCodec().Marshal(&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{any}})
			require.NoError(t, err)
			_, err = plugins.Grpc(ctx, &wasmvmtypes.GrpcQuery{Path: "/ark.treasury.v1.Query/ComputeTax", Data: data})
			require.Error(t, err)
		})
	}
}

// The estimate is advisory: it moves nothing, reserves nothing, and cannot
// stand in for the charge dispatch recomputes.
func TestTaxQueryWritesNothing(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)
	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	send := &banktypes.MsgSend{
		FromAddress: contract.String(), ToAddress: authtypes.NewModuleAddress("recipient").String(),
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}

	before := arkApp.BankKeeper.GetAllBalances(ctx, contract)
	for range 3 {
		_, err := taxQuery(t, arkApp, ctx, send)
		require.NoError(t, err)
	}
	require.Equal(t, before, arkApp.BankKeeper.GetAllBalances(ctx, contract))
	require.True(t, arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).IsZero())
}

// A running contract reads a listed path and gets the chain's own answer,
// and is refused an annotated path that is not listed, on both transports.
func TestContractReadsListedPathsOnly(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "reader")
	request, err := f.app.AppCodec().Marshal(&oracletypes.QueryReferenceDenomRequest{})
	require.NoError(t, err)

	const listed = "/ark.oracle.v1.Query/ReferenceDenom"
	direct, err := f.app.GRPCQueryRouter().Route(listed)(f.ctx, &abci.RequestQuery{Data: request})
	require.NoError(t, err)
	var want oracletypes.QueryReferenceDenomResponse
	require.NoError(t, f.app.AppCodec().Unmarshal(direct.Value, &want))

	// Wasmd hands a contract protobuf bytes over gRPC and, to keep the
	// answer deterministic, JSON over Stargate.
	t.Run("grpc", func(t *testing.T) {
		raw := f.chainQuery(t, contract, wasmvmtypes.QueryRequest{Grpc: &wasmvmtypes.GrpcQuery{Path: listed, Data: request}})
		var got oracletypes.QueryReferenceDenomResponse
		require.NoError(t, f.app.AppCodec().Unmarshal(raw, &got))
		require.Equal(t, want, got, "the contract reads what the chain answers")
	})
	t.Run("stargate", func(t *testing.T) {
		raw := f.chainQuery(t, contract, wasmvmtypes.QueryRequest{Stargate: &wasmvmtypes.StargateQuery{Path: listed, Data: request}})
		var got oracletypes.QueryReferenceDenomResponse
		require.NoError(t, f.app.AppCodec().UnmarshalJSON(raw, &got))
		require.Equal(t, want, got, "the contract reads what the chain answers")
	})

	const unlisted = "/ark.treasury.v1.Query/FundStatus"
	for name, req := range map[string]wasmvmtypes.QueryRequest{
		"grpc":     {Grpc: &wasmvmtypes.GrpcQuery{Path: unlisted, Data: []byte{}}},
		"stargate": {Stargate: &wasmvmtypes.StargateQuery{Path: unlisted, Data: []byte{}}},
	} {
		t.Run("refused "+name, func(t *testing.T) {
			query, err := json.Marshal(testdata.ReflectQueryMsg{Chain: &testdata.ChainQuery{Request: &req}})
			require.NoError(t, err)
			_, err = f.app.WasmKeeper.QuerySmart(f.ctx, contract, query)
			require.Error(t, err)
		})
	}
}
