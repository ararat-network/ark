package integration

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

const liabilityIncompleteEvent = "ark.treasury.v1.EventLiabilityIncomplete"

// capacityRates prices the traded stable and the reference unit the pool is denominated in, both
// in NOAH per unit.
type capacityRates struct {
	name      string
	stable    math.LegacyDec
	reference math.LegacyDec
}

var (
	capacityUnitRates = capacityRates{
		name:      "unit rates",
		stable:    math.LegacyOneDec(),
		reference: math.LegacyOneDec(),
	}
	// Eighteen significant decimals on both legs, so no conversion, spread, or coverage quotient
	// divides exactly.
	capacityMarketRates = capacityRates{
		name:      "market rates",
		stable:    math.LegacyMustNewDecFromStr("0.729394602479941648"),
		reference: math.LegacyMustNewDecFromStr("1.371742112482853224"),
	}
)

// redemptionScenario is the pool, liability, and Buffer a redemption is quoted against. Shares
// are dimensionless: delta and liability against depth, the Buffer against the stable supply's
// NOAH value.
type redemptionScenario struct {
	rates     capacityRates
	depth     math.LegacyDec
	delta     math.LegacyDec
	liability math.LegacyDec
	buffer    math.LegacyDec
	spread    math.LegacyDec
	recovery  uint64
	complete  bool
}

func (s redemptionScenario) String() string {
	return fmt.Sprintf(
		"rates %s/%s, depth %s, delta %s, liability %s, buffer %s, spread %s, recovery %d, complete %t",
		s.rates.stable, s.rates.reference, s.depth, s.delta, s.liability, s.buffer, s.spread, s.recovery, s.complete,
	)
}

// capacityState is what a redemption can move.
type capacityState struct {
	buffer math.Int
	market math.Int
	trader math.Int
	noah   math.Int
	stable math.Int
	delta  math.LegacyDec
}

// settledRedemption is one stable-to-NOAH sell and the block settlement after it, read back from
// state rather than from the keeper's arithmetic.
type settledRedemption struct {
	offer  math.Int
	quoted math.Int
	output math.Int
	// mid is the pool gap the fill left, before EndBlock replenishes it.
	mid       math.LegacyDec
	before    capacityState
	after     capacityState
	disclosed bool
}

// TestRedemptionCapacityEnvelope redeems across a grid of pool imbalance, liability, Buffer, and
// offer size, under complete and incomplete valuation, and checks every settled block against the
// capacity properties.
func TestRedemptionCapacityEnvelope(t *testing.T) {
	deltas := capacityDecs("-0.99", "-0.9", "-0.5", "0", "0.5", "0.9")
	liabilities := capacityDecs("0.1", "1", "10")
	buffers := capacityDecs("0", "0.25", "0.5", "1")
	offers := capacityDecs("0.000001", "0.0001", "0.01", "0.1", "0.25", "0.5", "0.75", "1")

	tests := []struct {
		rates    capacityRates
		complete bool
	}{
		{rates: capacityUnitRates, complete: true},
		{rates: capacityUnitRates, complete: false},
		{rates: capacityMarketRates, complete: true},
		{rates: capacityMarketRates, complete: false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s, complete valuation %t", tc.rates.name, tc.complete), func(t *testing.T) {
			f := newCapacityFixture(t)
			largest := math.ZeroInt()
			for _, delta := range deltas {
				for _, liability := range liabilities {
					for _, buffer := range buffers {
						for _, share := range offers {
							s := redemptionScenario{
								rates:     tc.rates,
								depth:     markettypes.DefaultBasePool.Amount,
								delta:     delta,
								liability: liability,
								buffer:    buffer,
								spread:    markettypes.DefaultMinStabilitySpread,
								recovery:  markettypes.DefaultPoolRecoveryPeriod,
								complete:  tc.complete,
							}
							ctx, supply := f.open(t, s)
							r := f.redeem(t, ctx, capacityShare(share, supply))
							requireRedemptionHolds(t, s, r, fmt.Sprintf(
								"delta %s, liability %s, buffer %s, offer %s", delta, liability, buffer, share,
							))
							largest = math.MaxInt(largest, r.after.noah.Sub(r.before.noah))
						}
					}
				}
			}
			t.Logf("largest residual mint %s, %s of the base pool",
				largest, math.LegacyNewDecFromInt(largest).Quo(markettypes.DefaultBasePool.Amount))
		})
	}
}

// TestRedemptionCapacityPropertiesRandomised checks the same properties over seeded draws of
// every input, rates and depth included.
func TestRedemptionCapacityPropertiesRandomised(t *testing.T) {
	f := newCapacityFixture(t)
	rng := rand.New(rand.NewSource(3))
	for i := range 2_000 {
		spread := capacityPPM(rng, 0, 100_000)
		if i%8 == 0 {
			spread = math.LegacyZeroDec()
		}
		s := redemptionScenario{
			rates: capacityRates{
				name:      "drawn",
				stable:    capacityRandomRate(rng),
				reference: capacityRandomRate(rng),
			},
			depth:     capacityRandomDepth(rng),
			delta:     capacityPPM(rng, -990_000, 1_000_000),
			liability: capacityPPM(rng, 10_000, 10_000_000),
			buffer:    capacityPPM(rng, 0, 1_500_000),
			spread:    spread,
			recovery:  1 + uint64(rng.Int63n(int64(markettypes.MaxPoolRecoveryPeriod))),
			complete:  i%5 != 0,
		}
		share := capacityPPM(rng, 1, 1_000_000)
		ctx, supply := f.open(t, s)
		r := f.redeem(t, ctx, capacityShare(share, supply))
		requireRedemptionHolds(t, s, r, fmt.Sprintf("case %d: %s, offer %s", i, s, share))
	}
}

