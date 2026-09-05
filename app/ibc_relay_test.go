package app_test

import (
	"errors"
	"testing"
	"time"

	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// enableStableTax puts a ten percent transfer tax with a wide cap on the
// chain's stablecoin, written into the state its next block commits.
func enableStableTax(t *testing.T, c *ibctesting.TestChain) {
	t.Helper()

	arkApp := apptestutil.ArkChain(t, c)
	ctx := c.GetContext()
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	treasurytestutil.SetDerivedTaxCap(t, arkApp.TreasuryKeeper, ctx, chain.USDBaseDenom, math.NewInt(1_000_000))
}

func stableBalance(t *testing.T, c *ibctesting.TestChain, addr sdk.AccAddress) math.Int {
	t.Helper()
	return apptestutil.ArkChain(t, c).BankKeeper.GetBalance(c.GetContext(), addr, chain.USDBaseDenom).Amount
}

func collectorAddress(t *testing.T, c *ibctesting.TestChain) sdk.AccAddress {
	t.Helper()
	return apptestutil.ArkChain(t, c).AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
}

func balance(t *testing.T, c *ibctesting.TestChain, addr sdk.AccAddress, denom string) math.Int {
	t.Helper()
	return apptestutil.ArkChain(t, c).BankKeeper.GetBalance(c.GetContext(), addr, denom).Amount
}

func transfer(t *testing.T, path *ibctesting.Path, token sdk.Coin, receiver string, memo string) channeltypes.Packet {
	t.Helper()

	msg := ibctransfertypes.NewMsgTransfer(
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID, token,
		path.EndpointA.Chain.SenderAccount.GetAddress().String(), receiver,
		clienttypes.NewHeight(1, 1_000), 0, memo,
	)
	res, err := path.EndpointA.Chain.SendMsgs(msg)
	require.NoError(t, err)
	packet, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	return packet
}

func forwardMemo(t *testing.T, receiver string, endpoint *ibctesting.Endpoint, timeout time.Duration) string {
	t.Helper()

	retries := uint8(0)
	memo, err := packetforwardtypes.PacketMetadata{Forward: packetforwardtypes.ForwardMetadata{
		Receiver: receiver,
		Port:     endpoint.ChannelConfig.PortID,
		Channel:  endpoint.ChannelID,
		Timeout:  timeout,
		Retries:  &retries,
	}}.ToMemo()
	require.NoError(t, err)
	return memo
}

// TestRelayedTransferEscrowsAndCredits is the round trip the direct packet
// tests cannot make: a real client, connection, and channel between two Ark
// chains, a transfer signed through Ark's ante, relayed, and acknowledged.
func TestRelayedTransferEscrowsAndCredits(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 2)
	path := ibctesting.NewTransferPath(coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2)))
	path.Setup()

	receiver := path.EndpointB.Chain.SenderAccount.GetAddress()
	token := sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)
	packet := transfer(t, path, token, receiver.String(), "")
	require.NoError(t, path.RelayPacket(packet))

	escrow := ibctransfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	require.Equal(t, token.Amount, balance(t, path.EndpointA.Chain, escrow, chain.NoahBaseDenom), "escrow holds the sent NOAH")
	voucher := ibctransfertypes.NewDenom(chain.NoahBaseDenom,
		ibctransfertypes.NewHop(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)).IBCDenom()
	require.Equal(t, token.Amount, balance(t, path.EndpointB.Chain, receiver, voucher), "the receiver holds the voucher")
}

