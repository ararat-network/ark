package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// The chain has no IBC, and Wasmd is built over nil channel keepers. These
// pin that every IBC surface a contract can reach stops at the seam the app
// replaced, never at a nil dereference.

// Every IBC and IBC v2 variant a contract can dispatch stops in the encoder
// with Wasmd's own unsupported error, before any router or keeper.
func TestWasmHandlerRefusesEveryIBCMessage(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})
	handler := arkApp.WasmMessageHandler()
	contract := authtypes.NewModuleAddress("islander")

	for name, msg := range map[string]wasmvmtypes.CosmosMsg{
		"transfer": {IBC: &wasmvmtypes.IBCMsg{Transfer: &wasmvmtypes.TransferMsg{
			ChannelID: "channel-0", ToAddress: "receiver", Amount: wasmvmtypes.NewCoin(1, chain.USDBaseDenom),
		}}},
		"send packet": {IBC: &wasmvmtypes.IBCMsg{SendPacket: &wasmvmtypes.SendPacketMsg{
			ChannelID: "channel-0", Data: []byte("{}"),
		}}},
		"write acknowledgement": {IBC: &wasmvmtypes.IBCMsg{WriteAcknowledgement: &wasmvmtypes.WriteAcknowledgementMsg{
			ChannelID: "channel-0",
		}}},
		"close channel": {IBC: &wasmvmtypes.IBCMsg{CloseChannel: &wasmvmtypes.CloseChannelMsg{ChannelID: "channel-0"}}},
		"v2 send packet": {IBC2: &wasmvmtypes.IBC2Msg{SendPacket: &wasmvmtypes.IBC2SendPacketMsg{
			SourceClient: "07-tendermint-0",
		}}},
		"v2 write acknowledgement": {IBC2: &wasmvmtypes.IBC2Msg{WriteAcknowledgement: &wasmvmtypes.IBC2WriteAcknowledgementMsg{
			SourceClient: "07-tendermint-0",
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := handler.DispatchMsg(ctx, contract, wasmkeeper.PortIDForContract(contract), msg)
			require.ErrorIs(t, err, wasmtypes.ErrUnsupportedForContract)
			require.ErrorContains(t, err, "ibc not supported")
		})
	}
}

// Through the VM: a contract's ICS-20 transfer fails the execution with that
// error, and neither principal nor tax moves.
func TestContractIBCTransferIsRefused(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "islander")
	funded := f.stable(contract)
	require.True(t, funded.GT(math.NewInt(1_000)), "the fixture funds the contract past the transfer")

	err := f.execute(t, contract, f.owner, wasmvmtypes.CosmosMsg{IBC: &wasmvmtypes.IBCMsg{Transfer: &wasmvmtypes.TransferMsg{
		ChannelID: "channel-0",
		ToAddress: f.recipient.String(),
		Amount:    wasmvmtypes.Coin{Denom: chain.USDBaseDenom, Amount: "1000"},
		Timeout:   wasmvmtypes.IBCTimeout{Timestamp: 1},
	}}})
	require.ErrorContains(t, err, "ibc not supported")
	require.Equal(t, funded, f.stable(contract))
	require.True(t, f.stable(f.collector()).IsZero())
}

// Every IBC query a contract can make is refused by the query plugin; the
// VM reports the refusal to the contract as an unsupported query kind.
func TestContractIBCQueriesAreRefused(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "islander")

	for name, req := range map[string]wasmvmtypes.QueryRequest{
		"port id":       {IBC: &wasmvmtypes.IBCQuery{PortID: &wasmvmtypes.PortIDQuery{}}},
		"list channels": {IBC: &wasmvmtypes.IBCQuery{ListChannels: &wasmvmtypes.ListChannelsQuery{}}},
		"channel":       {IBC: &wasmvmtypes.IBCQuery{Channel: &wasmvmtypes.ChannelQuery{ChannelID: "channel-0"}}},
	} {
		t.Run(name, func(t *testing.T) {
			query, err := json.Marshal(testdata.ReflectQueryMsg{Chain: &testdata.ChainQuery{Request: &req}})
			require.NoError(t, err)
			_, err = f.app.WasmKeeper.QuerySmart(f.ctx, contract, query)
			require.Error(t, err)
			text := strings.ToLower(err.Error())
			require.Contains(t, text, "unsupported")
			require.Contains(t, text, "ibc")
		})
	}
}

// A contract that ships IBC entry points still stores and instantiates, so
// code that merely carries them stays deployable. The port it is assigned is
// a label: the app builds no IBC router, so nothing can serve it.
func TestContractWithIBCEntryPointsStillInstantiates(t *testing.T) {
	f := newReflectFixture(t)
	codeID := f.store(t, testdata.IBCReflectContractWasm())
	init, err := json.Marshal(struct {
		ReflectCodeID uint64 `json:"reflect_code_id"`
	}{f.codeID})
	require.NoError(t, err)

	contract, _, err := f.pk.Instantiate(f.ctx, codeID, f.owner, nil, init, "islander", nil)
	require.NoError(t, err)
	require.Equal(t, wasmkeeper.PortIDForContract(contract), f.app.WasmKeeper.GetContractInfo(f.ctx, contract).IBCPortID)
}

// The VM is handed Wasmd's built-in capabilities without ibc2, so a contract
// requiring IBC v2 entry points is refused at upload by the VM's own check.
func TestContractCapabilitiesWithholdIBC2(t *testing.T) {
	capabilities := app.ContractCapabilities()
	require.NotContains(t, capabilities, "ibc2")
	require.Contains(t, wasmkeeper.BuiltInCapabilities(), "ibc2", "the case is a capability Wasmd ships")
	require.Len(t, capabilities, len(wasmkeeper.BuiltInCapabilities())-1)
	require.Subset(t, wasmkeeper.BuiltInCapabilities(), capabilities)
}