// TestSameBlockSplitNeverPaysMore splits one redemption into many sells in the same block. The
// pieces never pay the trader more than the whole would have, nor leave more minted after the
// Buffer's draw, beyond what 18-decimal rounding can move.
func TestSameBlockSplitNeverPaysMore(t *testing.T) {
	deltas := capacityDecs("-0.99", "-0.5", "0", "0.5", "0.9")
	liabilities := capacityDecs("0.1", "1", "10")
	buffers := capacityDecs("0", "0.25", "1")
	offers := capacityDecs("0.01", "0.1", "0.5", "1")

	for _, rates := range []capacityRates{capacityUnitRates, capacityMarketRates} {
		t.Run(rates.name, func(t *testing.T) {
			f := newCapacityFixture(t)
			for _, delta := range deltas {
				for _, liability := range liabilities {
					for _, buffer := range buffers {
						for _, share := range offers {
							s := redemptionScenario{
								rates:     rates,
								depth:     markettypes.DefaultBasePool.Amount,
								delta:     delta,
								liability: liability,
								buffer:    buffer,
								spread:    markettypes.DefaultMinStabilitySpread,
								recovery:  markettypes.DefaultPoolRecoveryPeriod,
								complete:  true,
							}
							counts := []int{10, 100}
							if share.Equal(math.LegacyNewDecWithPrec(5, 1)) && buffer.Equal(math.LegacyNewDecWithPrec(25, 2)) {
								counts = append(counts, 1_000)
							}

							ctx, supply := f.open(t, s)
							total := capacityShare(share, supply)
							wholeOutput, wholeResidual := f.redeemPieces(t, ctx, capacityPieces(total, 1))

							// Each sell's rounding can favour the trader by a fraction of a quantum of
							// its gross value, so the pieces together gain under one quantum of the
							// whole; one base unit more covers the whole fill's own truncation.
							gross := math.LegacyNewDecFromInt(total).Mul(rates.stable)
							tolerance := capacityTolerance(gross, 2)
							for _, count := range counts {
								ctx, _ := f.open(t, s)
								output, residual := f.redeemPieces(t, ctx, capacityPieces(total, count))
								label := fmt.Sprintf("delta %s, liability %s, buffer %s, offer %s, %d pieces",
									delta, liability, buffer, share, count)
								require.Truef(t, output.LTE(wholeOutput.Add(tolerance)),
									"%s: pieces paid %s, the whole %s, tolerance %s", label, output, wholeOutput, tolerance)
								// The residual can step one unit where the draw's floor does.
								require.Truef(t, residual.LTE(wholeResidual.Add(tolerance).AddRaw(1)),
									"%s: pieces minted %s net, the whole %s, tolerance %s", label, residual, wholeResidual, tolerance)
							}
						}
					}
				}
			}
		})
	}
}

