package keeper_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
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

// wide joins two words into one amount below 2^128.
func wide(hi, lo uint64) *big.Int {
	amount := new(big.Int).Lsh(new(big.Int).SetUint64(hi), 64)
	return amount.Or(amount, new(big.Int).SetUint64(lo))
}

// FuzzSettlementCoverageMatchesExactArithmetic compares multiply-before-divide coverage with a
// rational reference. Decimal rounding permits at most one extra base unit; output and custody
// bounds must hold exactly. See x/treasury/README.md for the tolerance.
func FuzzSettlementCoverageMatchesExactArithmetic(f *testing.F) {
	// Each amount is a (high, low) word pair; narrow seeds carry theirs in the low word.
	f.Add(uint64(0), uint64(100), uint64(0), uint64(25), uint64(0), uint64(40), uint64(0), uint64(10), int64(0))
	f.Add(uint64(0), uint64(0), uint64(0), uint64(1), uint64(0), uint64(1), uint64(0), uint64(1), int64(1_000))
	f.Add(uint64(0), uint64(1), uint64(0), uint64(1), uint64(0), uint64(1_000_000), uint64(0), uint64(1), int64(3_000))
	f.Add(uint64(0), uint64(999_999_999), uint64(0), uint64(1), uint64(0), uint64(3), uint64(0), uint64(1), int64(1))
	f.Add(uint64(0), uint64(7), uint64(0), uint64(3), uint64(0), uint64(5), uint64(0), uint64(3), int64(2_500))
	// The rounding-to-one adjacency: the whole float redeems while the Buffer
	// sits one anoah short of the basis.
	f.Add(
		uint64(0), uint64(0),
		uint64(0), uint64(3_000_000_000_000_000_000),
		uint64(0), uint64(2_999_999_999_999_999_999),
		uint64(0), uint64(3_000_000_000_000_000_000),
		int64(3_000),
	)
	// Found by a fuzz run against an earlier draw, kept as a regression seed.
	// The second is the same block swept to the top of the multiplier domain.
	f.Add(uint64(0), uint64(128), uint64(0), uint64(145), uint64(0), uint64(84), uint64(0), uint64(39), int64(0))
	f.Add(uint64(0), uint64(128), uint64(0), uint64(145), uint64(0), uint64(84), uint64(0), uint64(39), int64(3_000))
	// Every amount at the top of the domain: the output-by-Buffer product nears 2^256.
	f.Add(
		^uint64(0), ^uint64(0),
		^uint64(0), ^uint64(0),
		^uint64(0), ^uint64(0),
		^uint64(0), ^uint64(0),
		int64(0),
	)
	// A wide output against a Buffer near half the basis, where a rounded ratio's
	// error times the output is many anoah.
	f.Add(
		uint64(1)<<36, uint64(0),
		uint64(1)<<63, uint64(12_345),
		uint64(1)<<62, uint64(999),
		uint64(1)<<63, uint64(0),
		int64(1_500),
	)

	f.Fuzz(func(t *testing.T, supplyHi, supplyLo, redeemedHi, redeemedLo, bufferHi, bufferLo, outputHi, outputLo uint64, multiplierMilli int64) {
		// The domain settlement can actually be handed: supply and buffer are
		// bank figures, a redeemed value exists whenever an output does, and the
		// bound the recorder enforces per conversion means the block's output
		// never exceeds the value it retired. The pair is ordered rather than
		// skipped, so the basis bounds the redeemed value, which bounds the output.
		supply, buffer := wide(supplyHi, supplyLo), wide(bufferHi, bufferLo)
		redeemed, output := wide(redeemedHi, redeemedLo), wide(outputHi, outputLo)
		if output.Cmp(redeemed) > 0 {
			output, redeemed = redeemed, output
		}
		if output.Sign() == 0 {
			t.Skip()
		}

		suite := newSettlementFuzzSuite(t, math.NewIntFromBigInt(supply), math.NewIntFromBigInt(buffer))
		// Sweep the exposure multiplier to verify it never changes coverage payments. The rational
		// reference intentionally omits it.
		suite.setFuzzMultiplier(multiplierMilli)
		drawn, err := suite.keeper.SettleConversions(suite.ctx, markettypes.ConversionTotals{
			GrossOffer:        math.ZeroInt(),
			EligiblePrincipal: math.ZeroInt(),
			RedemptionOutput:  math.NewIntFromBigInt(output),
			RedeemedValue:     math.LegacyNewDecFromInt(math.NewIntFromBigInt(redeemed)),
		})
		require.NoError(t, err)

		// Computed without the multiplier, deliberately.
		want := ratCoverageDraw(supply, redeemed, buffer, output)
		// One rounding on the final quantity, so the draw is the exact floor or
		// a single anoah above it — a structural bound, not a scale-dependent
		// tolerance.
		gap := new(big.Int).Sub(drawn.BigInt(), want)
		require.GreaterOrEqual(t, gap.Sign(), 0,
			"draw %s falls below the exact floored share %s", drawn, want)
		require.LessOrEqual(t, gap.Cmp(big.NewInt(1)), 0,
			"draw %s exceeds the exact floored share %s by more than one anoah", drawn, want)
		require.True(t, drawn.LTE(math.NewIntFromBigInt(buffer)), "draw exceeds the Buffer")
		require.True(t, drawn.LTE(math.NewIntFromBigInt(output)), "draw exceeds the output it funds")
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
func newSettlementFuzzSuite(t *testing.T, supply, buffer math.Int) *KeeperTestSuite {
	t.Helper()
	suite := new(KeeperTestSuite)
	suite.SetT(t)
	suite.SetupTest()

	suite.setAssets(chain.USDBaseDenom)
	suite.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	suite.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, supply)).AnyTimes()
	suite.bankKeeper.EXPECT().
		GetBalance(
			gomock.Any(),
			authtypes.NewModuleAddress(types.RedemptionBufferName),
			chain.NoahBaseDenom,
		).
		Return(chain.NoahCoin(buffer)).AnyTimes()
	suite.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()

	return suite
}
