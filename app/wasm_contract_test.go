package app_test

import (
	"encoding/json"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/CosmWasm/wasmd/x/wasm/keeper/testdata"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvmtypes "github.com/CosmWasm/wasmvm/v3/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// reflectFixture is an app with the reflect contract's code stored, a ten
// percent transfer tax on the stablecoin, and helpers to run contracts. The
// reflect contract executes whatever its owner hands it, which makes it a
// programmable stand-in for any contract that moves funds.
type reflectFixture struct {
	app       *app.ArkApp
	ctx       sdk.Context
	pk        *wasmkeeper.PermissionedKeeper
	codeID    uint64
	owner     sdk.AccAddress
	recipient sdk.AccAddress
}

func newReflectFixture(t *testing.T) reflectFixture {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1, Time: time.Now()})
	setStableTax(t, arkApp, ctx, 10_000)
	f := reflectFixture{
		app: arkApp, ctx: ctx,
		pk:        wasmkeeper.NewDefaultPermissionKeeper(&arkApp.WasmKeeper),
		owner:     authtypes.NewModuleAddress("reflect-owner"),
		recipient: authtypes.NewModuleAddress("recipient"),
	}
	f.codeID = f.store(t, testdata.ReflectContractWasm())
	return f
}

// setStableTax puts a ten percent transfer tax on the stablecoin with the
// given per-input cap.
func setStableTax(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, capacity int64) {
	t.Helper()

	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	treasurytestutil.SetDerivedTaxCap(t, arkApp.TreasuryKeeper, ctx, chain.USDBaseDenom, math.NewInt(capacity))
}

func (f reflectFixture) store(t *testing.T, code []byte) uint64 {
	t.Helper()
	codeID, _, err := f.pk.Create(f.ctx, f.owner, code, nil)
	require.NoError(t, err)
	return codeID
}

// instantiate creates a reflect contract owned by owner and funds it with
// ten thousand stablecoin units.
func (f reflectFixture) instantiate(t *testing.T, owner sdk.AccAddress, label string) sdk.AccAddress {
	t.Helper()
	contract, _, err := f.pk.Instantiate(f.ctx, f.codeID, owner, nil, []byte("{}"), label, nil)
	require.NoError(t, err)
	f.fund(t, contract, 10_000)
	return contract
}

func (f reflectFixture) fund(t *testing.T, addr sdk.AccAddress, amount int64) {
	t.Helper()
	apptestutil.FundAccount(t, f.app, f.ctx, addr, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, amount)))
}

// execute has the contract dispatch msgs on its owner's instruction.
func (f reflectFixture) execute(t *testing.T, contract, owner sdk.AccAddress, msgs ...wasmvmtypes.CosmosMsg) error {
	t.Helper()
	return f.run(contract, owner, reflectBody(t, msgs...))
}

// executeSub has the contract dispatch submessages, whose replies it records.
func (f reflectFixture) executeSub(t *testing.T, contract, owner sdk.AccAddress, msgs ...wasmvmtypes.SubMsg) error {
	t.Helper()
	body, err := json.Marshal(testdata.ReflectHandleMsg{ReflectSubMsg: &testdata.ReflectSubPayload{Msgs: msgs}})
	require.NoError(t, err)
	return f.run(contract, owner, body)
}

// run executes the contract the way a transaction would: on a branch that
// is written only if the whole call succeeds.
func (f reflectFixture) run(contract, owner sdk.AccAddress, body []byte) error {
	cacheCtx, write := f.ctx.CacheContext()
	if _, err := f.pk.Execute(cacheCtx, contract, owner, body, nil); err != nil {
		return err
	}
	write()
	return nil
}