// TestRestedPoolSpreadFollowsDepth checks sells from a rested pool against the closed form, in
// both directions: x pool units pay x / (depth + x), or the floor where that is less. Depth and
// offer scaled together leave the spread where it was and scale the payout with them.
func TestRestedPoolSpreadFollowsDepth(t *testing.T) {
	// One stable unit is worth one reference unit at a rate that does not divide, so x is the
	// offer's own amount while the payout's arithmetic still rounds.
	par := math.LegacyMustNewDecFromStr("0.729394602479941648")
	rates := capacityRates{name: "par", stable: par, reference: par}
	depths := []math.LegacyDec{
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(500_000)),
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(1_000_000)),
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(2_000_000)),
	}
	tests := []struct {
		name   string
		share  math.LegacyDec
		spread *big.Rat
	}{
		{name: "a hundredth of depth pays the floor", share: math.LegacyNewDecWithPrec(1, 2), spread: big.NewRat(2, 100)},
		{name: "a fiftieth of depth still pays the floor", share: math.LegacyNewDecWithPrec(2, 2), spread: big.NewRat(2, 100)},
		{name: "a forty-eighth of depth pays the pool", share: math.LegacyOneDec().QuoInt64(48), spread: big.NewRat(1, 49)},
		{name: "half of depth pays a third", share: math.LegacyNewDecWithPrec(5, 1), spread: big.NewRat(1, 3)},
		{name: "twice depth pays two thirds", share: math.LegacyNewDec(2), spread: big.NewRat(2, 3)},
	}
	directions := []struct {
		name       string
		offerDenom string
		askDenom   string
	}{
		{name: "redemption", offerDenom: chain.USDBaseDenom, askDenom: chain.NoahBaseDenom},
		{name: "expansion", offerDenom: chain.NoahBaseDenom, askDenom: chain.USDBaseDenom},
	}

	f := newCapacityFixture(t)
	for _, direction := range directions {
		for _, tc := range tests {
			t.Run(direction.name+": "+tc.name, func(t *testing.T) {
				outputs := make([]math.Int, len(depths))
				tolerances := make([]math.Int, len(depths))
				for i, depth := range depths {
					s := redemptionScenario{
						rates:     rates,
						depth:     depth,
						delta:     math.LegacyZeroDec(),
						liability: math.LegacyNewDec(2),
						buffer:    math.LegacyZeroDec(),
						spread:    markettypes.DefaultMinStabilitySpread,
						recovery:  markettypes.DefaultPoolRecoveryPeriod,
						complete:  true,
					}
					ctx, _ := f.open(t, s)
					x := tc.share.Mul(depth).TruncateInt()
					// At par the stable offer is x itself; NOAH is x at the reference rate.
					offer := x
					if direction.offerDenom == chain.NoahBaseDenom {
						offer = math.LegacyNewDecFromInt(x).Mul(par).TruncateInt()
						apptestutil.FundAccount(t, f.app, ctx, f.trader, sdk.NewCoins(chain.NoahCoin(offer)))
					}
					_, output := capacitySell(t, f.app, ctx, f.trader, sdk.NewCoin(direction.offerDenom, offer), direction.askDenom)

					gross := capacityDecRat(math.LegacyNewDecFromInt(offer).Mul(par))
					if direction.offerDenom == chain.NoahBaseDenom {
						gross = capacityDecRat(math.LegacyNewDecFromInt(offer).Quo(par))
					}
					want := capacityFloor(new(big.Rat).Mul(gross, new(big.Rat).Sub(big.NewRat(1, 1), tc.spread)))
					tolerance := capacityTolerance(capacityRatDec(gross), 2)
					require.Truef(t, output.Sub(want).Abs().LTE(tolerance),
						"depth %s: paid %s, closed form %s, tolerance %s", depth, output, want, tolerance)
					outputs[i], tolerances[i] = output, tolerance
				}
				for i := 1; i < len(depths); i++ {
					drift := outputs[i].Sub(outputs[i-1].MulRaw(2)).Abs()
					bound := tolerances[i].Add(tolerances[i-1].MulRaw(2)).AddRaw(2)
					require.Truef(t, drift.LTE(bound),
						"doubling depth paid %s against %s, drift %s past %s", outputs[i], outputs[i-1], drift, bound)
				}
			})
		}
	}
}

// TestPoolRecoveryRegressesDeltaTowardRest drives empty blocks and checks each EndBlock returns
// one part in the recovery period of the imbalance, truncated, never crossing rest; after one
// period at most 1/e of it remains, after two at most 1/e².
func TestPoolRecoveryRegressesDeltaTowardRest(t *testing.T) {
	depth := markettypes.DefaultBasePool.Amount
	tests := []struct {
		name   string
		delta  math.LegacyDec
		period uint64
		blocks int
		// held is an imbalance below one quantum per block, which truncation never moves.
		held bool
	}{
		{
			name:   "an excess of stable",
			delta:  depth.Mul(math.LegacyMustNewDecFromStr("0.512345678901234567")),
			period: 100,
			blocks: 200,
		},
		{
			name:   "a shortfall of stable",
			delta:  depth.Mul(math.LegacyMustNewDecFromStr("-0.876543210987654321")),
			period: 100,
			blocks: 200,
		},
		{
			name:   "less than a quantum a block",
			delta:  math.LegacySmallestDec().MulInt64(99),
			period: 100,
			blocks: 5,
			held:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newCapacityChain(t, capacityMarketRates)
			s := redemptionScenario{
				rates:     capacityMarketRates,
				depth:     depth,
				delta:     math.LegacyZeroDec(),
				liability: math.LegacyZeroDec(),
				buffer:    math.LegacyZeroDec(),
				spread:    markettypes.DefaultMinStabilitySpread,
				recovery:  tc.period,
				complete:  true,
			}
			c.block(func(ctx sdk.Context) {
				seedRedemptionScenario(t, c.app, ctx, c.trader, s)
				require.NoError(t, c.app.MarketKeeper.ArkPoolDelta.Set(ctx, tc.delta))
			})

			expected := tc.delta
			for block := 1; block <= tc.blocks; block++ {
				if block > 1 {
					c.block(nil)
				}
				previous := expected
				expected = previous.Sub(previous.QuoInt64(int64(tc.period)))
				delta := c.state(t).delta
				require.Truef(t, expected.Equal(delta), "block %d: delta %s, want %s", block, delta, expected)
				require.True(t, delta.Abs().LTE(previous.Abs()), "block %d: recovery grew the imbalance", block)
				require.False(t, delta.IsPositive() && previous.IsNegative() || delta.IsNegative() && previous.IsPositive(),
					"block %d: recovery crossed rest", block)

				switch {
				case tc.held:
					require.Truef(t, tc.delta.Equal(delta), "block %d: a sub-quantum regression moved the delta", block)
				case block == int(tc.period):
					// (1 - 1/P)^P < 1/e < 0.36788.
					require.True(t, delta.Abs().LTE(tc.delta.Abs().Mul(math.LegacyMustNewDecFromStr("0.36788"))),
						"one period left %s of %s", delta, tc.delta)
				case block == 2*int(tc.period):
					// (1 - 1/P)^2P < 1/e² < 0.13534.
					require.True(t, delta.Abs().LTE(tc.delta.Abs().Mul(math.LegacyMustNewDecFromStr("0.13534"))),
						"two periods left %s of %s", delta, tc.delta)
				}
			}
		})
	}
}

