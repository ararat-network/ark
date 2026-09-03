package app_test

import (
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// Every listed path must carry the annotation, route, and construct its
// response. Listing is a review the annotation does not replace, and the
// annotation is a claim listing does not restate; both are required. Vacuous
// while the list is empty, and the first check the activation gate runs.
func TestAcceptedQueriesAreAnnotatedRouteAndConstruct(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	annotated := moduleQuerySafePaths()

	for path, response := range app.AcceptedQueries() {
		t.Run(path, func(t *testing.T) {
			require.Contains(t, annotated, path, "a listed path must be annotated module_query_safe")
			require.NotNil(t, arkApp.GRPCQueryRouter().Route(path), "path must route")
			require.NotNil(t, response(), "response type must construct")
		})
	}
}

// With the list empty, a read is refused by path on both transports: the check
// that contracts cannot reach chain state before the activation gate.
func TestQueriesAreRefusedWhileUnlisted(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})
	plugins := arkApp.WasmQueryPlugins(app.AcceptedQueries())

	const path = "/ark.treasury.v1.Query/ComputeTax"

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
	plugins := arkApp.WasmQueryPlugins(wasmkeeper.AcceptedQueries{
		path: func() gogoproto.Message { return &treasurytypes.QueryComputeTaxResponse{} },
	})

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

	charged, err := arkApp.TreasuryKeeper.ComputeTax(ctx, []sdk.Msg{send})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), charged, "ten percent of the principal")
	require.Equal(t, charged, estimate.Tax)
}