// chainQuery has the contract run a chain query and returns the raw answer.
func (f reflectFixture) chainQuery(t *testing.T, contract sdk.AccAddress, req wasmvmtypes.QueryRequest) []byte {
	t.Helper()
	query, err := json.Marshal(testdata.ReflectQueryMsg{Chain: &testdata.ChainQuery{Request: &req}})
	require.NoError(t, err)
	raw, err := f.app.WasmKeeper.QuerySmart(f.ctx, contract, query)
	require.NoError(t, err)
	var res testdata.ChainResponse
	require.NoError(t, json.Unmarshal(raw, &res))
	return res.Data
}

func (f reflectFixture) stable(addr sdk.AccAddress) math.Int {
	return f.app.BankKeeper.GetBalance(f.ctx, addr, chain.USDBaseDenom).Amount
}

func (f reflectFixture) collector() sdk.AccAddress {
	return f.app.AccountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName)
}

func stableCoins(amount int64) wasmvmtypes.Array[wasmvmtypes.Coin] {
	return wasmvmtypes.Array[wasmvmtypes.Coin]{{Denom: chain.USDBaseDenom, Amount: math.NewInt(amount).String()}}
}

func bankSend(to sdk.AccAddress, amount int64) wasmvmtypes.CosmosMsg {
	return wasmvmtypes.CosmosMsg{Bank: &wasmvmtypes.BankMsg{Send: &wasmvmtypes.SendMsg{
		ToAddress: to.String(), Amount: stableCoins(amount),
	}}}
}

// executeReflect is the message that has a reflect contract dispatch msgs,
// as one contract sends to another.
func executeReflect(contract sdk.AccAddress, funds wasmvmtypes.Array[wasmvmtypes.Coin], msgs ...wasmvmtypes.CosmosMsg) wasmvmtypes.CosmosMsg {
	body, err := json.Marshal(testdata.ReflectHandleMsg{Reflect: &testdata.ReflectPayload{Msgs: msgs}})
	if err != nil {
		panic(err)
	}
	return wasmvmtypes.CosmosMsg{Wasm: &wasmvmtypes.WasmMsg{Execute: &wasmvmtypes.ExecuteMsg{
		ContractAddr: contract.String(), Msg: body, Funds: funds,
	}}}
}

// keepOwner is the reflect contract's nearest thing to doing nothing: it
// refuses an empty message list, so a call that only carries funds asks it
// to keep the owner it has.
func keepOwner(owner sdk.AccAddress) []byte {
	body, err := json.Marshal(testdata.ReflectHandleMsg{ChangeOwner: &testdata.OwnerPayload{Owner: owner}})
	if err != nil {
		panic(err)
	}
	return body
}

// TestContractBankSendPaysExecutionTax: a contract-generated send pays the
// transfer tax from the contract, on top of the principal, through the
// router Wasmd dispatches into.
func TestContractBankSendPaysExecutionTax(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "sender")

	require.NoError(t, f.execute(t, contract, f.owner, bankSend(f.recipient, 1_000)))

	require.Equal(t, math.NewInt(1_000), f.stable(f.recipient))
	require.Equal(t, math.NewInt(100), f.stable(f.collector()))
	require.Equal(t, math.NewInt(8_900), f.stable(contract))
}

// TestContractToContractFundsAreTaxedAtExecution: funds one contract attaches
// when executing another are execution-generated principal, taxed from the
// sending contract.
func TestContractToContractFundsAreTaxedAtExecution(t *testing.T) {
	f := newReflectFixture(t)
	parent := f.instantiate(t, f.owner, "parent")
	child := f.instantiate(t, parent, "child")

	require.NoError(t, f.execute(t, parent, f.owner, wasmvmtypes.CosmosMsg{Wasm: &wasmvmtypes.WasmMsg{Execute: &wasmvmtypes.ExecuteMsg{
		ContractAddr: child.String(), Msg: keepOwner(parent), Funds: stableCoins(1_000),
	}}}))

	require.Equal(t, math.NewInt(11_000), f.stable(child))
	require.Equal(t, math.NewInt(100), f.stable(f.collector()))
	require.Equal(t, math.NewInt(8_900), f.stable(parent))
}

