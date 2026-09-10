package app_test

import (
	"encoding/json"
	"testing"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	icacontrollertypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/types"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ratelimitingv2 "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/v2"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v11/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

var _ ibctesting.TestingApp = (*app.ArkApp)(nil)

func TestIBCWiring(t *testing.T) {
	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	require.NotNil(t, arkApp.IBCKeeper)
	require.NotNil(t, arkApp.TransferKeeper)
	require.NotNil(t, arkApp.RateLimitKeeper)
	require.NotNil(t, arkApp.PacketForwardKeeper)
	require.NotNil(t, arkApp.ICAControllerKeeper)
	require.NotNil(t, arkApp.ICAHostKeeper)

	// Rate limiting is outermost on both stacks, which is also what keeps the
	// callbacks middleware underneath it: a callback must not run for a packet
	// the limiter refused.
	transferRoute, ok := arkApp.IBCKeeper.PortKeeper.Route(ibctransfertypes.ModuleName)
	require.True(t, ok)
	require.IsType(t, &ratelimiting.IBCMiddleware{}, transferRoute)

	// Native contract channels, distinct from the callbacks path over transfer.
	wasmRoute, ok := arkApp.IBCKeeper.PortKeeper.Route(wasmtypes.ModuleName)
	require.True(t, ok)
	require.IsType(t, wasm.IBCHandler{}, wasmRoute)

	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icacontrollertypes.SubModuleName)
	require.True(t, ok)
	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icahosttypes.SubModuleName)
	require.True(t, ok)
	require.True(t, arkApp.IBCKeeper.PortKeeper.Router.Sealed())

	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(ibctransfertypes.PortID))
	require.IsType(
		t,
		ratelimitingv2.IBCMiddleware{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(ibctransfertypes.PortID),
	)
	// Contract v2 ports are per-contract, so any port under the prefix resolves.
	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(
		wasmkeeper.PortIDPrefixV2+"1contractaddress",
	))

	for _, moduleName := range []string{
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		ratelimittypes.ModuleName,
		packetforwardtypes.ModuleName,
		icatypes.ModuleName,
		ibctm.ModuleName,
		ibcwasmtypes.ModuleName,
	} {
		require.Contains(t, arkApp.ModuleManager.Modules, moduleName)
	}

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		ibcexported.ModuleName,
		ratelimittypes.ModuleName,
	)
	require.NotContains(t, arkApp.ModuleManager.OrderEndBlockers, ibcexported.ModuleName)
}

// TestLightClientRoutes pins the client router: both wired light clients
// resolve, and an unwired client type is refused. Route reads the allowed
// clients param, so this runs over applied genesis rather than a bare app.
func TestLightClientRoutes(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(true)

	for _, clientID := range []string{"07-tendermint-0", "08-wasm-0"} {
		route, err := arkApp.IBCKeeper.ClientKeeper.Route(ctx, clientID)
		require.NoError(t, err, clientID)
		require.NotNil(t, route, clientID)
	}

	_, err := arkApp.IBCKeeper.ClientKeeper.Route(ctx, "06-solomachine-0")
	require.Error(t, err, "unwired client type must not route")
}

// TestIBCWiring asserts the transfer stack's shape — that rate limiting sits
// outermost, with the callbacks middleware beneath it. These drive a packet
// through that stack instead, which is the only thing that shows the order is
// load-bearing rather than merely declared: a limiter that ran after the
// transfer would mint the voucher before refusing the packet, and a structural
// assertion cannot tell the difference.

const (
	// The counterparty's side of the transfer channel, and ours.
	counterpartyChannel = "channel-0"
	arkChannel          = "channel-0"
	// A denomination native to the counterparty, so a receive mints a voucher
	// rather than unescrowing.
	foreignDenom = "uatom"
)

// transferStack returns the top of the routed transfer stack, which is what a
// relayer's packet actually enters.
func transferStack(t *testing.T, arkApp *app.ArkApp) porttypes.IBCModule {
	t.Helper()

	route, ok := arkApp.IBCKeeper.PortKeeper.Route(ibctransfertypes.PortID)
	require.True(t, ok, "the transfer port must be routed")

	return route
}