// TestRedemptionScheduleAcrossRecovery spreads one redemption over more blocks, with the pool
// recovering between them. Every block keeps the capacity properties, so the Buffer and the
// residual mint fund the whole output between them, and from a rested pool a longer schedule
// never pays less.
func TestRedemptionScheduleAcrossRecovery(t *testing.T) {
	const period = 240
	tests := []struct {
		name      string
		scenario  redemptionScenario
		share     math.LegacyDec
		schedules []int
		monotone  bool
	}{
		{
			name: "from a rested pool",
			scenario: redemptionScenario{
				delta:     math.LegacyZeroDec(),
				liability: math.LegacyOneDec(),
				buffer:    math.LegacyZeroDec(),
			},
			share:     math.LegacyNewDecWithPrec(5, 1),
			schedules: []int{1, period / 4, period, 2 * period},
			monotone:  true,
		},
		{
			name: "the whole supply from a stable shortfall, Buffer-funded",
			scenario: redemptionScenario{
				delta:     math.LegacyNewDecWithPrec(-5, 1),
				liability: math.LegacyNewDec(10),
				buffer:    math.LegacyNewDecWithPrec(25, 2),
			},
			share:     math.LegacyOneDec(),
			schedules: []int{1, 10, period / 4, period},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.scenario
			s.rates = capacityMarketRates
			s.depth = markettypes.DefaultBasePool.Amount
			s.spread = markettypes.DefaultMinStabilitySpread
			s.recovery = period
			s.complete = true

			previous := math.ZeroInt()
			for _, blocks := range tc.schedules {
				c := newCapacityChain(t, s.rates)
				var supply math.Int
				c.block(func(ctx sdk.Context) {
					supply = seedRedemptionScenario(t, c.app, ctx, c.trader, s)
				})

				total := capacityShare(tc.share, supply)
				paid, drawn, minted := math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
				for i, piece := range capacityPieces(total, blocks) {
					r := c.redeem(t, piece)
					requireRedemptionHolds(t, s, r, fmt.Sprintf("%d blocks, block %d", blocks, i+1))
					paid = paid.Add(r.output)
					drawn = drawn.Add(r.before.buffer.Sub(r.after.buffer))
					minted = minted.Add(r.after.noah.Sub(r.before.noah))
				}
				require.Equal(t, paid, drawn.Add(minted), "%d blocks: the Buffer and the mint fund the output", blocks)
				if tc.monotone {
					require.Truef(t, paid.GTE(previous),
						"%d blocks paid %s, a shorter schedule %s", blocks, paid, previous)
				}
				previous = paid
			}
		})
	}
}

// TestSteadyRedemptionFlowFloorCorridor holds a one-way flow for three recovery periods. While one
// period's flow is within half the floor times depth, every block pays exactly the floor; past it,
// the imbalance the flow sustains lifts the pool's spread above the floor.
func TestSteadyRedemptionFlowFloorCorridor(t *testing.T) {
	const (
		period = 100
		blocks = 3 * period
	)
	depth := markettypes.DefaultBasePool.Amount
	floor := markettypes.DefaultMinStabilitySpread
	tests := []struct {
		name string
		// flow is one period's flow in pool units, as a multiple of floor times depth.
		flow         math.LegacyDec
		floorThrough bool
	}{
		{name: "within the corridor", flow: math.LegacyNewDecWithPrec(4, 1), floorThrough: true},
		{name: "past the corridor", flow: math.LegacyNewDec(2), floorThrough: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rates := capacityMarketRates
			perBlock := tc.flow.Mul(floor).Mul(depth).QuoInt64(period)
			offer := perBlock.Mul(rates.reference).Quo(rates.stable).TruncateInt()
			s := redemptionScenario{
				rates:     rates,
				depth:     depth,
				delta:     math.LegacyZeroDec(),
				liability: math.LegacyNewDecFromInt(offer.MulRaw(blocks)).Quo(depth).Ceil(),
				buffer:    math.LegacyZeroDec(),
				spread:    floor,
				recovery:  period,
				complete:  true,
			}
			c := newCapacityChain(t, rates)
			c.block(func(ctx sdk.Context) {
				seedRedemptionScenario(t, c.app, ctx, c.trader, s)
			})

			value := capacityDecRat(math.LegacyNewDecFromInt(offer).Mul(rates.stable))
			price := capacityFloor(new(big.Rat).Mul(value, capacityDecRat(math.LegacyOneDec().Sub(floor))))
			var atFloor bool
			for block := 1; block <= blocks; block++ {
				r := c.redeem(t, offer)
				requireRedemptionHolds(t, s, r, fmt.Sprintf("block %d", block))
				atFloor = r.output.Equal(price)
				if block == 1 || tc.floorThrough {
					require.Truef(t, atFloor, "block %d: paid %s, floor price %s", block, r.output, price)
				}
			}
			require.Equal(t, tc.floorThrough, atFloor, "the last block's spread against the floor")
		})
	}
}