// TestContractInstantiateFundsAreTaxedAtExecution: the same for funds a
// contract attaches when instantiating one.
func TestContractInstantiateFundsAreTaxedAtExecution(t *testing.T) {
	f := newReflectFixture(t)
	parent := f.instantiate(t, f.owner, "parent")

	require.NoError(t, f.execute(t, parent, f.owner, wasmvmtypes.CosmosMsg{Wasm: &wasmvmtypes.WasmMsg{
		Instantiate: &wasmvmtypes.InstantiateMsg{CodeID: f.codeID, Msg: []byte("{}"), Funds: stableCoins(1_000), Label: "child"},
	}}))

	require.Equal(t, math.NewInt(100), f.stable(f.collector()))
	require.Equal(t, math.NewInt(8_900), f.stable(parent))
}

// TestNestedContractsReceiveIndependentCaps: with the per-input cap below
// the rate's figure, a parent's own send and the send of the child it
// executes through a submessage are two inputs, each capped on its own.
func TestNestedContractsReceiveIndependentCaps(t *testing.T) {
	f := newReflectFixture(t)
	setStableTax(t, f.app, f.ctx, 50)
	parent := f.instantiate(t, f.owner, "parent")
	child := f.instantiate(t, parent, "child")

	require.NoError(t, f.executeSub(t, parent, f.owner,
		wasmvmtypes.SubMsg{ID: 1, Msg: bankSend(f.recipient, 1_000), ReplyOn: wasmvmtypes.ReplyAlways},
		wasmvmtypes.SubMsg{ID: 2, Msg: executeReflect(child, nil, bankSend(f.recipient, 1_000)), ReplyOn: wasmvmtypes.ReplyAlways},
	))

	require.Equal(t, math.NewInt(2_000), f.stable(f.recipient))
	require.Equal(t, math.NewInt(100), f.stable(f.collector()), "two inputs, each at the cap")
	require.Equal(t, math.NewInt(8_950), f.stable(parent))
	require.Equal(t, math.NewInt(8_950), f.stable(child))
}

// TestTwoContractsReceiveIndependentCaps: two unrelated contracts sending
// the same denomination are two inputs too.
func TestTwoContractsReceiveIndependentCaps(t *testing.T) {
	f := newReflectFixture(t)
	setStableTax(t, f.app, f.ctx, 50)
	first := f.instantiate(t, f.owner, "first")
	second := f.instantiate(t, f.owner, "second")

	require.NoError(t, f.execute(t, first, f.owner, bankSend(f.recipient, 1_000)))
	require.NoError(t, f.execute(t, second, f.owner, bankSend(f.recipient, 1_000)))

	require.Equal(t, math.NewInt(100), f.stable(f.collector()))
	require.Equal(t, math.NewInt(8_950), f.stable(first))
	require.Equal(t, math.NewInt(8_950), f.stable(second))
}

// TestCaughtSubmessageFailureRollsBackTheSendAndItsTax: a submessage the
// contract catches in reply fails alone; its tax goes with it and the outer
// call succeeds.
func TestCaughtSubmessageFailureRollsBackTheSendAndItsTax(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "catcher")

	require.NoError(t, f.executeSub(t, contract, f.owner,
		wasmvmtypes.SubMsg{ID: 7, Msg: bankSend(f.recipient, 100_000), ReplyOn: wasmvmtypes.ReplyError},
	))

	require.True(t, f.stable(f.recipient).IsZero())
	require.True(t, f.stable(f.collector()).IsZero(), "no tax without the transfer")
	require.Equal(t, math.NewInt(10_000), f.stable(contract))
	query, err := json.Marshal(testdata.ReflectQueryMsg{SubMsgResult: &testdata.SubCall{ID: 7}})
	require.NoError(t, err)
	reply, err := f.app.WasmKeeper.QuerySmart(f.ctx, contract, query)
	require.NoError(t, err)
	require.Contains(t, string(reply), "error", "the contract saw the failure in its reply")
}

