package keeper

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

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
	"github.com/ararat-network/ark/x/market/testutil"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

type AccumulatorTestSuite struct {
	suite.Suite

	ctx    sdk.Context
	keeper *Keeper
}

func TestAccumulatorTestSuite(t *testing.T) {
	suite.Run(t, new(AccumulatorTestSuite))
}

func (s *AccumulatorTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, transientKey)
	s.ctx = testCtx.Ctx

	ctrl := gomock.NewController(s.T())
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	s.keeper = NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		runtime.NewTransientStoreService(transientKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		wasmKeeper,
		testutil.NewMockBankKeeper(ctrl),
		testutil.NewMockOracleKeeper(ctrl),
		testutil.NewMockTreasuryKeeper(ctrl),
		testutil.NewMockAssetKeeper(ctrl),
	)
}

// TestEmptyBlockTotals covers the EndBlocker's licence to skip settlement: a
// block that recorded nothing must read back as zero rather than as absent
// state the caller has to interpret.
func (s *AccumulatorTestSuite) TestEmptyBlockTotals() {
	totals, err := s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().True(totals.IsZero())
	s.Require().NoError(totals.Validate())
	s.Require().True(totals.EligiblePrincipal.IsZero())
	s.Require().True(totals.RedemptionOutput.IsZero())
	s.Require().True(totals.RedeemedValue.IsZero())
}

// expansionRates prices one stable at one NOAH, which makes a conversion's
// eligible principal its output amount and its spread whatever the offer holds
// above that.
func expansionRates() oracletypes.RateSet {
	return oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
}

// TestExpansionAccumulates covers what the waterfall is handed: the gross
// offer, spread included, beside the principal liability grew by, so the block
// total is exactly what settlement has to place and the flow indicator reads.
func (s *AccumulatorTestSuite) TestExpansionAccumulates() {
	s.Require().NoError(s.keeper.recordExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
		sdk.NewInt64Coin(chain.USDBaseDenom, 990),
		expansionRates(),
	))
	s.Require().NoError(s.keeper.recordExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 500),
		sdk.NewInt64Coin(chain.USDBaseDenom, 495),
		expansionRates(),
	))

	totals, err := s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(totals.Validate())
	s.Require().False(totals.IsZero())
	s.Require().Equal(math.NewInt(1_500), totals.GrossOffer)
	s.Require().Equal(math.NewInt(1_485), totals.EligiblePrincipal)
	s.Require().True(totals.RedemptionOutput.IsZero())
}

// TestExpansionRecordsASpreadOnlyConversion covers the conversion whose stable
// output valued below one base unit: the whole offer is spread, so it leaves no
// principal but still obliges the EndBlocker to place it (D6).
func (s *AccumulatorTestSuite) TestExpansionRecordsASpreadOnlyConversion() {
	rates := expansionRates()
	// Half a NOAH per unit: the one unit of output values below one anoah.
	rates[chain.USDBaseDenom] = math.LegacyNewDecWithPrec(5, 1)

	s.Require().NoError(s.keeper.recordExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
		sdk.NewInt64Coin(chain.USDBaseDenom, 1),
		rates,
	))

	totals, err := s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(totals.Validate())
	s.Require().False(totals.IsZero())
	s.Require().Equal(math.OneInt(), totals.GrossOffer)
	s.Require().True(totals.EligiblePrincipal.IsZero())
}

// TestExpansionRejections checks accumulator-specific refusals, including minted value above the
// NOAH offer received in custody.
func (s *AccumulatorTestSuite) TestExpansionRejections() {
	testCases := []struct {
		name   string
		offer  sdk.Coin
		output sdk.Coin
	}{
		{
			name:   "non-positive offer",
			offer:  sdk.NewInt64Coin(chain.NoahBaseDenom, 0),
			output: sdk.NewInt64Coin(chain.USDBaseDenom, 990),
		},
		{
			name:   "non-positive output",
			offer:  sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
			output: sdk.NewInt64Coin(chain.USDBaseDenom, 0),
		},
		{
			name:   "output the rate set cannot price",
			offer:  sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
			output: sdk.NewInt64Coin(chain.XDRBaseDenom, 990),
		},
		{
			name:   "output outvalues the offer",
			offer:  sdk.NewInt64Coin(chain.NoahBaseDenom, 989),
			output: sdk.NewInt64Coin(chain.USDBaseDenom, 990),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.Require().Error(s.keeper.recordExpansion(s.ctx, tc.offer, tc.output, expansionRates()))

			totals, err := s.keeper.conversionTotals(s.ctx)
			s.Require().NoError(err)
			s.Require().True(totals.IsZero())
		})
	}
}

// TestRedemptionsShareOneTotal covers the two redemption paths meeting: a
// quoted redemption and a settled one are priced from different rate sources,
// and settlement asks the same two questions of both, so both land here.
func (s *AccumulatorTestSuite) TestRedemptionsShareOneTotal() {
	s.Require().NoError(s.keeper.recordRedemption(s.ctx, math.LegacyNewDec(100), math.NewInt(90)))
	s.Require().NoError(s.keeper.recordRedemption(s.ctx, math.LegacyNewDec(200), math.NewInt(150)))
	// A settled redemption, priced at its plan's own committed rate.
	s.Require().NoError(s.keeper.recordRedemption(s.ctx, math.LegacyNewDec(70), math.NewInt(70)))

	totals, err := s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(totals.Validate())
	s.Require().False(totals.IsZero())
	s.Require().Equal(math.LegacyNewDec(370), totals.RedeemedValue)
	s.Require().Equal(math.NewInt(310), totals.RedemptionOutput)
	s.Require().True(totals.EligiblePrincipal.IsZero())
}

// TestRedemptionRejections covers the bound that keeps the coverage draw inside
// the Buffer: a conversion minting more than it retired is refused where it
// happens, so no block-level total can inherit it.
func (s *AccumulatorTestSuite) TestRedemptionRejections() {
	testCases := []struct {
		name   string
		value  math.LegacyDec
		output math.Int
	}{
		{
			name:   "output exceeds the redeemed liability",
			value:  math.LegacyNewDec(10),
			output: math.NewInt(11),
		},
		{
			name:   "any output against nothing redeemed",
			value:  math.LegacyZeroDec(),
			output: math.NewInt(1),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.Require().Error(s.keeper.recordRedemption(s.ctx, tc.value, tc.output))
		})
	}
}

// TestAccumulatorOverflowIsRefused covers why the adds are checked: no single
// conversion can overflow, but a block's sum has no bound of its own, and the
// refusal has to land on the transaction rather than on the settlement that
// would inherit it.
func (s *AccumulatorTestSuite) TestAccumulatorOverflowIsRefused() {
	nearMax := math.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	s.Require().NoError(s.keeper.addInt(s.ctx, eligiblePrincipalKey, nearMax))
	s.Require().Error(s.keeper.addInt(s.ctx, eligiblePrincipalKey, nearMax))
}

// TestTotalsResetWithTheBlock pins the property that lets the recorders skip
// clearing entirely: the accumulators are transient, so a new block starts from
// nothing without anyone deleting a key.
func (s *AccumulatorTestSuite) TestTotalsResetWithTheBlock() {
	s.Require().NoError(s.keeper.recordExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
		sdk.NewInt64Coin(chain.USDBaseDenom, 990),
		expansionRates(),
	))

	totals, err := s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().False(totals.IsZero())

	s.SetupTest()

	totals, err = s.keeper.conversionTotals(s.ctx)
	s.Require().NoError(err)
	s.Require().True(totals.IsZero())
}