// TestConversionRoundTripNeverProfits expands NOAH into a stable and redeems exactly what it
// issued, in the same block or after the pool has recovered, and checks the trader never comes
// back with as much NOAH as it sent. Without a floor, what holds it is that no spread goes below
// zero: a fill that beats frictionless pays the oracle value and no more.
func TestConversionRoundTripNeverProfits(t *testing.T) {
	const period = 40
	shares := capacityDecs("0.01", "0.1", "0.5", "1", "10", "100")
	tests := []struct {
		name   string
		spread math.LegacyDec
		blocks []int
	}{
		{name: "floor", spread: markettypes.DefaultMinStabilitySpread, blocks: []int{0, period / 4, period, 2 * period}},
		{name: "no floor", spread: math.LegacyZeroDec(), blocks: []int{0, period}},
	}

	for _, tc := range tests {
		for _, share := range shares {
			for _, blocks := range tc.blocks {
				t.Run(fmt.Sprintf("%s, %s of depth, %d blocks between", tc.name, share, blocks), func(t *testing.T) {
					s := redemptionScenario{
						rates:     capacityMarketRates,
						depth:     markettypes.DefaultBasePool.Amount,
						delta:     math.LegacyZeroDec(),
						liability: math.LegacyZeroDec(),
						buffer:    math.LegacyZeroDec(),
						spread:    tc.spread,
						recovery:  period,
						complete:  true,
					}
					sent := share.Mul(s.depth).TruncateInt()
					c := newCapacityChain(t, s.rates)
					c.block(func(ctx sdk.Context) {
						seedRedemptionScenario(t, c.app, ctx, c.trader, s)
						apptestutil.FundAccount(t, c.app, ctx, c.trader, sdk.NewCoins(chain.NoahCoin(sent)))
					})
					start := c.state(t)

					var issued, returned math.Int
					expand := func(ctx sdk.Context) {
						_, issued = capacitySell(t, c.app, ctx, c.trader, chain.NoahCoin(sent), chain.USDBaseDenom)
					}
					redeem := func(ctx sdk.Context) {
						_, returned = capacitySell(t, c.app, ctx, c.trader, sdk.NewCoin(chain.USDBaseDenom, issued), chain.NoahBaseDenom)
					}
					if blocks == 0 {
						c.block(func(ctx sdk.Context) { expand(ctx); redeem(ctx) })
					} else {
						c.block(expand)
						for range blocks {
							c.block(nil)
						}
						c.block(redeem)
					}
					end := c.state(t)

					require.Truef(t, returned.LT(sent), "sent %s NOAH, issued %s, returned %s", sent, issued, returned)
					require.Equal(t, returned.Sub(sent), end.trader.Sub(start.trader))
					// The expansion's whole offer was placed or burned and the draw burned against
					// output already minted: Market holds what it held before.
					require.Equal(t, start.market, end.market)
				})
			}
		}
	}

	t.Run("repeated cycles in one block", func(t *testing.T) {
		f := newCapacityFixture(t)
		s := redemptionScenario{
			rates:     capacityMarketRates,
			depth:     markettypes.DefaultBasePool.Amount,
			delta:     math.LegacyZeroDec(),
			liability: math.LegacyZeroDec(),
			buffer:    math.LegacyZeroDec(),
			spread:    markettypes.DefaultMinStabilitySpread,
			recovery:  markettypes.DefaultPoolRecoveryPeriod,
			complete:  true,
		}
		ctx, _ := f.open(t, s)
		sent := math.LegacyNewDecWithPrec(1, 1).Mul(s.depth).TruncateInt()
		apptestutil.FundAccount(t, f.app, ctx, f.trader, sdk.NewCoins(chain.NoahCoin(sent.MulRaw(100))))
		start := readCapacityState(t, f.app, ctx, f.trader)
		for cycle := range 100 {
			_, issued := capacitySell(t, f.app, ctx, f.trader, chain.NoahCoin(sent), chain.USDBaseDenom)
			_, returned := capacitySell(t, f.app, ctx, f.trader, sdk.NewCoin(chain.USDBaseDenom, issued), chain.NoahBaseDenom)
			require.Truef(t, returned.LT(sent), "cycle %d: sent %s, returned %s", cycle, sent, returned)
		}
		require.NoError(t, f.app.MarketKeeper.EndBlocker(ctx))
		end := readCapacityState(t, f.app, ctx, f.trader)
		require.True(t, end.trader.LT(start.trader))
		require.Equal(t, start.market, end.market)
	})
}

