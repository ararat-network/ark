package app_test

import (
	"testing"

	"github.com/cosmos/gogoproto/proto"
	gmp "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v11/modules/core/04-channel/v2/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

func TestGMPWiring(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	require.NotNil(t, arkApp.GMPKeeper)
	require.Contains(t, arkApp.ModuleManager.Modules, gmptypes.ModuleName)

	// GMP is a v2-only application and carries no middleware of its own.
	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(gmptypes.PortID))
	require.IsType(
		t,
		&gmp.IBCModule{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(gmptypes.PortID),
	)
	require.False(t, arkApp.IBCKeeper.PortKeeper.Router.HasRoute(gmptypes.ModuleName),
		"GMP has no Classic route")

	// It holds no module account: a derived account is an ordinary account, and
	// the module itself never takes custody.
	require.NotContains(t, arkApp.AccountKeeper.GetModulePermissions(), gmptypes.ModuleName)

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		gmptypes.ModuleName,
	)
}

// D48's substance: GMP and the contract runtime dispatch through one router, so
// a derived account is charged execution-generated tax on exactly the terms a
// contract is. This pins that they are the same construction rather than two
// that happen to agree today.
func TestGMPSharesTheExecutionPolicyRouter(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)

	router := arkApp.ExecutionPolicyRouter()
	require.Equal(t, arkApp.MsgServiceRouter(), router.Inner())
	require.Equal(t, arkApp.TreasuryKeeper, router.Treasury())
	require.Equal(t, arkApp.AppCodec(), router.Cdc())
}

type gmpFixture struct {
	path      *ibctesting.Path
	chainA    *ibctesting.TestChain
	chainB    *ibctesting.TestChain
	sender    string
	salt      []byte
	derived   sdk.AccAddress
	recipient sdk.AccAddress
}

// setupGMP opens a v2 client pair between two Ark chains and funds, on B,
// the account GMP will derive for A's sender.
func setupGMP(t *testing.T) gmpFixture {
	t.Helper()

	coord := apptestutil.NewIBCCoordinator(t, 2)
	chainA, chainB := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewPath(chainA, chainB)
	path.SetupV2()

	sender := chainA.SenderAccount.GetAddress().String()
	salt := []byte("ark")
	id := gmptypes.NewAccountIdentifier(path.EndpointB.ClientID, sender, salt)
	derived, err := gmptypes.BuildAddressPredictable(&id)
	require.NoError(t, err)

	enableStableTax(t, chainB)
	apptestutil.FundAccount(t, apptestutil.ArkChain(t, chainB), chainB.GetContext(), derived,
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))

	return gmpFixture{
		path: path, chainA: chainA, chainB: chainB, sender: sender, salt: salt, derived: derived,
		recipient: authtypes.NewModuleAddress("gmp-recipient"),
	}
}

// send relays a GMP packet carrying msgs from A to B and returns the packet.
func (f gmpFixture) send(t *testing.T, msgs ...proto.Message) channeltypesv2.Packet {
	t.Helper()

	cdc := apptestutil.ArkChain(t, f.chainB).AppCodec()
	payload, err := gmptypes.SerializeCosmosTx(cdc, msgs)
	require.NoError(t, err)
	data := gmptypes.NewGMPPacketData(f.sender, "", f.salt, payload, "")
	value, err := cdc.Marshal(&data)
	require.NoError(t, err)

	packet, err := f.path.EndpointA.MsgSendPacket(f.chainA.GetTimeoutTimestampSecs(), channeltypesv2.Payload{
		SourcePort:      gmptypes.PortID,
		DestinationPort: gmptypes.PortID,
		Version:         gmptypes.Version,
		Encoding:        gmptypes.EncodingProtobuf,
		Value:           value,
	})
	require.NoError(t, err)
	require.NoError(t, f.path.EndpointA.RelayPacket(packet))
	return packet
}

func (f gmpFixture) stable(t *testing.T, addr sdk.AccAddress) math.Int {
	t.Helper()
	return stableBalance(t, f.chainB, addr)
}

// TestGMPDerivedAccountExecutesAndPaysTax: a remote caller's message runs on
// the account Ark derives for it, over a real v2 client, and that account
// pays the execution-generated tax on the terms a contract does.
func TestGMPDerivedAccountExecutesAndPaysTax(t *testing.T) {
	f := setupGMP(t)

	f.send(t, &banktypes.MsgSend{
		FromAddress: f.derived.String(),
		ToAddress:   f.recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	})

	require.Equal(t, math.NewInt(1_000), f.stable(t, f.recipient))
	require.Equal(t, math.NewInt(100), f.stable(t, collectorAddress(t, f.chainB)))
	require.Equal(t, math.NewInt(8_900), f.stable(t, f.derived))
}

// TestGMPFailedDispatchMovesNothing: a message the derived account cannot
// afford fails the packet, and the failure acknowledgement carries no state.
func TestGMPFailedDispatchMovesNothing(t *testing.T) {
	f := setupGMP(t)

	f.send(t, &banktypes.MsgSend{
		FromAddress: f.derived.String(),
		ToAddress:   f.recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100_000)),
	})

	require.True(t, f.stable(t, f.recipient).IsZero())
	require.True(t, f.stable(t, collectorAddress(t, f.chainB)).IsZero(), "no tax without the transfer")
	require.Equal(t, math.NewInt(10_000), f.stable(t, f.derived))
}

// TestGMPRejectsAMessageSignedByAnotherAccount: the derived account is the
// only signer GMP authenticates; a payload naming any other signer fails.
func TestGMPRejectsAMessageSignedByAnotherAccount(t *testing.T) {
	f := setupGMP(t)
	other := f.chainB.SenderAccount.GetAddress()
	apptestutil.FundAccount(t, apptestutil.ArkChain(t, f.chainB), f.chainB.GetContext(), other,
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))

	f.send(t, &banktypes.MsgSend{
		FromAddress: other.String(),
		ToAddress:   f.recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	})

	require.True(t, f.stable(t, f.recipient).IsZero())
	require.Equal(t, math.NewInt(10_000), f.stable(t, other), "the other account is untouched")
}
