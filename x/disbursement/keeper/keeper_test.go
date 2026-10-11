package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/testutil"
	"github.com/ararat-network/ark/x/disbursement/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// KeeperTestSuite drives the keeper over an in-memory store with mocked neighbours. Bank is a
// balance map the mocks read and move, so a payment or a buy shows in custody as it would on chain.
type KeeperTestSuite struct {
	suite.Suite

	ctx         context.Context
	cdc         codec.Codec
	keeper      *keeper.Keeper
	msgServer   types.MsgServer
	queryServer types.QueryServer
	authority   string
	// committee and registrar are the appointed accounts; term and registrarTerm track their live terms.
	committee, registrar string
	term, registrarTerm  uint64

	accountKeeper      *testutil.MockAccountKeeper
	wasmKeeper         *testutil.MockWasmKeeper
	bankKeeper         *testutil.MockBankKeeper
	distributionKeeper *testutil.MockDistributionKeeper
	marketKeeper       *testutil.MockMarketKeeper
	assetKeeper        *testutil.MockAssetKeeper
	oracleKeeper       *testutil.MockOracleKeeper

	// balances is what Bank reports for each custody account, by module name; received is what each
	// payee has been paid, by address; communityPool collects what returns through Distribution.
	balances      map[string]sdk.Coins
	received      map[string]sdk.Coins
	communityPool sdk.Coins
	// blocked are the recipients Bank blocks; refused the recipients a send restriction refuses.
	blocked map[string]bool
	refused map[string]error
	// assets is the registry; reference and rates are the oracle's view, every rate NOAH per unit.
	assets    map[string]assettypes.Asset
	reference string
	rates     map[string]math.LegacyDec
	// premium is the NOAH a buy costs past its ask's face value, the spread a steep pool charges.
	premium int64
	// moduleAccountsMissing makes the account keeper report no module account, which import refuses.
	moduleAccountsMissing bool
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	s.cdc = codec.NewProtoCodec(interfaceRegistry)
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	s.committee, s.registrar = testAddress(1), testAddress(2)
	s.term, s.registrarTerm = 0, 0

	s.balances = map[string]sdk.Coins{}
	s.received = map[string]sdk.Coins{}
	s.communityPool = sdk.NewCoins()
	s.blocked = map[string]bool{}
	s.refused = map[string]error{}
	s.assets = map[string]assettypes.Asset{"ausd": {Denom: "ausd", Status: assettypes.AssetStatus_ASSET_STATUS_ACTIVE}}
	s.reference = chain.XDRBaseDenom
	s.rates = map[string]math.LegacyDec{chain.XDRBaseDenom: math.LegacyOneDec(), "ausd": math.LegacyOneDec()}
	s.premium = 0
	s.moduleAccountsMissing = false

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as the shape it observed.
	s.accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.accountKeeper.EXPECT().GetModuleAddress(gomock.Any()).DoAndReturn(authtypes.NewModuleAddress).AnyTimes()
	s.accountKeeper.EXPECT().GetModuleAccount(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, name string) sdk.ModuleAccountI {
		if s.moduleAccountsMissing {
			return nil
		}
		return authtypes.NewEmptyModuleAccount(name)
	}).AnyTimes()
	s.wasmKeeper = testutil.NewMockWasmKeeper(ctrl)
	s.wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()

	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.bankKeeper.EXPECT().BlockedAddr(gomock.Any()).DoAndReturn(func(a sdk.AccAddress) bool { return s.blocked[a.String()] }).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a sdk.AccAddress, denom string) sdk.Coin {
		return sdk.NewCoin(denom, s.custody(a).AmountOf(denom))
	}).AnyTimes()
	s.bankKeeper.EXPECT().GetAllBalances(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a sdk.AccAddress) sdk.Coins {
		return s.custody(a)
	}).AnyTimes()
	// A send checks the blocked list, then the restrictions, then the balance, and moves the coins.
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, module string, to sdk.AccAddress, coins sdk.Coins) error {
		if s.blocked[to.String()] {
			return fmt.Errorf("%s is not allowed to receive funds", to)
		}
		if err := s.refused[to.String()]; err != nil {
			return err
		}
		rest, negative := s.balances[module].SafeSub(coins...)
		if negative {
			return fmt.Errorf("%s holds %s, short of %s", module, s.balances[module], coins)
		}
		s.balances[module] = rest
		s.received[to.String()] = s.received[to.String()].Add(coins...)
		return nil
	}).AnyTimes()

	s.distributionKeeper = testutil.NewMockDistributionKeeper(ctrl)
	s.distributionKeeper.EXPECT().FundCommunityPool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, coins sdk.Coins, from sdk.AccAddress) error {
		name, ok := s.poolName(from)
		if !ok {
			return fmt.Errorf("%s holds no custody", from)
		}
		rest, negative := s.balances[name].SafeSub(coins...)
		if negative {
			return fmt.Errorf("%s holds %s, short of %s", name, s.balances[name], coins)
		}
		s.balances[name] = rest
		s.communityPool = s.communityPool.Add(coins...)
		return nil
	}).AnyTimes()

	// Market fills one to one, as the fixture rates price every denomination, plus the premium: the
	// offer is the ask's amount, taken from the contributor account as the ask lands in it.
	s.marketKeeper = testutil.NewMockMarketKeeper(ctrl)
	s.marketKeeper.EXPECT().Buy(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, trader sdk.AccAddress, ask sdk.Coin, offerDenom string, maximumOffer sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error) {
		offer := sdk.NewCoin(offerDenom, ask.Amount.AddRaw(s.premium))
		if offer.Amount.GT(maximumOffer.Amount) {
			return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrapf(markettypes.ErrMaximumOfferExceeded, "maximum %s, needed %s", maximumOffer, offer)
		}
		name, ok := s.poolName(trader)
		if !ok {
			return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, fmt.Errorf("%s holds no custody", trader)
		}
		rest, negative := s.balances[name].SafeSub(offer)
		if negative {
			return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, fmt.Errorf("%s holds %s, short of %s", name, s.balances[name], offer)
		}
		s.balances[name] = rest.Add(ask)
		return offer, ask, sdk.DecCoin{}, nil
	}).AnyTimes()

	s.assetKeeper = testutil.NewMockAssetKeeper(ctrl)
	s.assetKeeper.EXPECT().GetAsset(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, denom string) (assettypes.Asset, error) {
		asset, ok := s.assets[denom]
		if !ok {
			return assettypes.Asset{}, fmt.Errorf("asset %s not found", denom)
		}
		return asset, nil
	}).AnyTimes()
	s.assetKeeper.EXPECT().HasAsset(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, denom string) (bool, error) {
		_, ok := s.assets[denom]
		return ok, nil
	}).AnyTimes()

	s.oracleKeeper = testutil.NewMockOracleKeeper(ctrl)
	s.oracleKeeper.EXPECT().GetReferenceDenom(gomock.Any()).DoAndReturn(func(context.Context) (string, error) { return s.reference, nil }).AnyTimes()
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
		rates := oracletypes.NewRateSet()
		rates[chain.NoahBaseDenom] = math.LegacyOneDec()
		for _, denom := range denoms {
			if denom == chain.NoahBaseDenom {
				continue
			}
			rate, ok := s.rates[denom]
			if !ok {
				return nil, fmt.Errorf("no fresh rate for %s", denom)
			}
			rates[denom] = rate
		}
		return rates, nil
	}).AnyTimes()

	s.keeper, s.ctx = s.newKeeper()
	gs := types.DefaultGenesisState()
	gs.Params = testParams()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, gs))
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
	s.queryServer = keeper.NewQueryServerImpl(s.keeper)
}

