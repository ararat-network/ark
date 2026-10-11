package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/testutil"
	"github.com/ararat-network/ark/x/claims/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx         context.Context
	cdc         codec.Codec
	keeper      *keeper.Keeper
	msgServer   types.MsgServer
	queryClient types.QueryClient
	// queryServer serves clock-sensitive query assertions. queryClient pins
	// the context it was built with (baseapp.NewQueryServerTestHelper stores
	// Ctx at construction), so a later setBlockTime never reaches it.
	queryServer   types.QueryServer
	authority     string
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper
	marketKeeper  *testutil.MockMarketKeeper

	// insurance is what Bank reports the Insurance account holds. It is a
	// suite fixture rather than a per-test expectation because nearly every
	// claims path reads it, and what a test actually varies is the coverage it
	// implies, not the fact of the read.
	insurance sdk.Coins
	// blockedAddrs are the recipients Bank refuses to pay. Empty by default:
	// blocking is the exception each test that cares opts into.
	blockedAddrs map[string]struct{}
	// rates is what the oracle reports fresh, NOAH per unit. The reference is axdr at one NOAH, so a
	// NOAH claim charges its amount.
	rates oracletypes.RateSet
	// assets are the registered assets and their statuses.
	assets map[string]assettypes.AssetStatus
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
	// Height starts far from every block time the suite uses, so a window read off the height
	// fails.
	s.ctx = testCtx.Ctx.WithBlockHeight(heightOrigin).WithBlockTime(time.Unix(0, 0))
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	s.accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)

	s.insurance = sdk.NewCoins()
	s.blockedAddrs = map[string]struct{}{}
	s.rates = oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(8, 1),
	})
	s.assets = map[string]assettypes.AssetStatus{chain.USDBaseDenom: assettypes.AssetStatus_ASSET_STATUS_ACTIVE}

	insuranceAddr := authtypes.NewModuleAddress(types.InsuranceName)
	s.accountKeeper.EXPECT().
		GetModuleAddress(types.InsuranceName).
		Return(insuranceAddr).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(insuranceAddr) {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return sdk.NewCoin(denom, s.insurance.AmountOf(denom))
		}).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), insuranceAddr).
		DoAndReturn(func(context.Context, sdk.AccAddress) sdk.Coins { return s.insurance }).
		AnyTimes()
	s.bankKeeper.EXPECT().
		BlockedAddr(gomock.Any()).
		DoAndReturn(func(addr sdk.AccAddress) bool {
			_, blocked := s.blockedAddrs[addr.String()]
			return blocked
		}).
		AnyTimes()

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().GetReferenceDenom(gomock.Any()).Return(chain.XDRBaseDenom, nil).AnyTimes()
	oracleKeeper.EXPECT().
		GetRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			for _, denom := range denoms {
				rate, ok := s.rates[denom]
				if !ok {
					return nil, fmt.Errorf("no fresh rate for %s", denom)
				}
				rates[denom] = rate
			}
			return rates, nil
		}).
		AnyTimes()
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)
	assetKeeper.EXPECT().
		GetAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (assettypes.Asset, error) {
			status, ok := s.assets[denom]
			if !ok {
				return assettypes.Asset{}, fmt.Errorf("asset %s is not registered", denom)
			}
			return assettypes.Asset{Denom: denom, Status: status}, nil
		}).
		AnyTimes()
	assetKeeper.EXPECT().
		HasAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (bool, error) {
			_, ok := s.assets[denom]
			return ok, nil
		}).
		AnyTimes()
	s.marketKeeper = testutil.NewMockMarketKeeper(ctrl)
	s.keeper = keeper.NewKeeper(
		s.cdc,
		runtime.NewKVStoreService(key),
		s.authority,
		s.accountKeeper,
		wasmKeeper,
		s.bankKeeper,
		oracleKeeper,
		assetKeeper,
	)
	s.keeper.SetMarketKeeper(s.marketKeeper)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, types.DefaultClaimsMandate()))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.ZeroInt()))
	s.Require().NoError(s.keeper.NextClaimID.Set(s.ctx, 1))

	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)
	s.queryServer = keeper.NewQueryServerImpl(s.keeper)
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

// heightOrigin is the suite's first height.
const heightOrigin = 1_000_000

// setBlockTime moves to the next block at the given block time.
func (s *KeeperTestSuite) setBlockTime(seconds int64) {
	ctx := sdk.UnwrapSDKContext(s.ctx)
	s.ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1).WithBlockTime(time.Unix(seconds, 0))
}