// incomingPacket builds a relayed ICS-20 packet carrying a counterparty-native
// denomination to the given receiver.
func incomingPacket(t *testing.T, receiver sdk.AccAddress, amount string) channeltypes.Packet {
	t.Helper()

	data, err := json.Marshal(ibctransfertypes.NewFungibleTokenPacketData(
		foreignDenom, amount, "cosmos1sender", receiver.String(), "",
	))
	require.NoError(t, err)

	return channeltypes.NewPacket(
		data,
		1,
		ibctransfertypes.PortID, counterpartyChannel,
		ibctransfertypes.PortID, arkChannel,
		clienttypes.NewHeight(1, 1_000), 0,
	)
}

// voucherDenom is what a counterparty-native denomination becomes once it has
// crossed into this chain.
func voucherDenom() string {
	return ibctransfertypes.NewDenom(
		foreignDenom,
		ibctransfertypes.NewHop(ibctransfertypes.PortID, arkChannel),
	).IBCDenom()
}

// TestTransferStackDeliversAnIncomingPacket drives a relayed packet through
// the whole routed stack — rate limiting, callbacks, packet forwarding, and
// the transfer module beneath them — and holds it to the outcome that matters:
// the voucher is minted to the named receiver.
func TestTransferStackDeliversAnIncomingPacket(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(false)
	receiver := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	ack := transferStack(t, arkApp).OnRecvPacket(
		ctx, ibctransfertypes.V1, incomingPacket(t, receiver, "1000"), receiver,
	)

	require.NotNil(t, ack)
	require.True(t, ack.Success(), string(ack.Acknowledgement()))
	require.Equal(t,
		sdk.NewInt64Coin(voucherDenom(), 1000),
		arkApp.BankKeeper.GetBalance(ctx, receiver, voucherDenom()),
	)
}

// TestRateLimiterRefusesBeforeTheTransferRuns is the ordering proof. The
// limiter is outermost so that a refused packet never reaches the transfer
// module: were it underneath, the voucher would already be minted by the time
// the limit was consulted, and the error acknowledgement would be a lie.
func TestRateLimiterRefusesBeforeTheTransferRuns(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(false)
	receiver := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	// A quota admitting no inflow at all, so the first packet is over it.
	arkApp.RateLimitKeeper.SetRateLimit(ctx, ratelimittypes.RateLimit{
		Path: &ratelimittypes.Path{
			Denom:             voucherDenom(),
			ChannelOrClientId: arkChannel,
		},
		Quota: &ratelimittypes.Quota{
			MaxPercentSend: math.ZeroInt(),
			MaxPercentRecv: math.ZeroInt(),
			DurationHours:  24,
		},
		Flow: &ratelimittypes.Flow{
			Inflow:       math.ZeroInt(),
			Outflow:      math.ZeroInt(),
			ChannelValue: math.NewInt(1_000_000),
		},
	})

	ack := transferStack(t, arkApp).OnRecvPacket(
		ctx, ibctransfertypes.V1, incomingPacket(t, receiver, "1000"), receiver,
	)

	require.NotNil(t, ack)
	require.False(t, ack.Success(), "a packet over the quota must be refused")
	require.True(t,
		arkApp.BankKeeper.GetBalance(ctx, receiver, voucherDenom()).IsZero(),
		"the transfer module must not have run beneath the refusal",
	)
}

// escrowOutbound puts a channel's escrow into the state an outbound transfer
// leaves behind: the principal held by the escrow account, and the module's
// own total for that denomination raised to match. A refund reads both, so
// funding the account alone would underflow the total rather than pay out.
func escrowOutbound(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, amount int64) (sdk.AccAddress, sdk.Coin) {
	t.Helper()

	escrow := ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, arkChannel)
	sent := sdk.NewInt64Coin(chain.NoahBaseDenom, amount)
	apptestutil.FundAccount(t, arkApp, ctx, escrow, sdk.NewCoins(sent))
	arkApp.TransferKeeper.SetTotalEscrowForDenom(ctx, sent)

	return escrow, sent
}

