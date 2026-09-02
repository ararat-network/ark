package keeper_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// ratCoverageDraw is the coverage draw in exact rational arithmetic: the
// Buffer's share of the pre-burn basis, applied to the block's output and
// truncated once. It is the definition the keeper's LegacyDec implementation
// approximates, and the fuzz below asserts the two agree.
func ratCoverageDraw(liability, redeemed, buffer, output *big.Int) *big.Int {
	basis := new(big.Int).Add(liability, redeemed)
	if basis.Sign() <= 0 {
		return big.NewInt(0)
	}
	coverage := new(big.Rat).SetFrac(buffer, basis)
	if coverage.Cmp(new(big.Rat).SetInt64(1)) > 0 {
		coverage = new(big.Rat).SetInt64(1)
	}
	drawn := new(big.Rat).Mul(coverage, new(big.Rat).SetInt(output))

	return new(big.Int).Quo(drawn.Num(), drawn.Denom())
}

// FuzzSettlementCoverageMatchesExactArithmetic pins the one calculation in
// settlement that divides, against the exact rational answer.
//
// The draw multiplies the output by the Buffer before dividing by the basis, so
// the only rounding is the last place of the final quantity: the result is the
// exact floored share, or one anoah above it when the true share sits within
// half an ulp below a whole number. That single anoah is the whole difference
// between this and a fused multiply-divide, which would need a primitive
// neither cosmossdk.io/math nor pkg/decimal provides.
//
// The order matters more than the width. Forming the ratio first would round an
// intermediate against its own bound of one, and the output would amplify that
// into a draw above the Buffer — a block nothing corrupted, failing settlement.
// The last seed below is such a block: the whole float redeems while the Buffer
// sits one anoah short of the basis. Both bounds are therefore asserted
// exactly, never within a tolerance.
func FuzzSettlementCoverageMatchesExactArithmetic(f *testing.F) {
	f.Add(int64(100), int64(25), int64(40), int64(10), int64(0))
	f.Add(int64(0), int64(1), int64(1), int64(1), int64(1_000))
	f.Add(int64(1), int64(1), int64(1_000_000), int64(1), int64(3_000))
	f.Add(int64(999_999_999), int64(1), int64(3), int64(1), int64(1))
	f.Add(int64(7), int64(3), int64(5), int64(3), int64(2_500))
	// The rounding-to-one adjacency: the whole float redeems while the Buffer
	// sits one anoah short of the basis.
	f.Add(
		int64(0),
		int64(3_000_000_000_000_000_000),
		int64(2_999_999_999_999_999_999),
		int64(3_000_000_000_000_000_000),
		int64(3_000),
	)
	// Found by a fuzz run against an earlier draw, kept as a regression seed.
	// The second is the same block swept to the top of the multiplier domain.
	f.Add(int64(128), int64(145), int64(84), int64(39), int64(0))
	f.Add(int64(128), int64(145), int64(84), int64(39), int64(3_000))

	f.Fuzz(func(t *testing.T, supply, redeemed, buffer, output, multiplierMilli int64) {
		// The domain settlement can actually be handed: supply and buffer are
		// bank figures, a redeemed value exists whenever an output does, and the
		// bound the recorder enforces per conversion means the block's output
		// never exceeds the value it retired.
		if supply < 0 || buffer < 0 || redeemed <= 0 || output <= 0 || output > redeemed {
			t.Skip()
		}

		suite := newSettlementFuzzSuite(t, supply, buffer)
		// The exposure multiplier, swept across its whole domain. The draw must
		// not move with it (D73): the multiplier scales requirement bases, and
		// the coverage basis is a payment denominator. The rational oracle below
		// does not take it as an argument at all, which is the assertion — every
		// bound holds identically at a multiplier of one and of four.
		suite.setFuzzMultiplier(multiplierMilli)
		drawn, err := suite.keeper.SettleConversions(suite.ctx, markettypes.ConversionTotals{
			GrossOffer:        math.ZeroInt(),
			EligiblePrincipal: math.ZeroInt(),
			RedemptionOutput:  math.NewInt(output),
			RedeemedValue:     math.LegacyNewDec(redeemed),
		})
		require.NoError(t, err)

		// Computed without the multiplier, deliberately.
		want := ratCoverageDraw(
			big.NewInt(supply),
			big.NewInt(redeemed),
			big.NewInt(buffer),
			big.NewInt(output),
		)
		// One rounding on the final quantity, so the draw is the exact floor or
		// a single anoah above it — a structural bound, not a scale-dependent
		// tolerance.
		gap := new(big.Int).Sub(drawn.BigInt(), want)
		require.GreaterOrEqual(t, gap.Sign(), 0,
			"draw %s falls below the exact floored share %s", drawn, want)
		require.LessOrEqual(t, gap.Cmp(big.NewInt(1)), 0,
			"draw %s exceeds the exact floored share %s by more than one anoah", drawn, want)
		require.True(t, drawn.LTE(math.NewInt(buffer)), "draw exceeds the Buffer")
		require.True(t, drawn.LTE(math.NewInt(output)), "draw exceeds the output it funds")
	})
}

// setFuzzMultiplier installs an applied multiplier drawn from the fuzz input,
// mapped into [1, 4] — the launch cap — in thousandths. Any input maps to a
// valid multiplier rather than skipping, so the sweep spends every case on the
// invariance being asserted.
func (s *KeeperTestSuite) setFuzzMultiplier(milli int64) {
	if milli < 0 {
		milli = -milli
	}
	state := types.DefaultExposureState()
	state.Multiplier = math.LegacyOneDec().Add(
		math.LegacyNewDec(milli % 3_001).Quo(math.LegacyNewDec(1_000)),
	)
	s.Require().NoError(s.keeper.ExposureState.Set(s.ctx, state))
}

// newSettlementFuzzSuite stands the keeper suite up outside its runner, which
// lets a fuzz target reuse the mocks every other settlement test is written
// against. One priced member carries the whole aggregate, so the liability the
// draw divides by is exactly the supply the case names.
func newSettlementFuzzSuite(t *testing.T, supply, buffer int64) *KeeperTestSuite {
	t.Helper()
	suite := new(KeeperTestSuite)
	suite.SetT(t)
	suite.SetupTest()

	suite.setAssets(chain.USDBaseDenom)
	suite.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	suite.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, supply)).AnyTimes()
	suite.bankKeeper.EXPECT().
		GetBalance(
			gomock.Any(),
			authtypes.NewModuleAddress(types.RedemptionBufferName),
			chain.NoahBaseDenom,
		).
		Return(chain.NoahCoin(math.NewInt(buffer))).AnyTimes()
	suite.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()

	return suite
}
