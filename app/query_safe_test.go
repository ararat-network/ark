package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"
	icahostkeeper "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/keeper"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	msgv1 "cosmossdk.io/api/cosmos/msg/v1"
	queryv1 "cosmossdk.io/api/cosmos/query/v1"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// arkModuleQuerySafePaths is every Ark query the protos declare safe to serve
// in-consensus. The ICA host derives its MsgModuleQuerySafe allow list from
// this annotation inside ibc-go, with no chain-side override, so the set is
// consensus-reachable today. It is checked in so that widening it is a
// reviewable diff rather than a side effect of adding an RPC. The Wasm accept
// list is separate and hand-written (app/wasm_query.go).
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

func moduleQuerySafe(arkApp *ArkApp, path string) (*icahosttypes.MsgModuleQuerySafeResponse, error) {
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})
	return icahostkeeper.NewMsgServerImpl(arkApp.ICAHostKeeper).ModuleQuerySafe(ctx, &icahosttypes.MsgModuleQuerySafe{
		Signer:   authtypes.NewModuleAddress("interchain-account").String(),
		Requests: []icahosttypes.QueryRequest{{Path: path}},
	})
}

// The annotated set must match the protos exactly. A diff here means an RPC was
// added, removed, or had its annotation changed, and the ICA-reachable surface
// moved with it.
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

	_, err := moduleQuerySafe(Setup(t, false), path)
	require.ErrorContains(t, err, "not module query safe")
}

// The ICA host serves the annotated set with no list of Ark's own in between:
// an annotated path answers through MsgModuleQuerySafe as it stands.
func TestInterchainAccountReachesAnnotatedQueries(t *testing.T) {
	response, err := moduleQuerySafe(Setup(t, false), "/ark.treasury.v1.Query/Params")
	require.NoError(t, err)
	require.Len(t, response.Responses, 1)
	require.NotEmpty(t, response.Responses[0])
}

// Every annotated path must route, or the ICA host would fail it at call time
// with a routing error rather than serve it.
func TestModuleQuerySafePathsRoute(t *testing.T) {
	arkApp := Setup(t, false)

	for _, path := range arkPathsOnly(moduleQuerySafePaths()) {
		t.Run(path, func(t *testing.T) {
			require.NotNil(t, arkApp.GRPCQueryRouter().Route(path), "path must route")
		})
	}
}

// A smoke test for gross non-determinism: the same query against the same state
// must answer identically. Go randomises map iteration on every range, so a
// response assembled from a map diverges here quickly.
//
// It is a guard, not a proof. Zero-value requests do not reach the populated
// paths where ordering matters most, and it cannot see a query reading
// node-local state, which is the same on repeat by definition.
func TestModuleQuerySafePathsAnswerDeterministically(t *testing.T) {
	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})

	for _, path := range arkPathsOnly(moduleQuerySafePaths()) {
		t.Run(path, func(t *testing.T) {
			route := arkApp.GRPCQueryRouter().Route(path)
			require.NotNil(t, route)

			first, firstErr := route(ctx, &abci.RequestQuery{Path: path, Data: []byte{}})
			second, secondErr := route(ctx, &abci.RequestQuery{Path: path, Data: []byte{}})

			require.Equal(t, fmt.Sprint(firstErr), fmt.Sprint(secondErr), "error must not vary")
			if firstErr == nil {
				require.Equal(t, first.Value, second.Value, "response bytes must not vary")
			}
		})
	}
}