// TestUncaughtSubmessageFailureFailsTheWholeCall: without a reply to catch
// it, one failing message fails the call, and an earlier send in the same
// call is rolled back with its tax.
func TestUncaughtSubmessageFailureFailsTheWholeCall(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "sender")

	require.Error(t, f.execute(t, contract, f.owner, bankSend(f.recipient, 1_000), bankSend(f.recipient, 100_000)))

	require.True(t, f.stable(f.recipient).IsZero())
	require.True(t, f.stable(f.collector()).IsZero())
	require.Equal(t, math.NewInt(10_000), f.stable(contract))
}

// TestInsufficientBalanceForPrincipalAndTaxFailsAtomically: a contract that
// can pay the principal but not the tax on top of it moves nothing.
func TestInsufficientBalanceForPrincipalAndTaxFailsAtomically(t *testing.T) {
	f := newReflectFixture(t)
	contract, _, err := f.pk.Instantiate(f.ctx, f.codeID, f.owner, nil, []byte("{}"), "short", nil)
	require.NoError(t, err)
	f.fund(t, contract, 1_050)

	require.Error(t, f.execute(t, contract, f.owner, bankSend(f.recipient, 1_000)))

	require.True(t, f.stable(f.recipient).IsZero())
	require.True(t, f.stable(f.collector()).IsZero())
	require.Equal(t, math.NewInt(1_050), f.stable(contract))
}

// TestContractCannotCreditAFundWithStablecoin: a contract's send into a
// Treasury fund finishes through the restricted Bank path like any other,
// so a non-NOAH credit fails atomically.
func TestContractCannotCreditAFundWithStablecoin(t *testing.T) {
	f := newReflectFixture(t)
	contract := f.instantiate(t, f.owner, "depositor")
	buffer := f.app.AccountKeeper.GetModuleAddress(treasurytypes.RedemptionBufferName)

	require.Error(t, f.execute(t, contract, f.owner, bankSend(buffer, 1_000)))

	require.True(t, f.stable(buffer).IsZero())
	require.True(t, f.stable(f.collector()).IsZero())
	require.Equal(t, math.NewInt(10_000), f.stable(contract))
}

// signedContractFixture is a single Ark chain whose sender owns a reflect
// contract, for the shapes only a signed transaction can take.
type signedContractFixture struct {
	chain    *ibctesting.TestChain
	app      *app.ArkApp
	sender   sdk.AccAddress
	codeID   uint64
	contract sdk.AccAddress
}

func newSignedContractFixture(t *testing.T, coord *ibctesting.Coordinator, c *ibctesting.TestChain) signedContractFixture {
	t.Helper()

	arkApp := apptestutil.ArkChain(t, c)
	ctx := c.GetContext()
	sender := c.SenderAccount.GetAddress()
	setStableTax(t, arkApp, ctx, 10_000)
	apptestutil.FundAccount(t, arkApp, ctx, sender, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))

	pk := wasmkeeper.NewDefaultPermissionKeeper(&arkApp.WasmKeeper)
	codeID, _, err := pk.Create(ctx, sender, testdata.ReflectContractWasm(), nil)
	require.NoError(t, err)
	contract, _, err := pk.Instantiate(ctx, codeID, sender, nil, []byte("{}"), "owned", nil)
	require.NoError(t, err)
	apptestutil.FundAccount(t, arkApp, ctx, contract, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)))
	coord.CommitBlock(c)

	return signedContractFixture{chain: c, app: arkApp, sender: sender, codeID: codeID, contract: contract}
}

func (f signedContractFixture) stable(t *testing.T, addr sdk.AccAddress) math.Int {
	t.Helper()
	return stableBalance(t, f.chain, addr)
}

func reflectBody(t *testing.T, msgs ...wasmvmtypes.CosmosMsg) []byte {
	t.Helper()
	body, err := json.Marshal(testdata.ReflectHandleMsg{Reflect: &testdata.ReflectPayload{Msgs: msgs}})
	require.NoError(t, err)
	return body
}