// TestTransferStackRefundsATimeout pins the other direction through the same
// stack: a packet this chain sent that never arrived returns its escrow to the
// sender. Without it, a timeout would strand the principal in the escrow
// account with nothing to release it.
func TestTransferStackRefundsATimeout(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(false)
	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	// An outbound transfer holds the principal in the channel's escrow
	// account until the packet resolves; the refund pays back out of it.
	escrow, sent := escrowOutbound(t, arkApp, ctx, 5_000)

	data, err := json.Marshal(ibctransfertypes.NewFungibleTokenPacketData(
		chain.NoahBaseDenom, sent.Amount.String(), sender.String(), "cosmos1receiver", "",
	))
	require.NoError(t, err)
	packet := channeltypes.NewPacket(
		data,
		1,
		ibctransfertypes.PortID, arkChannel,
		ibctransfertypes.PortID, counterpartyChannel,
		clienttypes.NewHeight(1, 1_000), 0,
	)

	require.NoError(t, transferStack(t, arkApp).OnTimeoutPacket(ctx, ibctransfertypes.V1, packet, sender))

	require.Equal(t, sent, arkApp.BankKeeper.GetBalance(ctx, sender, chain.NoahBaseDenom))
	require.True(t, arkApp.BankKeeper.GetBalance(ctx, escrow, chain.NoahBaseDenom).IsZero())
}

// TestTransferStackRefundsAFailedAcknowledgement covers the third resolution a
// sent packet can take: it arrived, the counterparty refused it, and the
// escrow comes back the same way a timeout returns it.
func TestTransferStackRefundsAFailedAcknowledgement(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(false)
	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	_, sent := escrowOutbound(t, arkApp, ctx, 7_500)

	data, err := json.Marshal(ibctransfertypes.NewFungibleTokenPacketData(
		chain.NoahBaseDenom, sent.Amount.String(), sender.String(), "cosmos1receiver", "",
	))
	require.NoError(t, err)
	packet := channeltypes.NewPacket(
		data,
		1,
		ibctransfertypes.PortID, arkChannel,
		ibctransfertypes.PortID, counterpartyChannel,
		clienttypes.NewHeight(1, 1_000), 0,
	)
	refusal := channeltypes.NewErrorAcknowledgement(ibctransfertypes.ErrInvalidAmount)

	require.NoError(t, transferStack(t, arkApp).OnAcknowledgementPacket(
		ctx, ibctransfertypes.V1, packet, refusal.Acknowledgement(), sender,
	))

	require.Equal(t, sent, arkApp.BankKeeper.GetBalance(ctx, sender, chain.NoahBaseDenom))
}

// TestTransferStackKeepsAnAcceptedTransferEscrowed is the negative of the
// refunds above: a successful acknowledgement resolves the packet without
// paying anything back, so a middleware that refunded on every acknowledgement
// would show up here rather than as a slow leak.
func TestTransferStackKeepsAnAcceptedTransferEscrowed(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(false)
	sender := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	escrow, sent := escrowOutbound(t, arkApp, ctx, 2_500)

	data, err := json.Marshal(ibctransfertypes.NewFungibleTokenPacketData(
		chain.NoahBaseDenom, sent.Amount.String(), sender.String(), "cosmos1receiver", "",
	))
	require.NoError(t, err)
	packet := channeltypes.NewPacket(
		data,
		1,
		ibctransfertypes.PortID, arkChannel,
		ibctransfertypes.PortID, counterpartyChannel,
		clienttypes.NewHeight(1, 1_000), 0,
	)

	require.NoError(t, transferStack(t, arkApp).OnAcknowledgementPacket(
		ctx, ibctransfertypes.V1, packet, channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement(), sender,
	))

	require.True(t, arkApp.BankKeeper.GetBalance(ctx, sender, chain.NoahBaseDenom).IsZero())
	require.Equal(t, sent, arkApp.BankKeeper.GetBalance(ctx, escrow, chain.NoahBaseDenom))
}
