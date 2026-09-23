package app_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	icahostkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
	queryv1 "cosmossdk.io/api/cosmos/query/v1"

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

// arkModuleQuerySafePaths records all Ark RPCs annotated for consensus queries, including ICA host
// access. Explicit expectations make annotation changes reviewable; the Wasm accept list is
// separate.
var arkModuleQuerySafePaths = []string{
	"/ark.asset.v1.Query/Asset",
	"/ark.asset.v1.Query/Assets",
	"/ark.asset.v1.Query/EmergencyMandate",
	"/ark.asset.v1.Query/Params",
	"/ark.asset.v1.Query/ResolutionHistory",
	"/ark.asset.v1.Query/SettlementPlan",
	"/ark.claims.v1.Query/Balance",
	"/ark.claims.v1.Query/Claim",
	"/ark.claims.v1.Query/Claims",
	"/ark.claims.v1.Query/ClaimsMandate",
	"/ark.claims.v1.Query/Params",
	"/ark.disbursement.v1.Query/Balance",
	"/ark.disbursement.v1.Query/Beneficiary",
	"/ark.disbursement.v1.Query/Grant",
	"/ark.disbursement.v1.Query/Grants",
	"/ark.disbursement.v1.Query/Issuance",
	"/ark.disbursement.v1.Query/Journal",
	"/ark.disbursement.v1.Query/Member",
	"/ark.disbursement.v1.Query/Params",
	"/ark.disbursement.v1.Query/RegistrarMandate",
	"/ark.disbursement.v1.Query/Releasable",
	"/ark.disbursement.v1.Query/Totals",
	"/ark.market.v1.Query/ConversionMandate",
	"/ark.market.v1.Query/ConversionPolicy",
	"/ark.market.v1.Query/Params",
	"/ark.market.v1.Query/Pool",
	"/ark.market.v1.Query/Swap",
	"/ark.market.v1.Query/TobinTax",
	"/ark.market.v1.Query/TobinTaxOverrides",
	"/ark.oracle.v1.Query/Attendance",
	"/ark.oracle.v1.Query/ExchangeRate",
	"/ark.oracle.v1.Query/ExchangeRates",
	"/ark.oracle.v1.Query/Feeds",
	"/ark.oracle.v1.Query/Params",
	"/ark.oracle.v1.Query/ReferenceDenom",
	"/ark.oracle.v1.Query/RewardWeight",
	"/ark.reserve.v1.Query/Balance",
	"/ark.reserve.v1.Query/ClosedPositions",
	"/ark.reserve.v1.Query/Ledger",
	"/ark.reserve.v1.Query/Mandate",
	"/ark.reserve.v1.Query/OpenPositions",
	"/ark.reserve.v1.Query/Position",
	"/ark.reserve.v1.Query/RecognisedCapital",
	"/ark.reserve.v1.Query/RecognitionPolicy",
	"/ark.security.v1.Query/CommitteePlan",
	"/ark.security.v1.Query/SecurityMandate",
	"/ark.treasury.v1.Query/ComputeTax",
	"/ark.treasury.v1.Query/ConversionFactor",
	"/ark.treasury.v1.Query/ConversionFactors",
	"/ark.treasury.v1.Query/EconomicMandate",
	"/ark.treasury.v1.Query/EconomicPolicy",
	"/ark.treasury.v1.Query/ExposureStatus",
	"/ark.treasury.v1.Query/FundStatus",
	"/ark.treasury.v1.Query/GasPrice",
	"/ark.treasury.v1.Query/GasPrices",
	"/ark.treasury.v1.Query/Params",
	"/ark.treasury.v1.Query/RewardFunding",
	"/ark.treasury.v1.Query/TaxCap",
	"/ark.treasury.v1.Query/TaxCaps",
}

// moduleQuerySafePaths mirrors ibc-go's newModuleQuerySafeAllowList: every
// method annotated module_query_safe on a non-Msg service, sorted.
func moduleQuerySafePaths() []string {
	var paths []string
	gogoproto.GogoResolver.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			sd := fd.Services().Get(i)
			if service, ok := protov2.GetExtension(sd.Options(), msgv1.E_Service).(bool); ok && service {
				continue
			}
			for j := range sd.Methods().Len() {
				md := sd.Methods().Get(j)
				if safe, ok := protov2.GetExtension(md.Options(), queryv1.E_ModuleQuerySafe).(bool); ok && safe {
					paths = append(paths, fmt.Sprintf("/%s/%s", sd.FullName(), md.Name()))
				}
			}
		}
		return true
	})
	slices.Sort(paths)
	return paths
}