// setBlockHeight moves the height alone, leaving the block time.
func (s *KeeperTestSuite) setBlockHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

// blockHeight is the current height as a claim records it.
func (s *KeeperTestSuite) blockHeight() uint64 {
	return uint64(sdk.UnwrapSDKContext(s.ctx).BlockHeight())
}

// fundInsurance sets what Bank reports Insurance holds in NOAH.
func (s *KeeperTestSuite) fundInsurance(amount int64) {
	s.hold(noahCoin(amount))
}

// hold sets what Bank reports Insurance holds in coin's denomination.
func (s *KeeperTestSuite) hold(coin sdk.Coin) {
	rest := sdk.NewCoins()
	for _, held := range s.insurance {
		if held.Denom != coin.Denom {
			rest = rest.Add(held)
		}
	}
	s.insurance = rest.Add(coin)
}

// expectPayouts arranges for Bank to pay claims and to drop the Insurance
// balance by what it pays, so a test that settles more than one claim keeps
// reporting a coherent balance.
func (s *KeeperTestSuite) expectPayouts(times int) {
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ string, _ sdk.AccAddress, coins sdk.Coins) error {
			s.insurance = s.insurance.Sub(coins...)
			return nil
		}).
		Times(times)
}

// expectBuy arranges one Market buy that costs offer and delivers output into Insurance custody.
func (s *KeeperTestSuite) expectBuy(ask, output sdk.Coin, maxOffer, offer math.Int) {
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
	s.marketKeeper.EXPECT().
		Buy(gomock.Any(), insurance, ask, chain.NoahBaseDenom, chain.NoahCoin(maxOffer)).
		DoAndReturn(func(context.Context, sdk.AccAddress, sdk.Coin, string, sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error) {
			s.insurance = s.insurance.Sub(chain.NoahCoin(offer)).Add(output)
			return chain.NoahCoin(offer), output, sdk.NewDecCoin(chain.NoahBaseDenom, math.ZeroInt()), nil
		})
}

// atSubmission pins a five-second veto window and moves to block time 20, inside the suite's
// appointment window.
func (s *KeeperTestSuite) atSubmission() {
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
}

// submitGovernance submits a governance claim to testAddress(3) at the current block time.
func (s *KeeperTestSuite) submitGovernance(amount sdk.Coin, maxOffer int64) (uint64, error) {
	resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
		Authority: s.authority,
		Reference: "incident",
		Recipient: testAddress(3),
		Amount:    amount,
		MaxOffer:  math.NewInt(maxOffer),
	})
	if err != nil {
		return 0, err
	}
	return resp.ClaimId, nil
}

// submitGovernanceClaim submits a NOAH governance claim that must be accepted.
func (s *KeeperTestSuite) submitGovernanceClaim(amount int64) uint64 {
	id, err := s.submitGovernance(noahCoin(amount), 0)
	s.Require().NoError(err)
	return id
}

// submitCommittee submits a committee claim to testAddress(3) under the suite's appointment at the
// current block time.
func (s *KeeperTestSuite) submitCommittee(amount sdk.Coin, maxOffer int64) (uint64, error) {
	resp, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
		Committee:    testAddress(1),
		ExpectedTerm: 1,
		Reference:    "incident",
		Recipient:    testAddress(3),
		Amount:       amount,
		MaxOffer:     math.NewInt(maxOffer),
	})
	if err != nil {
		return 0, err
	}
	return resp.ClaimId, nil
}

// reserved reads a denomination's reservation, zero when none is stored.
func (s *KeeperTestSuite) reserved(denom string) (math.Int, error) {
	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx, denom)
	if errors.Is(err, collections.ErrNotFound) {
		return math.ZeroInt(), nil
	}
	return reserved, err
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

// appointCommittee stores an active mandate spanning block times 10–100 with a
// 1000-unit limit, a thousand NOAH at the suite's rates, and resets allowance usage, mirroring a
// successful MsgSetClaimsMandate.
func (s *KeeperTestSuite) appointCommittee(committee string) types.ClaimsMandate {
	claimsMandate := types.ClaimsMandate{
		Envelope: mandate.Envelope{
			Term:           1,
			Committee:      committee,
			ActivationTime: 10,
			ExpiryTime:     100,
		},
		CommitteeClaimLimit: math.NewInt(1_000),
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