// TestAttachedFundsAreTaxedByTheAnteOnce: funds a signed execute attaches
// are ante-visible principal, taxed from the signer and never again.
func TestAttachedFundsAreTaxedByTheAnteOnce(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 1)
	f := newSignedContractFixture(t, coord, coord.GetChain(ibctesting.GetChainID(1)))

	_, err := f.chain.SendMsgs(&wasmtypes.MsgExecuteContract{
		Sender: f.sender.String(), Contract: f.contract.String(), Msg: keepOwner(f.sender),
		Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	})
	require.NoError(t, err)

	require.Equal(t, math.NewInt(11_000), f.stable(t, f.contract))
	require.Equal(t, math.NewInt(100), f.stable(t, collectorAddress(t, f.chain)))
	require.Equal(t, math.NewInt(8_900), f.stable(t, f.sender))
}

// TestInstantiateFundsAreTaxedByTheAnteOnce: the same for a signed
// instantiate.
func TestInstantiateFundsAreTaxedByTheAnteOnce(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 1)
	f := newSignedContractFixture(t, coord, coord.GetChain(ibctesting.GetChainID(1)))

	_, err := f.chain.SendMsgs(&wasmtypes.MsgInstantiateContract{
		Sender: f.sender.String(), CodeID: f.codeID, Label: "funded", Msg: []byte("{}"),
		Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	})
	require.NoError(t, err)

	require.Equal(t, math.NewInt(100), f.stable(t, collectorAddress(t, f.chain)))
	require.Equal(t, math.NewInt(8_900), f.stable(t, f.sender))
}

// TestContractIBCTransferIsTaxedAtExecution: an ICS-20 transfer a contract
// dispatches is not ante-visible, so the router taxes it from the contract at
// execution, and the packet then crosses like any other.
func TestContractIBCTransferIsTaxedAtExecution(t *testing.T) {
	coord := apptestutil.NewIBCCoordinator(t, 2)
	chainA, chainB := coord.GetChain(ibctesting.GetChainID(1)), coord.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewTransferPath(chainA, chainB)
	path.Setup()
	f := newSignedContractFixture(t, coord, chainA)
	receiver := chainB.SenderAccount.GetAddress()

	res, err := f.chain.SendMsgs(&wasmtypes.MsgExecuteContract{
		Sender: f.sender.String(), Contract: f.contract.String(),
		Msg: reflectBody(t, wasmvmtypes.CosmosMsg{IBC: &wasmvmtypes.IBCMsg{Transfer: &wasmvmtypes.TransferMsg{
			ChannelID: path.EndpointA.ChannelID,
			ToAddress: receiver.String(),
			Amount:    wasmvmtypes.Coin{Denom: chain.USDBaseDenom, Amount: "1000"},
			Timeout:   wasmvmtypes.IBCTimeout{Timestamp: chainA.GetTimeoutTimestamp()},
		}}}),
	})
	require.NoError(t, err)

	escrow := ibctransfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	require.Equal(t, math.NewInt(1_000), f.stable(t, escrow))
	require.Equal(t, math.NewInt(100), f.stable(t, collectorAddress(t, f.chain)), "taxed once, from the contract")
	require.Equal(t, math.NewInt(8_900), f.stable(t, f.contract))
	require.Equal(t, math.NewInt(10_000), f.stable(t, f.sender), "the signer attached nothing and paid nothing")

	packet, err := ibctesting.ParseV1PacketFromEvents(res.Events)
	require.NoError(t, err)
	require.NoError(t, path.RelayPacket(packet))
	voucher := ibctransfertypes.NewDenom(chain.USDBaseDenom,
		ibctransfertypes.NewHop(path.EndpointB.ChannelConfig.PortID, path.EndpointB.ChannelID)).IBCDenom()
	require.Equal(t, math.NewInt(1_000), balance(t, chainB, receiver, voucher))
}
