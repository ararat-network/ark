package keeper_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/x/security/keeper"
	"github.com/ararat-network/ark/x/security/types"
)

// Fixtures shared across the keeper suite.
const (
	committeeUpgradeName     = "v2-emergency"
	committeeUpgradeNameB    = "v2-emergency-b"
	governanceUpgradeName    = "v3-planned"
	testSubjectClient        = "07-tendermint-0"
	testSubstituteClient     = "07-tendermint-1"
	notAppointedCommitteeErr = "security mandate: signer is not the exact appointed committee"
)

// stubRouter records what the keeper dispatched instead of executing it. The
// handlers are judged on the upstream message they construct, above all the
// signer they stamp on it; the real targets are exercised from the app tests.
type stubRouter struct {
	dispatched []sdk.Msg
	// events, when set, are returned on the handler result, standing in for a
	// target that emitted under the router's own event manager.
	events sdk.Events
	// err, when set, is returned by the handler, standing in for a target that
	// rejects the message.
	err error
	// unroutable, when set, makes Handler return nil, standing in for a target
	// module that was never registered.
	unroutable bool
}

func (r *stubRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	if r.unroutable {
		return nil
	}

	return func(_ sdk.Context, req sdk.Msg) (*sdk.Result, error) {
		if r.err != nil {
			return nil, r.err
		}
		r.dispatched = append(r.dispatched, req)

		return &sdk.Result{Events: r.events.ToABCIEvents()}, nil
	}
}

func (r *stubRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler {
	return nil
}

func (r *stubRouter) only() sdk.Msg {
	if len(r.dispatched) != 1 {
		return nil
	}

	return r.dispatched[0]
}

// stubAccountKeeper serves the committee account an appointment observes,
// absent by default.
type stubAccountKeeper struct {
	accounts map[string]sdk.AccountI
}

func (s *stubAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return s.accounts[addr.String()]
}

// stubWasmKeeper answers for a contract store holding no code.
type stubWasmKeeper struct{}

func (stubWasmKeeper) HasContractInfo(context.Context, sdk.AccAddress) bool { return false }

// stubUpgradeKeeper serves one optional pending plan.
type stubUpgradeKeeper struct {
	plan *upgradetypes.Plan
}

func (s *stubUpgradeKeeper) GetUpgradePlan(context.Context) (upgradetypes.Plan, error) {
	if s.plan == nil {
		return upgradetypes.Plan{}, upgradetypes.ErrNoUpgradePlanFound
	}

	return *s.plan, nil
}

type KeeperTestSuite struct {
	suite.Suite

	ctx         sdk.Context
	keeper      *keeper.Keeper
	msgServer   types.MsgServer
	queryClient types.QueryClient

	router    *stubRouter
	account   *stubAccountKeeper
	upgrade   *stubUpgradeKeeper
	authority string
	committee string
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	s.ctx = testCtx.Ctx.WithBlockHeight(100)

	s.router = &stubRouter{}
	s.account = &stubAccountKeeper{accounts: map[string]sdk.AccountI{}}
	s.upgrade = &stubUpgradeKeeper{}

	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	s.committee = authtypes.NewModuleAddress("security-committee").String()

	s.keeper = keeper.NewKeeper(cdc, storeService, s.authority, s.router, s.account, stubWasmKeeper{}, s.upgrade)

	s.Require().NoError(s.keeper.Mandate.Set(s.ctx, types.DefaultSecurityMandate()))
	s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{}))

	queryHelper := baseapp.NewQueryServerTestHelper(s.ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

// appoint installs a live committee with a window containing the suite's
// height.
func (s *KeeperTestSuite) appoint() {
	appointment := types.NewDisabledSecurityMandate(1)
	appointment.Committee = s.committee
	appointment.ActivationHeight = 50
	appointment.ExpiryHeight = 500
	s.Require().NoError(appointment.Validate())
	s.Require().NoError(s.keeper.Mandate.Set(s.ctx, appointment))
}

// TestDispatchUsesConsensusParamsAuthority pins that the signer stamped on a
// dispatched message is the address sdk.ValidateAuthority would demand: the
// consensus-params authority when one is set, the keeper fallback otherwise.
// Getting it wrong breaks every committee power on an authority rotation.
func (s *KeeperTestSuite) TestDispatchUsesConsensusParamsAuthority() {
	s.appoint()
	rotated := authtypes.NewModuleAddress("rotated-authority").String()

	s.Run("falls back to the keeper authority", func() {
		s.router.dispatched = nil
		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       200,
		})
		s.Require().NoError(err)
		dispatched, ok := s.router.only().(*upgradetypes.MsgSoftwareUpgrade)
		s.Require().True(ok)
		s.Require().Equal(s.authority, dispatched.Authority)
	})

	s.Run("prefers the consensus-params authority", func() {
		s.router.dispatched = nil
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{}))
		ctx := s.ctx.WithConsensusParams(cmtproto.ConsensusParams{
			Authority: &cmtproto.AuthorityParams{Authority: rotated},
		})

		_, err := s.msgServer.CommitteePlanUpgrade(ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       200,
		})
		s.Require().NoError(err)
		dispatched, ok := s.router.only().(*upgradetypes.MsgSoftwareUpgrade)
		s.Require().True(ok)
		s.Require().Equal(rotated, dispatched.Authority)
	})
}

// TestDispatchFailuresSurface covers the two ways the router can refuse:
// an unregistered target, and a target that rejects the message.
func (s *KeeperTestSuite) TestDispatchFailuresSurface() {
	s.appoint()

	s.Run("unroutable target", func() {
		s.router.unroutable = true
		defer func() { s.router.unroutable = false }()

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       200,
		})
		s.Require().ErrorContains(err, "no handler registered for /cosmos.upgrade.v1beta1.MsgSoftwareUpgrade")
	})

	s.Run("target rejects", func() {
		s.router.err = errors.New("plan height is in the past")
		defer func() { s.router.err = nil }()

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       200,
		})
		s.Require().ErrorContains(err, "plan height is in the past")

		// The record must not survive a dispatch that failed, or the committee
		// would hold a claim on a slot it never filled.
		record, recordErr := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(recordErr)
		s.Require().True(record.IsZero())
	})
}