// requireRedemptionHolds checks one settled redemption against the capacity properties, each
// restated from the scenario and the state the block left.
func requireRedemptionHolds(t *testing.T, s redemptionScenario, r settledRedemption, label string) {
	t.Helper()

	require.Equalf(t, !s.complete, r.disclosed, "%s: the scenario's valuation is not what it set up", label)
	// The fill pays exactly what the quote said.
	require.Truef(t, r.quoted.Equal(r.output), "%s: quoted %s, paid %s", label, r.quoted, r.output)

	// The floor bounds the payout exactly: never more than the redeemed value less the floor.
	value := math.LegacyNewDecFromInt(r.offer).Mul(s.rates.stable)
	bound := value.MulTruncate(math.LegacyOneDec().Sub(s.spread)).TruncateInt()
	require.Truef(t, r.output.LTE(bound), "%s: paid %s past the floor's bound %s", label, r.output, bound)

	// The payout is the value less the larger of the floor and the pool's constant-product
	// shortfall, within rounding.
	x := new(big.Rat).Quo(
		new(big.Rat).Mul(capacityIntRat(r.offer), capacityDecRat(s.rates.stable)),
		capacityDecRat(s.rates.reference),
	)
	spread := capacityPoolSpread(capacityDecRat(s.depth), capacityDecRat(r.before.delta), x, false)
	if floor := capacityDecRat(s.spread); floor.Cmp(spread) > 0 {
		spread = floor
	}
	want := capacityFloor(new(big.Rat).Mul(capacityDecRat(value), new(big.Rat).Sub(big.NewRat(1, 1), spread)))
	tolerance := capacityTolerance(value, 2)
	require.Truef(t, r.output.Sub(want).Abs().LTE(tolerance),
		"%s: paid %s, the quote's closed form %s, tolerance %s", label, r.output, want, tolerance)

	// The pool absorbs the offer at its value in pool units, then EndBlock returns one part in
	// the recovery period of the gap.
	moved := r.before.delta.Add(math.LegacyNewDecFromInt(r.offer).Mul(s.rates.stable).Quo(s.rates.reference))
	require.Truef(t, moved.Equal(r.mid), "%s: the fill left delta %s, want %s", label, r.mid, moved)
	recovered := r.mid.Sub(r.mid.QuoInt64(int64(s.recovery)))
	require.Truef(t, recovered.Equal(r.after.delta), "%s: EndBlock left delta %s, want %s", label, r.after.delta, recovered)

	// Supply and the Buffer move by exactly what the block minted, drew, and burned. The draw is
	// output times custody over the liability outstanding when the redemption quoted, floored once.
	// A never-priced member is outside that liability.
	before := math.LegacyNewDecFromInt(r.before.stable).Mul(s.rates.stable)
	drawn := math.MinInt(r.output, math.LegacyNewDecFromInt(r.output.Mul(r.before.buffer)).Quo(before).TruncateInt())
	require.Truef(t, drawn.LTE(r.before.buffer), "%s: drew %s from a Buffer of %s", label, drawn, r.before.buffer)
	require.Equalf(t, r.offer, r.before.stable.Sub(r.after.stable), "%s: stable burned", label)
	require.Equalf(t, r.output, r.after.trader.Sub(r.before.trader), "%s: NOAH received", label)
	require.Equalf(t, r.before.buffer.Sub(drawn), r.after.buffer, "%s: Buffer after the draw", label)
	require.Equalf(t, r.output.Sub(drawn), r.after.noah.Sub(r.before.noah), "%s: NOAH supply change", label)
	require.Equalf(t, r.before.market, r.after.market, "%s: Market custody", label)

	// Coverage never falls: cross-multiplied, so nothing is divided.
	if s.complete {
		after := math.LegacyNewDecFromInt(r.after.stable).Mul(s.rates.stable)
		left := new(big.Int).Mul(r.after.buffer.BigInt(), before.BigInt())
		right := new(big.Int).Mul(r.before.buffer.BigInt(), after.BigInt())
		require.Truef(t, left.Cmp(right) >= 0, "%s: coverage fell from %s/%s to %s/%s",
			label, r.before.buffer, before, r.after.buffer, after)
	}
}

// capacityFixture holds one open block. Each scenario runs in its own branch of it, so scenarios
// never see each other's conversions.
type capacityFixture struct {
	app    *app.ArkApp
	block  sdk.Context
	trader sdk.AccAddress
}

func newCapacityFixture(t *testing.T) *capacityFixture {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	return &capacityFixture{app: arkApp, block: ctx, trader: treasuryGovernanceVoter(t, arkApp, ctx)}
}

// open branches the block and seeds a scenario into the branch, returning the stable supply.
func (f *capacityFixture) open(t *testing.T, s redemptionScenario) (sdk.Context, math.Int) {
	t.Helper()
	ctx, _ := f.block.CacheContext()
	return ctx, seedRedemptionScenario(t, f.app, ctx, f.trader, s)
}

// redeem sells offer stable for NOAH and settles the block.
func (f *capacityFixture) redeem(t *testing.T, ctx sdk.Context, offer math.Int) settledRedemption {
	t.Helper()
	r := settledRedemption{offer: offer, before: readCapacityState(t, f.app, ctx, f.trader)}
	r.quoted, r.output = capacitySell(t, f.app, ctx, f.trader, sdk.NewCoin(chain.USDBaseDenom, offer), chain.NoahBaseDenom)
	r.mid = readCapacityState(t, f.app, ctx, f.trader).delta

	events := len(ctx.EventManager().Events())
	require.NoError(t, f.app.MarketKeeper.EndBlocker(ctx))
	for _, event := range ctx.EventManager().Events()[events:] {
		r.disclosed = r.disclosed || event.Type == liabilityIncompleteEvent
	}
	r.after = readCapacityState(t, f.app, ctx, f.trader)
	return r
}

// redeemPieces sells each piece in turn, settles the block once, and returns the NOAH paid and the
// NOAH supply left minted after the Buffer's draw.
func (f *capacityFixture) redeemPieces(t *testing.T, ctx sdk.Context, pieces []math.Int) (math.Int, math.Int) {
	t.Helper()
	before := readCapacityState(t, f.app, ctx, f.trader)
	paid := math.ZeroInt()
	for _, piece := range pieces {
		_, output := capacitySell(t, f.app, ctx, f.trader, sdk.NewCoin(chain.USDBaseDenom, piece), chain.NoahBaseDenom)
		paid = paid.Add(output)
	}
	require.NoError(t, f.app.MarketKeeper.EndBlocker(ctx))
	after := readCapacityState(t, f.app, ctx, f.trader)
	return paid, after.noah.Sub(before.noah)
}