// TestPacketForwardHopsThroughAnArkHub is the multi-hop success path: a
// transfer from A lands on the hub with a forward memo, the hub sends it on
// to C without a signed transaction, C credits a two-hop voucher, and the
// hub acknowledges A only once C has acknowledged it.
func TestPacketForwardHopsThroughAnArkHub(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 3)
	chainA, hub, chainC := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2)), coord.GetChain(ibctesting.GetChainID(3))
	pathAH := ibctesting.NewTransferPath(chainA, hub)
	pathAH.Setup()
	pathHC := ibctesting.NewTransferPath(hub, chainC)
	pathHC.Setup()

	final := chainC.SenderAccount.GetAddress()
	token := sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)
	packet := transfer(t, pathAH, token, hub.SenderAccount.GetAddress().String(), forwardMemo(t, final.String(), pathHC.EndpointA, 0))

	require.NoError(t, pathAH.EndpointB.UpdateClient())
	res, err := pathAH.EndpointB.RecvPacketWithResult(packet)
	require.NoError(t, err)
	forwarded, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	require.Equal(t, pathHC.EndpointA.ChannelID, forwarded.SourceChannel, "the hub forwards on its channel to C")
	_, acked := apptestutil.ArkChain(t, hub).IBCKeeper.ChannelKeeper.GetPacketAcknowledgement(
		hub.GetContext(), packet.DestinationPort, packet.DestinationChannel, packet.Sequence)
	require.False(t, acked, "the hub does not acknowledge A before C answers")

	require.NoError(t, pathHC.EndpointB.UpdateClient())
	resC, err := pathHC.EndpointB.RecvPacketWithResult(forwarded)
	require.NoError(t, err)
	ackC, err := ibctesting.ParseAckFromEvents(resC.Events)
	require.NoError(t, err)
	require.NoError(t, pathHC.EndpointA.UpdateClient())
	require.NoError(t, pathHC.EndpointA.AcknowledgePacket(forwarded, ackC))

	// The hub passes C's acknowledgement through to A unchanged.
	require.Equal(t, channeltypes.CommitAcknowledgement(ackC), hub.GetAcknowledgement(packet))
	require.NoError(t, pathAH.EndpointA.UpdateClient())
	require.NoError(t, pathAH.EndpointA.AcknowledgePacket(packet, ackC))

	twoHop := ibctransfertypes.NewDenom(chain.NoahBaseDenom,
		ibctransfertypes.NewHop(pathHC.EndpointB.ChannelConfig.PortID, pathHC.EndpointB.ChannelID),
		ibctransfertypes.NewHop(pathAH.EndpointB.ChannelConfig.PortID, pathAH.EndpointB.ChannelID),
	).IBCDenom()
	require.Equal(t, token.Amount, balance(t, chainC, final, twoHop), "C credits the two-hop voucher")
	oneHop := ibctransfertypes.NewDenom(chain.NoahBaseDenom,
		ibctransfertypes.NewHop(pathAH.EndpointB.ChannelConfig.PortID, pathAH.EndpointB.ChannelID)).IBCDenom()
	require.True(t, balance(t, hub, hub.SenderAccount.GetAddress(), oneHop).IsZero(), "the hub keeps nothing")
	escrow := ibctransfertypes.GetEscrowAddress(pathAH.EndpointA.ChannelConfig.PortID, pathAH.EndpointA.ChannelID)
	require.Equal(t, token.Amount, balance(t, chainA, escrow, chain.NoahBaseDenom), "A's escrow still holds the NOAH")
}

// TestPacketForwardToAnUnknownChannelRefunds: a forward the hub cannot make
// fails the receive whole, and the error acknowledgement refunds A.
func TestPacketForwardToAnUnknownChannelRefunds(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 2)
	chainA, hub := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewTransferPath(chainA, hub)
	path.Setup()

	sender := chainA.SenderAccount.GetAddress()
	// The sender also relays and pays NOAH fees, so the token under test is
	// the stablecoin, which nothing but the transfer touches.
	apptestutil.FundAccount(t, apptestutil.ArkChain(t, chainA), chainA.GetContext(), sender,
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))
	token := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)
	unknown := &ibctesting.Endpoint{ChannelID: "channel-99", ChannelConfig: &ibctesting.ChannelConfig{PortID: ibctransfertypes.PortID}}
	packet := transfer(t, path, token, hub.SenderAccount.GetAddress().String(), forwardMemo(t, sender.String(), unknown, 0))
	require.Equal(t, math.NewInt(9_000), stableBalance(t, chainA, sender), "escrowed")
	require.NoError(t, path.RelayPacket(packet))

	escrow := ibctransfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	require.True(t, stableBalance(t, chainA, escrow).IsZero(), "the escrow is released")
	require.Equal(t, math.NewInt(10_000), stableBalance(t, chainA, sender), "the stablecoin is back with the sender")
}

