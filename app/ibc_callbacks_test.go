package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
	arktestdata "github.com/ararat-network/ark/app/testdata"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// callbacksFixture joins two Ark chains with callback contracts and a reflect contract on A.
// Callback contracts count transfer callbacks; reflect supplies arbitrary memos for invalid and
// misdirected cases.
type callbacksFixture struct {
	coord          *ibctesting.Coordinator
	path           *ibctesting.Path
	chainA, chainB *ibctesting.TestChain
	appA, appB     *app.ArkApp
	contractA      sdk.AccAddress
	contractB      sdk.AccAddress
	reflectA       sdk.AccAddress
}

func newCallbacksFixture(t *testing.T) callbacksFixture {
	t.Helper()

	coord := apptestutil.NewIBCCoordinator(t, 2)
	chainA, chainB := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewTransferPath(chainA, chainB)
	path.Setup()

	f := callbacksFixture{
		coord: coord, path: path, chainA: chainA, chainB: chainB,
		appA: apptestutil.ArkChain(t, chainA), appB: apptestutil.ArkChain(t, chainB),
	}
	f.contractA = storeAndInstantiate(t, coord, chainA, arktestdata.IBCCallbacksContractWasm(), "callbacks")
	f.reflectA = storeAndInstantiate(t, coord, chainA, testdata.ReflectContractWasm(), "reflect")
	f.contractB = storeAndInstantiate(t, coord, chainB, arktestdata.IBCCallbacksContractWasm(), "callbacks")

	ctx := chainA.GetContext()
	noah := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000))
	apptestutil.FundAccount(t, f.appA, ctx, f.contractA, noah)
	apptestutil.FundAccount(t, f.appA, ctx, f.reflectA, noah)
	coord.CommitBlock(chainA)
	return f
}

// storeAndInstantiate puts a contract on the chain, owned by its sender,
// through the state the next block commits.
func storeAndInstantiate(t *testing.T, coord *ibctesting.Coordinator, c *ibctesting.TestChain, code []byte, label string) sdk.AccAddress {
	t.Helper()

	arkApp := apptestutil.ArkChain(t, c)
	ctx := c.GetContext()
	sender := c.SenderAccount.GetAddress()
	pk := wasmkeeper.NewDefaultPermissionKeeper(&arkApp.WasmKeeper)
	codeID, _, err := pk.Create(ctx, sender, code, nil)
	require.NoError(t, err)
	contract, _, err := pk.Instantiate(ctx, codeID, sender, nil, []byte("{}"), label, nil)
	require.NoError(t, err)
	coord.CommitBlock(c)
	return contract
}

// transfer has the callbacks contract on A send NOAH over the channel,
// asking for callbacks of the given kind: "both" names itself as the source
// callback and the receiver as the destination one; "dst" names only the
// receiver.
func (f callbacksFixture) transfer(t *testing.T, to sdk.AccAddress, callbackType string, timeoutSeconds uint64) channeltypes.Packet {
	t.Helper()

	body := fmt.Sprintf(`{"transfer":{"to_address":%q,"channel_id":%q,"timeout_seconds":%d,"callback_type":%q}}`,
		to.String(), f.path.EndpointA.ChannelID, timeoutSeconds, callbackType)
	res, err := f.chainA.SendMsgs(&wasmtypes.MsgExecuteContract{
		Sender: f.chainA.SenderAccount.GetAddress().String(), Contract: f.contractA.String(), Msg: []byte(body),
		Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)),
	})
	require.NoError(t, err)
	packet, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	return packet
}

// reflectTransfer has the reflect contract on A send NOAH with the memo
// given, verbatim.
func (f callbacksFixture) reflectTransfer(t *testing.T, memo string) channeltypes.Packet {
	t.Helper()

	res, err := f.chainA.SendMsgs(&wasmtypes.MsgExecuteContract{
		Sender: f.chainA.SenderAccount.GetAddress().String(), Contract: f.reflectA.String(),
		Msg: reflectBody(t, wasmvmtypes.CosmosMsg{IBC: &wasmvmtypes.IBCMsg{Transfer: &wasmvmtypes.TransferMsg{
			ChannelID: f.path.EndpointA.ChannelID,
			ToAddress: f.chainB.SenderAccount.GetAddress().String(),
			Amount:    wasmvmtypes.Coin{Denom: chain.NoahBaseDenom, Amount: "1000"},
			Timeout:   wasmvmtypes.IBCTimeout{Timestamp: f.chainA.GetTimeoutTimestamp()},
			Memo:      memo,
		}}}),
	})
	require.NoError(t, err)
	packet, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	return packet
}

type callbackStats struct {
	Acks         []json.RawMessage `json:"ibc_ack_callbacks"`
	Timeouts     []json.RawMessage `json:"ibc_timeout_callbacks"`
	Destinations []json.RawMessage `json:"ibc_destination_callbacks"`
}

func stats(t *testing.T, arkApp *app.ArkApp, c *ibctesting.TestChain, contract sdk.AccAddress) callbackStats {
	t.Helper()

	raw, err := arkApp.WasmKeeper.QuerySmart(c.GetContext(), contract, []byte(`{"callback_stats":{}}`))
	require.NoError(t, err)
	var s callbackStats
	require.NoError(t, json.Unmarshal(raw, &s))
	return s
}

func (f callbacksFixture) escrow(t *testing.T) math.Int {
	t.Helper()
	escrow := ibctransfertypes.GetEscrowAddress(f.path.EndpointA.ChannelConfig.PortID, f.path.EndpointA.ChannelID)
	return balance(t, f.chainA, escrow, chain.NoahBaseDenom)
}