// capacityChain drives real blocks. Writes land through the incoming block's context before
// FinalizeBlock, so Market's EndBlocker settles them in that block; reads go through committed
// state.
type capacityChain struct {
	app    *app.ArkApp
	trader sdk.AccAddress
	rates  capacityRates
	time   time.Time
	t      *testing.T
	events []cmtabci.Event
}

func newCapacityChain(t *testing.T, rates capacityRates) *capacityChain {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	// Setup leaves its first block finalised but uncommitted.
	_, err := arkApp.Commit()
	require.NoError(t, err)
	c := &capacityChain{app: arkApp, rates: rates, time: time.Unix(1_800_000_000, 0), t: t}
	c.trader = treasuryGovernanceVoter(t, arkApp, c.committed())
	return c
}

// block finalises and commits the next block with tx applied at its start.
func (c *capacityChain) block(tx func(sdk.Context)) {
	c.t.Helper()
	c.time = c.time.Add(6 * time.Second)
	header := cmtproto.Header{Height: c.app.LastBlockHeight() + 1, Time: c.time}
	ctx := c.app.NewNextBlockContext(header)
	// Restamped every block, so no quote reads a stale price.
	setCapacityRates(c.t, c.app, ctx, c.rates)
	if tx != nil {
		tx(ctx)
	}
	res, err := c.app.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: header.Height, Time: header.Time})
	require.NoError(c.t, err)
	c.events = res.Events
	_, err = c.app.Commit()
	require.NoError(c.t, err)
}

func (c *capacityChain) committed() sdk.Context {
	return c.app.NewContextLegacy(true, cmtproto.Header{Height: c.app.LastBlockHeight(), Time: c.time})
}

func (c *capacityChain) state(t *testing.T) capacityState {
	t.Helper()
	return readCapacityState(t, c.app, c.committed(), c.trader)
}

// redeem sells offer stable for NOAH in its own block.
func (c *capacityChain) redeem(t *testing.T, offer math.Int) settledRedemption {
	t.Helper()
	r := settledRedemption{offer: offer, before: c.state(t)}
	c.block(func(ctx sdk.Context) {
		r.quoted, r.output = capacitySell(t, c.app, ctx, c.trader, sdk.NewCoin(chain.USDBaseDenom, offer), chain.NoahBaseDenom)
		r.mid = readCapacityState(t, c.app, ctx, c.trader).delta
	})
	for _, event := range c.events {
		r.disclosed = r.disclosed || event.Type == liabilityIncompleteEvent
	}
	r.after = c.state(t)
	return r
}

// seedRedemptionScenario writes a scenario's prices, pool, stable supply, and Buffer, and returns
// the stable supply.
func seedRedemptionScenario(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, trader sdk.AccAddress, s redemptionScenario) math.Int {
	t.Helper()
	setCapacityRates(t, arkApp, ctx, s.rates)

	policy := markettypes.DefaultConversionPolicy()
	policy.BasePool = sdk.NewDecCoinFromDec(chain.XDRBaseDenom, s.depth)
	policy.MinStabilitySpread = s.spread
	policy.PoolRecoveryPeriod = s.recovery
	require.NoError(t, policy.Validate())
	require.NoError(t, arkApp.MarketKeeper.ConversionPolicy.Set(ctx, policy))
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, s.depth.Mul(s.delta)))

	supply := s.liability.Mul(s.depth).TruncateInt()
	if supply.IsPositive() {
		apptestutil.FundAccount(t, arkApp, ctx, trader, sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, supply)))
	}
	if !s.complete {
		// A listed stable never priced: outstanding and disclosed, but outside the liability the
		// Buffer is measured against.
		apptestutil.FundAccount(t, arkApp, ctx, trader, sdk.NewCoins(sdk.NewCoin(chain.KRWBaseDenom, supply.QuoRaw(10).AddRaw(1))))
	}
	if buffer := s.buffer.MulInt(supply).Mul(s.rates.stable).TruncateInt(); buffer.IsPositive() {
		apptestutil.FundModule(t, arkApp, ctx, treasurytypes.RedemptionBufferName, sdk.NewCoins(chain.NoahCoin(buffer)))
	}
	return supply
}

func setCapacityRates(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, rates capacityRates) {
	t.Helper()
	for denom, rate := range map[string]math.LegacyDec{chain.XDRBaseDenom: rates.reference, chain.USDBaseDenom: rates.stable} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:       denom,
			Rate:        rate,
			BlockTime:   uint64(ctx.BlockTime().Unix()),
			BlockHeight: uint64(ctx.BlockHeight()),
		}))
	}
}