// TestPacketForwardTimeoutRefunds: a forwarded hop that times out with no
// retries left turns into an error acknowledgement for the original packet,
// and A refunds.
func TestPacketForwardTimeoutRefunds(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 3)
	chainA, hub, chainC := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2)), coord.GetChain(ibctesting.GetChainID(3))
	pathAH := ibctesting.NewTransferPath(chainA, hub)
	pathAH.Setup()
	pathHC := ibctesting.NewTransferPath(hub, chainC)
	pathHC.Setup()

	token := sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)
	packet := transfer(t, pathAH, token, hub.SenderAccount.GetAddress().String(),
		forwardMemo(t, chainC.SenderAccount.GetAddress().String(), pathHC.EndpointA, time.Minute))
	require.NoError(t, pathAH.EndpointB.UpdateClient())
	res, err := pathAH.EndpointB.RecvPacketWithResult(packet)
	require.NoError(t, err)
	forwarded, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)

	// Nobody relays the hop. Let it expire on C, then prove the timeout to the hub.
	coord.IncrementTimeBy(2 * time.Minute)
	coord.CommitBlock(chainC)
	require.NoError(t, pathHC.EndpointA.UpdateClient())
	require.NoError(t, pathHC.EndpointA.TimeoutPacket(forwarded))

	// With no retries left the hub answers A with an error acknowledgement,
	// which ibc-go redacts to its code, so the bytes are reconstructible.
	errorAck := channeltypes.NewErrorAcknowledgement(errors.New("forward timed out")).Acknowledgement()
	require.Equal(t, channeltypes.CommitAcknowledgement(errorAck), hub.GetAcknowledgement(packet), "the hub answers A with an error acknowledgement")
	require.NoError(t, pathAH.EndpointA.UpdateClient())
	require.NoError(t, pathAH.EndpointA.AcknowledgePacket(packet, errorAck))

	escrow := ibctransfertypes.GetEscrowAddress(pathAH.EndpointA.ChannelConfig.PortID, pathAH.EndpointA.ChannelID)
	require.True(t, balance(t, chainA, escrow, chain.NoahBaseDenom).IsZero(), "A's escrow is released")
}

// TestForwardedHopIsNotTaxedAgain: the ante taxes the signed transfer once;
// the hub's protocol-generated hop and the return to A create no second
// assessment on either chain.
func TestForwardedHopIsNotTaxedAgain(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 2)
	chainA, hub := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewTransferPath(chainA, hub)
	path.Setup()

	enableStableTax(t, chainA)
	enableStableTax(t, hub)
	sender := chainA.SenderAccount.GetAddress()
	apptestutil.FundAccount(t, apptestutil.ArkChain(t, chainA), chainA.GetContext(), sender,
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))
	homeAgain := authtypes.NewModuleAddress("home-again")

	token := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)
	packet := transfer(t, path, token, hub.SenderAccount.GetAddress().String(), forwardMemo(t, homeAgain.String(), path.EndpointB, 0))
	require.Equal(t, math.NewInt(100), stableBalance(t, chainA, collectorAddress(t, chainA)), "the ante taxed the transfer once")

	require.NoError(t, path.EndpointB.UpdateClient())
	res, err := path.EndpointB.RecvPacketWithResult(packet)
	require.NoError(t, err)
	returned, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	require.NoError(t, path.EndpointA.UpdateClient())
	resA, err := path.EndpointA.RecvPacketWithResult(returned)
	require.NoError(t, err)
	ackA, err := ibctesting.ParseAckFromEvents(resA.Events)
	require.NoError(t, err)
	require.NoError(t, path.EndpointB.UpdateClient())
	require.NoError(t, path.EndpointB.AcknowledgePacket(returned, ackA))
	require.NoError(t, path.EndpointA.UpdateClient())
	require.NoError(t, path.EndpointA.AcknowledgePacket(packet, ackA))

	require.Equal(t, math.NewInt(1_000), stableBalance(t, chainA, homeAgain), "the stablecoin came home unwound")
	require.Equal(t, math.NewInt(100), stableBalance(t, chainA, collectorAddress(t, chainA)), "no second tax on A")
	require.True(t, stableBalance(t, hub, collectorAddress(t, hub)).IsZero(), "no tax on the hub")
}
