package keeper_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

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

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/testutil"
	"github.com/ararat-network/ark/x/claims/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx         context.Context
	cdc         codec.Codec
	keeper      *keeper.Keeper
	msgServer   types.MsgServer
	queryClient types.QueryClient
	// queryServer serves height-sensitive query assertions. queryClient pins
	// the context it was built with (baseapp.NewQueryServerTestHelper stores
	// Ctx at construction), so a later setBlockHeight never reaches it.
	queryServer   types.QueryServer
	authority     string
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper

	// insuranceBalance is what Bank reports for the Insurance account. It is a
	// suite fixture rather than a per-test expectation because nearly every
	// claims path reads it, and what a test actually varies is the coverage it
	// implies, not the fact of the read.
	insuranceBalance math.Int
	// blockedAddrs are the recipients Bank refuses to pay. Empty by default:
	// blocking is the exception each test that cares opts into.
	blockedAddrs map[string]struct{}
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	s.cdc = codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, transientKey)
	s.ctx = testCtx.Ctx
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	s.accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)

	s.insuranceBalance = math.ZeroInt()
	s.blockedAddrs = map[string]struct{}{}

	insuranceAddr := authtypes.NewModuleAddress(types.InsuranceName)
	s.accountKeeper.EXPECT().
		GetModuleAddress(types.InsuranceName).
		Return(insuranceAddr).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(insuranceAddr) {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return sdk.NewCoin(denom, s.insuranceBalance)
		}).
		AnyTimes()
	s.bankKeeper.EXPECT().
		BlockedAddr(gomock.Any()).
		DoAndReturn(func(addr sdk.AccAddress) bool {
			_, blocked := s.blockedAddrs[addr.String()]
			return blocked
		}).
		AnyTimes()

	s.keeper = keeper.NewKeeper(
		s.cdc,
		runtime.NewKVStoreService(key),
		s.authority,
		s.accountKeeper,
		s.bankKeeper,
	)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, types.DefaultClaimsMandate()))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.ZeroInt()))
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.ZeroInt()))
	s.Require().NoError(s.keeper.NextClaimID.Set(s.ctx, 1))

	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)
	s.queryServer = keeper.NewQueryServerImpl(s.keeper)
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *KeeperTestSuite) setBlockHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

// fundInsurance sets what Bank reports for the Insurance account.
func (s *KeeperTestSuite) fundInsurance(amount int64) {
	s.insuranceBalance = math.NewInt(amount)
}

// expectPayouts arranges for Bank to pay claims and to drop the Insurance
// balance by what it pays, so a test that settles more than one claim keeps
// reporting a coherent balance.
func (s *KeeperTestSuite) expectPayouts(times int) {
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ string, _ sdk.AccAddress, coins sdk.Coins) error {
			s.insuranceBalance = s.insuranceBalance.Sub(coins.AmountOf(chain.NoahBaseDenom))
			return nil
		}).
		Times(times)
}

// requireStatus asserts the stored lifecycle state of one claim.
func (s *KeeperTestSuite) requireStatus(claimID uint64, expected types.ClaimStatus) {
	claim, err := s.keeper.Claims.Get(s.ctx, claimID)
	s.Require().NoError(err)
	s.Require().Equal(expected, claim.Status, "claim %d", claimID)
}

// requireDueClaims asserts exactly which claims remain queued for settlement.
func (s *KeeperTestSuite) requireDueClaims(expected ...uint64) {
	due := make([]uint64, 0, len(expected))
	s.Require().NoError(s.keeper.DueClaims.Walk(s.ctx, nil, func(key collections.Pair[uint64, uint64]) (bool, error) {
		due = append(due, key.K2())
		return false, nil
	}))
	if len(expected) == 0 {
		s.Require().Empty(due)
		return
	}
	s.Require().Equal(expected, due)
}

// appointCommittee stores an active mandate spanning the supplied window and
// resets allowance usage, mirroring a successful MsgSetClaimsMandate.
func (s *KeeperTestSuite) appointCommittee(
	committee string,
	term uint64,
	activation uint64,
	expiry uint64,
	limit int64,
) types.ClaimsMandate {
	claimsMandate := types.ClaimsMandate{
		Envelope: mandate.Envelope{
			Term:             term,
			Committee:        committee,
			ActivationHeight: activation,
			ExpiryHeight:     expiry,
		},
		CommitteeClaimLimit: noahCoin(limit),
	}
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, claimsMandate))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.ZeroInt()))
	return claimsMandate
}

func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}

func noahCoin(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(chain.NoahBaseDenom, amount)
}