// capacitySell quotes a sell, fills it, and returns both figures.
func capacitySell(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, trader sdk.AccAddress, offer sdk.Coin, askDenom string) (math.Int, math.Int) {
	t.Helper()
	quote, err := marketkeeper.NewQueryServerImpl(arkApp.MarketKeeper).Sell(ctx, &markettypes.QuerySellRequest{
		OfferCoin: offer.String(),
		AskDenom:  askDenom,
	})
	require.NoError(t, err)
	response, err := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper).Sell(ctx, &markettypes.MsgSell{
		Trader:    trader.String(),
		OfferCoin: offer,
		AskDenom:  askDenom,
	})
	require.NoError(t, err)
	require.Equal(t, askDenom, response.SwapCoin.Denom)
	return quote.SwapCoin.Amount, response.SwapCoin.Amount
}

func readCapacityState(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, trader sdk.AccAddress) capacityState {
	t.Helper()
	delta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)
	noahOf := func(addr sdk.AccAddress) math.Int {
		return arkApp.BankKeeper.GetBalance(ctx, addr, chain.NoahBaseDenom).Amount
	}
	return capacityState{
		buffer: noahOf(authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)),
		market: noahOf(authtypes.NewModuleAddress(markettypes.ModuleName)),
		trader: noahOf(trader),
		noah:   arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount,
		stable: arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
		delta:  delta,
	}
}

// capacityPoolSpread is the exact constant-product shortfall of x pool units against a
// frictionless fill, as a share of x; a fill that beats frictionless charges none.
func capacityPoolSpread(depth, delta, x *big.Rat, offeringNoah bool) *big.Rat {
	k := new(big.Rat).Mul(depth, depth)
	ark := new(big.Rat).Add(depth, delta)
	noah := new(big.Rat).Quo(k, ark)
	in, out := ark, noah
	if offeringNoah {
		in, out = noah, ark
	}
	leaving := new(big.Rat).Sub(out, new(big.Rat).Quo(k, new(big.Rat).Add(in, x)))
	if leaving.Cmp(x) >= 0 {
		return new(big.Rat)
	}
	shortfall := new(big.Rat).Sub(x, leaving)
	return shortfall.Quo(shortfall, x)
}

// capacityTolerance bounds what 18-decimal rounding can move a whole-unit figure computed from
// value: one quantum of relative error, plus a base unit per truncation.
func capacityTolerance(value math.LegacyDec, truncations int64) math.Int {
	return value.TruncateInt().Quo(math.NewIntWithDecimal(1, math.LegacyPrecision)).AddRaw(truncations)
}

// capacityShare is a share of amount, at least one base unit.
func capacityShare(share math.LegacyDec, amount math.Int) math.Int {
	return math.MaxInt(math.OneInt(), share.MulInt(amount).TruncateInt())
}

// capacityPieces splits total into count near-equal positive pieces.
func capacityPieces(total math.Int, count int) []math.Int {
	quotient, remainder := total.QuoRaw(int64(count)), total.ModRaw(int64(count)).Int64()
	pieces := make([]math.Int, 0, count)
	for i := range count {
		piece := quotient
		if int64(i) < remainder {
			piece = piece.AddRaw(1)
		}
		if piece.IsPositive() {
			pieces = append(pieces, piece)
		}
	}
	return pieces
}

func capacityDecs(values ...string) []math.LegacyDec {
	decs := make([]math.LegacyDec, len(values))
	for i, value := range values {
		decs[i] = math.LegacyMustNewDecFromStr(value)
	}
	return decs
}

// capacityPPM draws a share in parts per million from [low, high].
func capacityPPM(rng *rand.Rand, low, high int64) math.LegacyDec {
	return math.LegacyNewDecWithPrec(low+rng.Int63n(high-low+1), 6)
}

// capacityRandomRate draws a rate with eighteen random decimals from [0.05, 10].
func capacityRandomRate(rng *rand.Rand) math.LegacyDec {
	raw := new(big.Int).Mul(big.NewInt(50_000_000+rng.Int63n(9_950_000_001)), big.NewInt(1_000_000_000))
	raw.Add(raw, big.NewInt(rng.Int63n(1_000_000_000)))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}

// capacityRandomDepth draws a depth of 10^15 to 10^26 base units with a fractional part.
func capacityRandomDepth(rng *rand.Rand) math.LegacyDec {
	mantissa := math.NewInt(100_000_000 + rng.Int63n(900_000_000))
	whole := mantissa.Mul(math.NewIntWithDecimal(1, 7+rng.Intn(11)))
	return math.LegacyNewDecFromInt(whole).Add(math.LegacyNewDecWithPrec(rng.Int63n(1_000_000_000_000_000_000), math.LegacyPrecision))
}

var capacityQuantum = new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)

func capacityDecRat(d math.LegacyDec) *big.Rat {
	return new(big.Rat).SetFrac(d.BigInt(), capacityQuantum)
}

func capacityIntRat(i math.Int) *big.Rat {
	return new(big.Rat).SetInt(i.BigInt())
}

// capacityRatDec truncates a non-negative rational to eighteen decimals.
func capacityRatDec(r *big.Rat) math.LegacyDec {
	raw := new(big.Int).Mul(r.Num(), capacityQuantum)
	return math.LegacyNewDecFromBigIntWithPrec(raw.Quo(raw, r.Denom()), math.LegacyPrecision)
}

// capacityFloor floors a non-negative rational.
func capacityFloor(r *big.Rat) math.Int {
	return math.NewIntFromBigInt(new(big.Int).Quo(r.Num(), r.Denom()))
}