// newKeeper builds a keeper on a fresh store over the suite's mocks, at height 10 and a fixed
// block time; a second one stands in for a relaunched chain.
func (s *KeeperTestSuite) newKeeper() (*keeper.Keeper, context.Context) {
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_800_000_000, 0))
	k := keeper.NewKeeper(
		s.cdc,
		runtime.NewKVStoreService(key),
		s.authority,
		s.accountKeeper,
		s.wasmKeeper,
		s.bankKeeper,
		s.distributionKeeper,
		s.marketKeeper,
		s.assetKeeper,
		s.oracleKeeper,
	)
	return k, ctx
}

// hold is every first period in testParams: the two-week floor nothing pays before (D126).
const hold = types.MinFirstPeriod

// testParams keeps every first period at the floor and shortens the rest, so a few seconds past the
// hold cross a boundary: members a tenth then three ten-second periods, ownership a quarter then the
// rest ten seconds on, compensation in periods of the floor.
func testParams() types.Params {
	p := types.DefaultParams()
	p.MemberAmount = math.NewInt(10_000)
	p.MemberSchedule = types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 3, Periods: 3}
	p.OwnershipSchedule = types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 3, Periods: 1}
	p.CompensationPeriod = hold
	return p
}

// custody is what Bank holds for an address: a custody account's balance, or what a payee was paid.
func (s *KeeperTestSuite) custody(a sdk.AccAddress) sdk.Coins {
	if name, ok := s.poolName(a); ok {
		return s.balances[name]
	}
	return s.received[a.String()]
}