func arkPathsOnly(paths []string) []string {
	ark := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.HasPrefix(path, "/ark.") {
			ark = append(ark, path)
		}
	}
	return ark
}

func moduleQuerySafe(arkApp *app.ArkApp, path string) (*icahosttypes.MsgModuleQuerySafeResponse, error) {
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})
	return icahostkeeper.NewMsgServerImpl(arkApp.ICAHostKeeper).ModuleQuerySafe(ctx, &icahosttypes.MsgModuleQuerySafe{
		Signer:   authtypes.NewModuleAddress("interchain-account").String(),
		Requests: []icahosttypes.QueryRequest{{Path: path}},
	})
}

// The annotated RPC set must exactly match the proto definitions, keeping the ICA-reachable query
// surface explicit.
func TestModuleQuerySafePathsMatchTheProtos(t *testing.T) {
	require.Equal(t, arkModuleQuerySafePaths, arkPathsOnly(moduleQuerySafePaths()))
}

// FeedReferents is deterministic but deliberately unannotated: its claims are
// operator prose, and exposing them would make rewording one a state-machine
// change. This pins the exclusion against the walker and against the ICA host
// itself, so it cannot be annotated back by reflex.
func TestFeedReferentsStaysOutOfReach(t *testing.T) {
	const path = "/ark.oracle.v1.Query/FeedReferents"
	require.NotContains(t, moduleQuerySafePaths(), path)

	_, err := moduleQuerySafe(apptestutil.Setup(t, false), path)
	require.ErrorContains(t, err, "not module query safe")
}

// The ICA host serves the annotated set with no list of Ark's own in between:
// an annotated path answers through MsgModuleQuerySafe as it stands.
func TestInterchainAccountReachesAnnotatedQueries(t *testing.T) {
	response, err := moduleQuerySafe(apptestutil.Setup(t, false), "/ark.treasury.v1.Query/Params")
	require.NoError(t, err)
	require.Len(t, response.Responses, 1)
	require.NotEmpty(t, response.Responses[0])
}

// Every annotated path must route, or the ICA host would fail it at call time
// with a routing error rather than serve it.
func TestModuleQuerySafePathsRoute(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	for _, path := range arkPathsOnly(moduleQuerySafePaths()) {
		t.Run(path, func(t *testing.T) {
			require.NotNil(t, arkApp.GRPCQueryRouter().Route(path), "path must route")
		})
	}
}

// Repeated identical queries expose map-order nondeterminism. This smoke test cannot prove
// determinism for populated paths or detect stable node-local inputs.
func TestModuleQuerySafePathsAnswerDeterministically(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})

	for _, path := range arkPathsOnly(moduleQuerySafePaths()) {
		t.Run(path, func(t *testing.T) {
			route := arkApp.GRPCQueryRouter().Route(path)
			require.NotNil(t, route)

			first, firstErr := route(ctx, &cmtabci.RequestQuery{Path: path, Data: []byte{}})
			second, secondErr := route(ctx, &cmtabci.RequestQuery{Path: path, Data: []byte{}})

			require.Equal(t, fmt.Sprint(firstErr), fmt.Sprint(secondErr), "error must not vary")
			if firstErr == nil {
				require.Equal(t, first.Value, second.Value, "response bytes must not vary")
			}
		})
	}
}

// Every listed path must carry the annotation, route, and construct its
// response. Listing is a review the annotation does not replace, and the
// annotation is a claim listing does not restate; both are required.
func TestAcceptedQueriesAreAnnotatedRouteAndConstruct(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	annotated := moduleQuerySafePaths()
	require.Len(t, app.AcceptedQueries(), 48, "fourteen Ark paths and the SDK's thirty-four annotated auth, bank, and staking paths; widening is a review")

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
	direct, err := f.app.GRPCQueryRouter().Route(listed)(f.ctx, &cmtabci.RequestQuery{Data: request})
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
