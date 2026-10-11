package simulation_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/claims/keeper"
	"github.com/ararat-network/ark/x/claims/simulation"
	"github.com/ararat-network/ark/x/claims/testutil"
	"github.com/ararat-network/ark/x/claims/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

type claimFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  *simsx.BasicSimulationReporter
	accounts  []simtypes.Account
	rand      *rand.Rand
	// insurance is what Bank reports Insurance holds; Market's fills move it.
	insurance *sdk.Coins
}

// newClaimFixture builds a real Claims keeper over mocks, with Insurance
// holding the given NOAH balance, so an emitted message can be delivered through
// the handler it targets. The default registry's currencies are active and priced
// at one NOAH, and Market fills a buy at par.
func newClaimFixture(t *testing.T, insurance int64) claimFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10).WithBlockTime(time.Unix(10, 0))

	ctrl := gomock.NewController(t)
	insuranceAddr := authtypes.NewModuleAddress(types.InsuranceName)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(types.InsuranceName).Return(insuranceAddr).AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(govtypes.ModuleName).Return(authtypes.NewModuleAddress(govtypes.ModuleName)).AnyTimes()
	held := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, insurance))
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(insuranceAddr) {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return sdk.NewCoin(denom, held.AmountOf(denom))
		}).
		AnyTimes()
	bankKeeper.EXPECT().BlockedAddr(gomock.Any()).Return(false).AnyTimes()

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	// The reference is axdr at one NOAH, so a NOAH claim charges its amount.
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().GetReferenceDenom(gomock.Any()).Return(chain.XDRBaseDenom, nil).AnyTimes()
	oracleKeeper.EXPECT().
		GetRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			for _, denom := range denoms {
				rates[denom] = math.LegacyOneDec()
			}
			return rates, nil
		}).
		AnyTimes()
	registered := map[string]bool{}
	for _, asset := range assettypes.DefaultGenesisState().Assets {
		registered[asset.Denom] = true
	}
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)
	assetKeeper.EXPECT().
		GetAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (assettypes.Asset, error) {
			if !registered[denom] {
				return assettypes.Asset{}, fmt.Errorf("asset %s is not registered", denom)
			}
			return assettypes.Asset{Denom: denom, Status: assettypes.AssetStatus_ASSET_STATUS_ACTIVE}, nil
		}).
		AnyTimes()
	marketKeeper := testutil.NewMockMarketKeeper(ctrl)
	marketKeeper.EXPECT().
		Buy(gomock.Any(), insuranceAddr, gomock.Any(), chain.NoahBaseDenom, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ sdk.AccAddress, ask sdk.Coin, _ string, maximumOffer sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error) {
			offer := chain.NoahCoin(ask.Amount)
			if offer.Amount.GT(maximumOffer.Amount) {
				return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, fmt.Errorf("maximum %s, needed %s", maximumOffer, offer)
			}
			held = held.Sub(offer).Add(ask)
			return offer, ask, sdk.NewDecCoin(chain.NoahBaseDenom, math.ZeroInt()), nil
		}).
		AnyTimes()
	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		wasmKeeper,
		bankKeeper,
		oracleKeeper,
		assetKeeper,
	)
	k.SetMarketKeeper(marketKeeper)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	require.NoError(t, k.ClaimsMandate.Set(ctx, types.DefaultClaimsMandate()))
	require.NoError(t, k.ClaimsAllowanceUsed.Set(ctx, math.ZeroInt()))
	require.NoError(t, k.NextClaimID.Set(ctx, 1))

	r := rand.New(rand.NewSource(1))
	accounts := simtypes.RandomAccounts(r, 2)
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		accountKeeper,
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		accounts...,
	)

	return claimFixture{
		ctx:       ctx,
		keeper:    k,
		msgServer: keeper.NewMsgServerImpl(k),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
		accounts:  accounts,
		rand:      r,
		insurance: &held,
	}
}

func TestMsgSetClaimsMandateFactory(t *testing.T) {
	f := newClaimFixture(t, 0)

	_, msg := simulation.MsgSetClaimsMandateFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationTime, msg.ExpiryTime)
	require.True(t, msg.CommitteeClaimLimit.IsPositive())
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, msg.ExpiryTime-msg.ActivationTime, params.ClaimCancellationPeriod)
	// The factory's whole job is to emit what the handler accepts.
	_, err = f.msgServer.SetClaimsMandate(f.ctx, msg)
	require.NoError(t, err)
}

func TestMsgSubmitClaimFactory(t *testing.T) {
	t.Run("skips when Insurance holds nothing", func(t *testing.T) {
		f := newClaimFixture(t, 0)

		_, msg := simulation.MsgSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "beyond what is already reserved")
		require.Nil(t, msg)
	})

	t.Run("books a claim Insurance can cover", func(t *testing.T) {
		f := newClaimFixture(t, 1_000_000)

		_, msg := simulation.MsgSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.NotEmpty(t, msg.Reference)
		require.True(t, msg.Amount.Amount.LTE(math.NewInt(1_000_000)))
		_, err := f.msgServer.SubmitClaim(f.ctx, msg)
		require.NoError(t, err)
	})
}

func TestMsgCancelClaimFactory(t *testing.T) {
	t.Run("skips without a pending claim", func(t *testing.T) {
		f := newClaimFixture(t, 1_000_000)

		_, msg := simulation.MsgCancelClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no pending claim")
		require.Nil(t, msg)
	})

	t.Run("cancels a claim still inside its window", func(t *testing.T) {
		f := newClaimFixture(t, 1_000_000)
		_, submit := simulation.MsgSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)
		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		_, err := f.msgServer.SubmitClaim(f.ctx, submit)
		require.NoError(t, err)

		_, msg := simulation.MsgCancelClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Positive(t, msg.ClaimId)
		_, err = f.msgServer.CancelClaim(f.ctx, msg)
		require.NoError(t, err)
	})
}