// poolName resolves a custody account's address to its module name.
func (s *KeeperTestSuite) poolName(a sdk.AccAddress) (string, bool) {
	for _, name := range types.PoolAccountNames() {
		if a.Equals(authtypes.NewModuleAddress(name)) {
			return name, true
		}
	}
	return "", false
}

func (s *KeeperTestSuite) sdkCtx() sdk.Context { return sdk.UnwrapSDKContext(s.ctx) }

func (s *KeeperTestSuite) now() uint64 { return chain.BlockSeconds(s.ctx) }

// setTime moves the block time, leaving the height.
func (s *KeeperTestSuite) setTime(seconds uint64) {
	s.ctx = s.sdkCtx().WithBlockTime(time.Unix(int64(seconds), 0))
}

// advance moves to the next block, seconds later.
func (s *KeeperTestSuite) advance(seconds int64) {
	ctx := s.sdkCtx()
	s.ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Duration(seconds) * time.Second)).WithBlockHeight(ctx.BlockHeight() + 1)
}

// commit runs one message on a branch and keeps its writes only on success, as the transaction
// cache does; the mocked custody is restored with the store.
func (s *KeeperTestSuite) commit(call func(ctx context.Context) error) error {
	balances, received, pool := cloneCoins(s.balances), cloneCoins(s.received), s.communityPool
	ctx, write := s.sdkCtx().CacheContext()
	if err := call(ctx); err != nil {
		s.balances, s.received, s.communityPool = balances, received, pool
		return err
	}
	write()
	return nil
}