func (f callbacksFixture) voucher() string {
	return ibctransfertypes.NewDenom(chain.NoahBaseDenom,
		ibctransfertypes.NewHop(f.path.EndpointB.ChannelConfig.PortID, f.path.EndpointB.ChannelID)).IBCDenom()
}

func (f callbacksFixture) committed(t *testing.T, packet channeltypes.Packet) bool {
	t.Helper()
	return len(f.appA.IBCKeeper.ChannelKeeper.GetPacketCommitment(
		f.chainA.GetContext(), packet.SourcePort, packet.SourceChannel, packet.Sequence)) > 0
}

// TestCallbacksReachTheContractsOnBothChains is transfer-and-call end to
// end: the destination contract sees the receive, and once the
// acknowledgement relays back the source contract sees it too.
func TestCallbacksReachTheContractsOnBothChains(t *testing.T) {
	f := newCallbacksFixture(t)

	packet := f.transfer(t, f.contractB, "both", 300)
	require.NoError(t, f.path.RelayPacket(packet))

	require.Len(t, stats(t, f.appA, f.chainA, f.contractA).Acks, 1, "the source contract saw the acknowledgement")
	require.Len(t, stats(t, f.appB, f.chainB, f.contractB).Destinations, 1, "the destination contract saw the receive")
	require.Equal(t, math.NewInt(1_000), balance(t, f.chainB, f.contractB, f.voucher()))
	require.Equal(t, math.NewInt(1_000), f.escrow(t))
}

// TestTimeoutCallbackReachesTheSourceContract: an unrelayed transfer times
// out, the contract is told, and its NOAH comes back.
func TestTimeoutCallbackReachesTheSourceContract(t *testing.T) {
	f := newCallbacksFixture(t)

	packet := f.transfer(t, f.contractB, "both", 60)
	f.coord.IncrementTimeBy(2 * time.Minute)
	f.coord.CommitBlock(f.chainB)
	require.NoError(t, f.path.EndpointA.UpdateClient())
	require.NoError(t, f.path.EndpointA.TimeoutPacket(packet))

	require.Len(t, stats(t, f.appA, f.chainA, f.contractA).Timeouts, 1, "the source contract saw the timeout")
	require.True(t, f.escrow(t).IsZero(), "the escrow is refunded")
	// The thousand it sent arrived as the signer's attached funds, so the
	// refund leaves the contract holding its million and that thousand.
	require.Equal(t, math.NewInt(1_001_000), balance(t, f.chainA, f.contractA, chain.NoahBaseDenom))
}

// TestDestinationCallbackFailureFailsTheReceive: a destination callback that
// cannot run (the receiver is not a contract) fails the receive whole, so the
// error acknowledgement refunds the source contract.
func TestDestinationCallbackFailureFailsTheReceive(t *testing.T) {
	f := newCallbacksFixture(t)
	plain := f.chainB.SenderAccount.GetAddress()

	packet := f.transfer(t, plain, "dst", 300)
	require.NoError(t, f.path.RelayPacket(packet))

	require.True(t, balance(t, f.chainB, plain, f.voucher()).IsZero(), "the receive rolled back")
	require.True(t, f.escrow(t).IsZero(), "the source contract was refunded")
	require.Empty(t, stats(t, f.appA, f.chainA, f.contractA).Acks, "no source callback was asked for")
}

// TestSourceCallbackMustComeFromTheContract: a signed transfer naming a
// contract as its source callback is refused at send, so nobody can have a
// contract called back for a packet it did not send.
func TestSourceCallbackMustComeFromTheContract(t *testing.T) {
	f := newCallbacksFixture(t)
	memo := fmt.Sprintf(`{"src_callback":{"address":%q}}`, f.contractA.String())

	_, err := f.chainA.SendMsgs(ibctransfertypes.NewMsgTransfer(
		f.path.EndpointA.ChannelConfig.PortID, f.path.EndpointA.ChannelID,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
		f.chainA.SenderAccount.GetAddress().String(), f.chainB.SenderAccount.GetAddress().String(),
		clienttypes.NewHeight(1, 1_000), 0, memo,
	))
	require.Error(t, err)
	require.True(t, f.escrow(t).IsZero(), "nothing was escrowed")
}

// TestSourceCallbackFailureDoesNotBlockTheAcknowledgement: the reflect
// contract has no callback entry point, so its acknowledgement callback
// fails; the acknowledgement still settles the packet, with a gas limit above
// the cap clamped rather than trusted.
func TestSourceCallbackFailureDoesNotBlockTheAcknowledgement(t *testing.T) {
	for name, gasLimit := range map[string]string{
		"default gas":       "",
		"gas above the cap": `,"gas_limit":"999999999999"`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newCallbacksFixture(t)
			memo := fmt.Sprintf(`{"src_callback":{"address":%q%s}}`, f.reflectA.String(), gasLimit)

			packet := f.reflectTransfer(t, memo)
			require.True(t, f.committed(t, packet))
			require.NoError(t, f.path.RelayPacket(packet))

			require.False(t, f.committed(t, packet), "the acknowledgement settled the packet")
			require.Equal(t, math.NewInt(1_000), f.escrow(t), "a successful transfer keeps its escrow")
		})
	}
}

// TestMalformedCallbackMetadataIsIgnored: a memo that names no usable
// callback is not a callback, and the transfer proceeds without one.
func TestMalformedCallbackMetadataIsIgnored(t *testing.T) {
	f := newCallbacksFixture(t)

	packet := f.reflectTransfer(t, `{"src_callback":"nonsense"}`)
	require.NoError(t, f.path.RelayPacket(packet))

	require.False(t, f.committed(t, packet))
	require.Equal(t, math.NewInt(1_000), balance(t, f.chainB, f.chainB.SenderAccount.GetAddress(), f.voucher()))
}
