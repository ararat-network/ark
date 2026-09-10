package app_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/cosmos/gogoproto/proto"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v11/modules/core/04-channel/v2/types"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// The fixtures below twin the ones in app/ante's tests: the router seam needs
// the unexported executionPolicyRouter, so its tests live here while the
// policy's own tests moved with the policy.

// fundVoter moves bond-denom coins from the genesis delegator — the one
// funded account on a Setup chain, reached through the validator's delegation
// record since Setup does not export it — to a fresh voter.
func fundVoter(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, to sdk.AccAddress, amount math.Int) {
	t.Helper()
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)
	delegations, err := arkApp.StakingKeeper.GetValidatorDelegations(ctx, valAddr)
	require.NoError(t, err)
	require.NotEmpty(t, delegations)

	coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, amount))
	for _, delegation := range delegations {
		delegator, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		require.NoError(t, err)
		if arkApp.BankKeeper.GetBalance(ctx, delegator, sdk.DefaultBondDenom).Amount.GTE(amount) {
			require.NoError(t, arkApp.BankKeeper.SendCoins(ctx, delegator, to, coins))
			return
		}
	}
	t.Fatalf("no delegator holds %s to fund the voter", coins)
}

// setupRouterSeamTest returns a chain with two voters: one delegated past the
// stake floor, and one with no stake at all.
func setupRouterSeamTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress, sdk.AccAddress) {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)

	rich := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	bond := chain.NativeBaseAmount(2)
	fundVoter(t, arkApp, ctx, rich, bond)
	_, err = arkApp.StakingKeeper.Delegate(
		ctx, rich, bond, stakingtypes.Unbonded, validators[0], true,
	)
	require.NoError(t, err)

	poor := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	return arkApp, ctx, rich, poor
}

func vote(voter sdk.AccAddress) *govv1.MsgVote {
	return govv1.NewMsgVote(voter, 1, govv1.OptionYes, "")
}

// guardAddr derives a distinct valid address per output without generating a
// key per recipient.
func guardAddr(i int) sdk.AccAddress {
	a := make([]byte, 20)
	binary.BigEndian.PutUint32(a, uint32(i))
	return sdk.AccAddress(a)
}

// multiSend builds a well-formed MsgMultiSend fanning out one base unit to
// each of n recipients.
func multiSend(from sdk.AccAddress, n int) *banktypes.MsgMultiSend {
	outputs := make([]banktypes.Output, n)
	for i := range outputs {
		outputs[i] = banktypes.Output{
			Address: guardAddr(i).String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
		}
	}
	return &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: from.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(n))),
		}},
		Outputs: outputs,
	}
}

// TestTreasuryRouterEnforcesVoteStakeFloor pins the second seam: a vote
// dispatched by a contract, derived account, or interchain account meets the
// same floor before any tax or execution.
func TestTreasuryRouterEnforcesVoteStakeFloor(t *testing.T) {
	arkApp, ctx, rich, poor := setupRouterSeamTest(t)
	router := arkApp.ExecutionPolicyRouter()

	poorVote := vote(poor)
	handler := router.Handler(poorVote)
	require.NotNil(t, handler)
	_, err := handler(ctx, poorVote)
	require.ErrorContains(t, err, "voting requires at least")

	// The staked voter clears the floor; the error moves on to the vote
	// itself, which targets a proposal that does not exist.
	richVote := vote(rich)
	_, err = router.Handler(richVote)(ctx, richVote)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "voting requires at least")
}

// TestTreasuryRouterEnforcesMultiSendGuard pins the second seam: a MultiSend
// dispatched by a contract, derived account, or interchain account meets the
// same cap before any tax or execution.
func TestTreasuryRouterEnforcesMultiSendGuard(t *testing.T) {
	arkApp, ctx, _, _ := setupRouterSeamTest(t)
	router := arkApp.ExecutionPolicyRouter()
	from := guardAddr(1 << 16)

	// One past the ante package's maxMultiSendOutputs cap of 500.
	over := multiSend(from, 501)
	handler := router.Handler(over)
	require.NotNil(t, handler)
	_, err := handler(ctx, over)
	require.ErrorContains(t, err, "too many MultiSend outputs")

	// Within the cap the guard clears; whatever fails next — tax collection
	// or the unfunded send itself — is not the guard's refusal.
	within := multiSend(from, 2)
	_, err = router.Handler(within)(ctx, within)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "too many MultiSend outputs")
}

// stubRouter stands in for BaseApp's router so the test observes exactly what
// the wrapper does around a dispatch, without needing a real contract.
type stubRouter struct {
	called bool
	err    error
	handle func(ctx sdk.Context, req sdk.Msg)
}