func cloneCoins(m map[string]sdk.Coins) map[string]sdk.Coins {
	out := make(map[string]sdk.Coins, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// deliver routes a message to its handler, as the message router does.
func (s *KeeperTestSuite) deliver(msg sdk.Msg) error {
	return s.commit(func(ctx context.Context) error { return s.handle(ctx, msg) })
}

func (s *KeeperTestSuite) handle(ctx context.Context, msg sdk.Msg) error {
	var err error
	switch m := msg.(type) {
	case *types.MsgUpdateParams:
		_, err = s.msgServer.UpdateParams(ctx, m)
	case *types.MsgSetGrantsMandate:
		_, err = s.msgServer.SetGrantsMandate(ctx, m)
	case *types.MsgOpenTranche:
		_, err = s.msgServer.OpenTranche(ctx, m)
	case *types.MsgReturnUnallocated:
		_, err = s.msgServer.ReturnUnallocated(ctx, m)
	case *types.MsgGrantOwnership:
		_, err = s.msgServer.GrantOwnership(ctx, m)
	case *types.MsgCompensate:
		_, err = s.msgServer.Compensate(ctx, m)
	case *types.MsgCancelGrants:
		_, err = s.msgServer.CancelGrants(ctx, m)
	case *types.MsgReinstateMembers:
		_, err = s.msgServer.ReinstateMembers(ctx, m)
	case *types.MsgCommitteeRegistrar:
		_, err = s.msgServer.CommitteeRegistrar(ctx, m)
	case *types.MsgRegisterMembers:
		_, err = s.msgServer.RegisterMembers(ctx, m)
	case *types.MsgCommitteeSuspend:
		_, err = s.msgServer.CommitteeSuspend(ctx, m)
	case *types.MsgCommitteeReinstate:
		_, err = s.msgServer.CommitteeReinstate(ctx, m)
	case *types.MsgCommitteeCompensate:
		_, err = s.msgServer.CommitteeCompensate(ctx, m)
	case *types.MsgSetPayee:
		_, err = s.msgServer.SetPayee(ctx, m)
	case *types.MsgSetController:
		_, err = s.msgServer.SetController(ctx, m)
	default:
		err = fmt.Errorf("no handler for %T", msg)
	}
	return err
}

// fund adds to a custody account's Bank balance, as a deposit does.
func (s *KeeperTestSuite) fund(account string, coins ...sdk.Coin) {
	s.balances[account] = s.balances[account].Add(coins...)
}

// appointCommittee appoints the committee for a century from now with an allowance, longer than any
// test advances; every call advances the term and clears an appointed registrar.
func (s *KeeperTestSuite) appointCommittee(allowance int64) {
	registrar, err := s.keeper.RegistrarMandate.Get(s.ctx)
	s.Require().NoError(err)
	now := s.now()
	s.Require().NoError(s.deliver(&types.MsgSetGrantsMandate{
		Authority: s.authority, Committee: s.committee, ActivationTime: now, ExpiryTime: now + types.MaxPeriodLength,
		CompensationAllowance: math.NewInt(allowance),
	}))
	s.term++
	if !registrar.IsDisabled() {
		s.registrarTerm++
	}
}

// appointRegistrar names the registrar for the committee's own window; every call advances its term.
func (s *KeeperTestSuite) appointRegistrar() {
	committee, err := s.keeper.GrantsMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.deliver(&types.MsgCommitteeRegistrar{
		Committee: s.committee, ExpectedTerm: s.term, Registrar: s.registrar,
		ActivationTime: committee.ActivationTime, ExpiryTime: committee.ExpiryTime,
	}))
	s.registrarTerm++
}

// openTranche seeds the member account and opens the same amount, as a step proposal would.
func (s *KeeperTestSuite) openTranche(amount int64) {
	s.fund(types.MembersName, noah(amount))
	s.Require().NoError(s.deliver(&types.MsgOpenTranche{Authority: s.authority, Amount: math.NewInt(amount)}))
}

func (s *KeeperTestSuite) registerMembers(addresses ...string) ([]uint64, error) {
	var ids []uint64
	err := s.commit(func(ctx context.Context) error {
		res, err := s.msgServer.RegisterMembers(ctx, &types.MsgRegisterMembers{
			Registrar: s.registrar, ExpectedTerm: s.registrarTerm, Addresses: addresses, Reference: "admission batch",
		})
		if err != nil {
			return err
		}
		ids = res.GrantIds
		return nil
	})
	return ids, err
}

func (s *KeeperTestSuite) register(address string) uint64 {
	ids, err := s.registerMembers(address)
	s.Require().NoError(err)
	s.Require().Len(ids, 1)
	return ids[0]
}

func (s *KeeperTestSuite) grantOwnership(beneficiary string, amount int64, start uint64) (uint64, error) {
	var id uint64
	err := s.commit(func(ctx context.Context) error {
		res, err := s.msgServer.GrantOwnership(ctx, &types.MsgGrantOwnership{
			Authority: s.authority, Beneficiary: beneficiary, Amount: math.NewInt(amount), StartTime: start, Reference: "agreement",
		})
		if err != nil {
			return err
		}
		id = res.GrantId
		return nil
	})
	return id, err
}

func (s *KeeperTestSuite) compensate(beneficiary string, amount sdk.Coin, start uint64, periods uint32, maxOffer math.Int) (uint64, error) {
	var id uint64
	err := s.commit(func(ctx context.Context) error {
		res, err := s.msgServer.Compensate(ctx, &types.MsgCompensate{
			Authority: s.authority, Beneficiary: beneficiary, Amount: amount, StartTime: start, Periods: periods, Reference: "work", MaxOffer: maxOffer,
		})
		if err != nil {
			return err
		}
		id = res.GrantId
		return nil
	})
	return id, err
}

func (s *KeeperTestSuite) committeeCompensate(beneficiary string, amount sdk.Coin, periods uint32) (uint64, error) {
	return s.committeeConvert(beneficiary, amount, periods, math.ZeroInt())
}

// committeeConvert is a committee award that may convert NOAH for what custody lacks.
func (s *KeeperTestSuite) committeeConvert(beneficiary string, amount sdk.Coin, periods uint32, maxOffer math.Int) (uint64, error) {
	var id uint64
	err := s.commit(func(ctx context.Context) error {
		res, err := s.msgServer.CommitteeCompensate(ctx, &types.MsgCommitteeCompensate{
			Committee: s.committee, ExpectedTerm: s.term, Beneficiary: beneficiary, Amount: amount, StartTime: s.now(), Periods: periods, Reference: "committee pay",
			MaxOffer: maxOffer,
		})
		if err != nil {
			return err
		}
		id = res.GrantId
		return nil
	})
	return id, err
}

func (s *KeeperTestSuite) cancel(ids ...uint64) error {
	return s.deliver(&types.MsgCancelGrants{Authority: s.authority, GrantIds: ids})
}

func (s *KeeperTestSuite) suspend(addresses ...string) error {
	return s.deliver(&types.MsgCommitteeSuspend{Committee: s.committee, ExpectedTerm: s.term, Addresses: addresses})
}

func (s *KeeperTestSuite) reinstate(addresses ...string) error {
	return s.deliver(&types.MsgCommitteeReinstate{Committee: s.committee, ExpectedTerm: s.term, Addresses: addresses})
}

func (s *KeeperTestSuite) setPayee(sender string, id uint64, payee string) error {
	return s.deliver(&types.MsgSetPayee{Sender: sender, GrantId: id, Payee: payee})
}

func (s *KeeperTestSuite) setController(sender, beneficiary, controller string) error {
	return s.deliver(&types.MsgSetController{Sender: sender, Beneficiary: beneficiary, Controller: controller})
}

func (s *KeeperTestSuite) grant(id uint64) types.Grant {
	g, err := s.keeper.Grants.Get(s.ctx, id)
	s.Require().NoError(err)
	return g
}

// journal lists one grant's entries through the query.
func (s *KeeperTestSuite) journal(id uint64) []types.GrantEntry {
	res, err := s.queryServer.GrantJournal(s.ctx, &types.QueryGrantJournalRequest{GrantId: id})
	s.Require().NoError(err)
	return res.Entries
}

// actions names a grant's journal actions in order.
func (s *KeeperTestSuite) actions(id uint64) []types.GrantAction {
	entries := s.journal(id)
	out := make([]types.GrantAction, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}

func (s *KeeperTestSuite) endBlock() {
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
}

// deferrals returns every deferral the block has published so far.
func (s *KeeperTestSuite) deferrals() []*types.EventPaymentDeferred {
	var out []*types.EventPaymentDeferred
	for _, e := range s.sdkCtx().EventManager().ABCIEvents() {
		if e.Type != proto.MessageName(&types.EventPaymentDeferred{}) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(e)
		s.Require().NoError(err)
		out = append(out, parsed.(*types.EventPaymentDeferred))
	}
	return out
}

// export validates and returns the keeper's genesis.
func (s *KeeperTestSuite) export() *types.GenesisState {
	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())
	return gs
}

// requireCode asserts the gRPC code a registered error carries through its wraps.
func (s *KeeperTestSuite) requireCode(err error, code codes.Code) {
	s.Require().Error(err)
	s.Require().Equal(code, status.Code(err), err.Error())
}

func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}

func noah(amount int64) sdk.Coin { return chain.NoahCoin(math.NewInt(amount)) }

func usd(amount int64) sdk.Coin { return sdk.NewInt64Coin("ausd", amount) }

var errRefused = errors.New("destination refuses")