func (s *stubRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return func(ctx sdk.Context, req sdk.Msg) (*sdk.Result, error) {
		s.called = true
		if s.handle != nil {
			s.handle(ctx, req)
		}
		if s.err != nil {
			return nil, s.err
		}
		return &sdk.Result{}, nil
	}
}

// A contract pays transfer tax on what it dispatches, on top of the principal
// that message moves (D41), and the tax lands in the collector.
func TestExecutionGeneratedTaxChargesTheSendingContract(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)

	inner := &stubRouter{}
	router := app.NewExecutionPolicyRouter(inner, arkApp.TreasuryKeeper, arkApp.BankKeeper, nil, arkApp.AppCodec())

	msg := &banktypes.MsgSend{
		FromAddress: contract.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}

	_, err := router.Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.True(t, inner.called, "the underlying handler must still run")

	// Ten percent of the dispatched principal, taken from the contract and
	// nowhere else.
	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	require.Equal(t,
		math.NewInt(100),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t,
		math.NewInt(9_900),
		arkApp.BankKeeper.GetBalance(ctx, contract, chain.USDBaseDenom).Amount,
		"the contract pays tax in addition to the principal it is about to move")
}

// Wasmd copies a contract's chosen source port into the v2 send message it
// dispatches, so a contract shipping stablecoins through the raw v2 path pays
// the same tax as one dispatching MsgTransfer.
func TestExecutionGeneratedTaxChargesTheContractForV2SendPacket(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)

	inner := &stubRouter{}
	router := app.NewExecutionPolicyRouter(inner, arkApp.TreasuryKeeper, arkApp.BankKeeper, nil, arkApp.AppCodec())

	data := ibctransfertypes.NewFungibleTokenPacketData(
		chain.USDBaseDenom, "1000", contract.String(), "receiver", "")
	msg := &channeltypesv2.MsgSendPacket{
		SourceClient:     "07-tendermint-0",
		TimeoutTimestamp: 1,
		Payloads: []channeltypesv2.Payload{{
			SourcePort:      ibctransfertypes.PortID,
			DestinationPort: ibctransfertypes.PortID,
			Version:         ibctransfertypes.V1,
			Encoding:        ibctransfertypes.EncodingJSON,
			Value:           data.GetBytes(),
		}},
		Signer: contract.String(),
	}

	_, err := router.Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.True(t, inner.called, "the underlying handler must still run")

	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	require.Equal(t,
		math.NewInt(100),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t,
		math.NewInt(9_900),
		arkApp.BankKeeper.GetBalance(ctx, contract, chain.USDBaseDenom).Amount,
		"the contract pays tax in addition to the principal the packet escrows")
}

// A failure below the wrapper takes the tax with it. Wasmd dispatches inside a
// cached store, so returning the error is what makes both unwind (D42) — this
// pins that the wrapper propagates rather than swallowing.
func TestExecutionGeneratedTaxFailsWithTheDispatch(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)

	sentinel := errors.New("contract execution failed")
	inner := &stubRouter{err: sentinel}
	router := app.NewExecutionPolicyRouter(inner, arkApp.TreasuryKeeper, arkApp.BankKeeper, nil, arkApp.AppCodec())

	msg := &banktypes.MsgSend{
		FromAddress: contract.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}

	_, err := router.Handler(msg)(ctx, msg)
	require.ErrorIs(t, err, sentinel)
}

// An untaxable dispatch costs the contract nothing and still runs.
func TestExecutionGeneratedTaxSkipsUntaxableMessages(t *testing.T) {
	arkApp, ctx, contract := setupExecutionTaxFixture(t)

	inner := &stubRouter{}
	router := app.NewExecutionPolicyRouter(inner, arkApp.TreasuryKeeper, arkApp.BankKeeper, nil, arkApp.AppCodec())

	// No cap is held for anoah, so it is outside the tax base.
	msg := &banktypes.MsgSend{
		FromAddress: contract.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000)),
	}

	_, err := router.Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.True(t, inner.called)

	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	require.True(t, arkApp.BankKeeper.GetAllBalances(ctx, collector).IsZero())
}

// An unroutable message must produce Wasmd's own error, not one about tax, so
// the wrapper returns a nil handler exactly as the router beneath it would.
func TestExecutionGeneratedTaxPassesThroughUnroutableMessages(t *testing.T) {
	arkApp, _, _ := setupExecutionTaxFixture(t)

	router := app.NewExecutionPolicyRouter(nilRouter{}, arkApp.TreasuryKeeper, arkApp.BankKeeper, nil, arkApp.AppCodec())
	require.Nil(t, router.Handler(&banktypes.MsgSend{}))
}

type nilRouter struct{}

func (nilRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return nil }

// taxedStandIn sets a ten percent transfer tax with a cap on the taxed
// denomination, then funds a named account to spend under it. Each caller names
// its own stand-in and funds it for the remainder that caller asserts.
func taxedStandIn(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, name string, funded int64) sdk.AccAddress {
	t.Helper()

	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	treasurytestutil.SetDerivedTaxCap(t, arkApp.TreasuryKeeper, ctx, chain.USDBaseDenom, math.NewInt(10_000))

	account := authtypes.NewModuleAddress(name)
	funds := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, funded))
	apptestutil.FundAccount(t, arkApp, ctx, account, funds)

	return account
}

// setupExecutionTaxFixture returns an app with a ten percent tax rate, a cap on
// the taxed denomination, and a funded stand-in for a contract account.
func setupExecutionTaxFixture(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress) {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})

	return arkApp, ctx, taxedStandIn(t, arkApp, ctx, "contract-stand-in", 10_000)
}

// The ICA host dispatches through the policy router, so an interchain
// account pays transfer tax exactly as a contract does. The launch allowlist
// is empty, but the tax must not depend on it staying that way: this drives a
// packet through the host under allow-all params and watches the send pay.
func TestInterchainAccountPaysExecutionTax(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1})

	ica := taxedStandIn(t, arkApp, ctx, "interchain-account-stand-in", 2_000)
	arkApp.ICAHostKeeper.SetParams(ctx, icahosttypes.NewParams(true, []string{"*"}))

	const (
		controllerPort = "icacontroller-owner"
		hostChannel    = "channel-7"
		connectionID   = "connection-0"
	)
	recipient := authtypes.NewModuleAddress("ica-recipient")

	// The state an ICS-27 handshake would have left behind: an open channel
	// whose version carries the account metadata, and the registered account.
	metadata := icatypes.NewMetadata(
		icatypes.Version,
		connectionID,
		connectionID,
		ica.String(),
		icatypes.EncodingProtobuf,
		icatypes.TxTypeSDKMultiMsg,
	)
	arkApp.IBCKeeper.ChannelKeeper.SetChannel(ctx, icatypes.HostPortID, hostChannel, channeltypes.Channel{
		State:          channeltypes.OPEN,
		Ordering:       channeltypes.ORDERED,
		Counterparty:   channeltypes.NewCounterparty(controllerPort, "channel-1"),
		ConnectionHops: []string{connectionID},
		Version:        string(icatypes.ModuleCdc.MustMarshalJSON(&metadata)),
	})
	arkApp.ICAHostKeeper.SetInterchainAccountAddress(ctx, connectionID, controllerPort, ica.String())

	send := &banktypes.MsgSend{
		FromAddress: ica.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}
	data, err := icatypes.SerializeCosmosTx(arkApp.AppCodec(), []proto.Message{send}, icatypes.EncodingProtobuf)
	require.NoError(t, err)
	packetData := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: data}

	_, err = arkApp.ICAHostKeeper.OnRecvPacket(ctx, channeltypes.Packet{
		Sequence:           1,
		SourcePort:         controllerPort,
		SourceChannel:      "channel-1",
		DestinationPort:    icatypes.HostPortID,
		DestinationChannel: hostChannel,
		Data:               packetData.GetBytes(),
	})
	require.NoError(t, err)

	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	require.Equal(t, math.NewInt(1_000), arkApp.BankKeeper.GetBalance(ctx, recipient, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(100), arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(900), arkApp.BankKeeper.GetBalance(ctx, ica, chain.USDBaseDenom).Amount)
}

// A derived account pays tax from its own balance, on top of the principal it
// moves — the same charge a contract takes, through the same wrapper. GMP has
// already refused any message not signed by the derived account before this
// point, so the signer the router bills is that account by construction.
func TestGMPDerivedAccountPaysExecutionTax(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})

	// Stands in for the account GMP derives for a remote caller: an ordinary
	// account holding an ordinary balance.
	derived := taxedStandIn(t, arkApp, ctx, "gmp-derived-stand-in", 10_000)

	inner := &stubRouter{}
	router := arkApp.ExecutionPolicyRouter().WithInner(inner)

	msg := &banktypes.MsgSend{
		FromAddress: derived.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}
	_, err := router.Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.True(t, inner.called)

	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
	require.Equal(t, math.NewInt(100), arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(9_900), arkApp.BankKeeper.GetBalance(ctx, derived, chain.USDBaseDenom).Amount)
}
