package keeper_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/decimal"
	markettypes "ark/x/market/types"
)

const phase3ABasePool = int64(1_000_000_000_000)

type phase3AState struct {
	basePool          math.LegacyDec
	delta             math.LegacyDec
	stableSupply      math.Int
	buffer            math.Int
	noahSupply        math.Int
	minSpread         math.LegacyDec
	recoveryPeriod    uint64
	baseRate          math.LegacyDec
	valuationComplete bool
}

type phase3ARedemption struct {
	output            math.Int
	bufferPaid        math.Int
	residualMint      math.Int
	redeemedLiability math.LegacyDec
	liabilityBefore   math.LegacyDec
	liabilityAfter    math.LegacyDec
	bufferBefore      math.Int
	bufferAfter       math.Int
	valuationComplete bool
	rawSpread         math.LegacyDec
	appliedSpread     math.LegacyDec
	deltaBefore       math.LegacyDec
	deltaAfter        math.LegacyDec
}

type phase3ATotals struct {
	output       math.Int
	bufferPaid   math.Int
	residualMint math.Int
	endingDelta  math.LegacyDec
}

func TestPhase3ACapacityEnvelope(t *testing.T) {
	deltaRatios := []string{"-0.99", "-0.9", "-0.5", "0", "0.5", "0.9"}
	liabilityRatios := []string{"0.1", "1", "10"}
	bufferRatios := []string{"0", "0.25", "0.5", "1"}
	offerRatios := []string{"0.000001", "0.0001", "0.01", "0.1", "0.25", "0.5", "0.75", "1"}

	type maximum struct {
		residual       math.Int
		deltaRatio     string
		liabilityRatio string
		bufferRatio    string
		offerRatio     string
		complete       bool
		output         math.Int
	}
	maxResult := maximum{residual: math.ZeroInt()}
	cases := 0

	for _, deltaRatio := range deltaRatios {
		for _, liabilityRatio := range liabilityRatios {
			for _, bufferRatio := range bufferRatios {
				for _, complete := range []bool{true, false} {
					for _, offerRatio := range offerRatios {
						state := newPhase3AState(
							deltaRatio,
							liabilityRatio,
							bufferRatio,
							"0.02",
							"1",
							markettypes.DefaultPoolRecoveryPeriod,
							complete,
						)
						offer := phase3AFractionOfInt(state.stableSupply, offerRatio)
						result, err := state.redeem(offer)
						require.NoError(t, err)
						assertPhase3ARedemptionConservation(t, state, offer, result)
						assertPhase3AOracleValueBound(t, result, state.minSpread)
						cases++

						if result.residualMint.GT(maxResult.residual) {
							maxResult = maximum{
								residual:       result.residualMint,
								deltaRatio:     deltaRatio,
								liabilityRatio: liabilityRatio,
								bufferRatio:    bufferRatio,
								offerRatio:     offerRatio,
								complete:       complete,
								output:         result.output,
							}
						}
					}
				}
			}
		}
	}

	base := math.LegacyNewDec(phase3ABasePool)
	residualToBase, err := decimal.Quo(math.LegacyNewDecFromInt(maxResult.residual), base)
	require.NoError(t, err)
	t.Logf(
		"capacity sweep: cases=%d max_residual=%s residual/base_pool=%s output=%s delta/base=%s liability/base=%s buffer/liability=%s offer/liability=%s valuation_complete=%t",
		cases,
		maxResult.residual,
		residualToBase,
		maxResult.output,
		maxResult.deltaRatio,
		maxResult.liabilityRatio,
		maxResult.bufferRatio,
		maxResult.offerRatio,
		maxResult.complete,
	)
}

func TestPhase3ACapacityPropertiesRandomised(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 5_000; i++ {
		deltaBasisPoints := int64(rng.Intn(19_901) - 9_900)   // [-99%, 100%]
		liabilityBasisPoints := int64(rng.Intn(99_901) + 100) // [1%, 1,000%]
		bufferBasisPoints := int64(rng.Intn(10_001))
		offerBasisPoints := int64(rng.Intn(10_000) + 1)
		spreadBasisPoints := int64(rng.Intn(1_001)) // [0%, 10%]

		state := newPhase3AStateFromRatios(
			phase3ARatio(deltaBasisPoints, 10_000),
			phase3ARatio(liabilityBasisPoints, 10_000),
			phase3ARatio(bufferBasisPoints, 10_000),
			phase3ARatio(spreadBasisPoints, 10_000),
			math.LegacyOneDec(),
			markettypes.DefaultPoolRecoveryPeriod,
			i%5 != 0,
		)
		offer := phase3AFractionOfIntByDec(state.stableSupply, phase3ARatio(offerBasisPoints, 10_000))
		result, err := state.redeem(offer)
		require.NoError(t, err, "case %d", i)
		assertPhase3ARedemptionConservation(t, state, offer, result)
		assertPhase3AOracleValueBound(t, result, state.minSpread)
	}
}

func TestPhase3ASameBlockSplitting(t *testing.T) {
	deltaRatios := []string{"-0.99", "-0.9", "-0.5", "0", "0.5", "0.9"}
	liabilityRatios := []string{"0.1", "1", "10"}
	bufferRatios := []string{"0", "0.25", "0.5", "1"}
	offerRatios := []string{"0.01", "0.1", "0.25", "0.5", "0.75", "1"}
	splitCounts := []int{10, 100, 1_000}

	for _, deltaRatio := range deltaRatios {
		for _, liabilityRatio := range liabilityRatios {
			for _, bufferRatio := range bufferRatios {
				for _, offerRatio := range offerRatios {
					initial := newPhase3AState(
						deltaRatio,
						liabilityRatio,
						bufferRatio,
						"0.02",
						"1",
						markettypes.DefaultPoolRecoveryPeriod,
						true,
					)
					totalOffer := phase3AFractionOfInt(initial.stableSupply, offerRatio)
					unsplit, err := phase3ARunSameBlock(initial, totalOffer, 1)
					require.NoError(t, err)

					for _, splitCount := range splitCounts {
						split, err := phase3ARunSameBlock(initial, totalOffer, splitCount)
						require.NoError(t, err)
						require.True(
							t,
							split.output.LTE(unsplit.output),
							"same-block split increased output: delta=%s liability=%s buffer=%s offer=%s count=%d split=%s unsplit=%s",
							deltaRatio,
							liabilityRatio,
							bufferRatio,
							offerRatio,
							splitCount,
							split.output,
							unsplit.output,
						)

						residualExcess := split.residualMint.Sub(unsplit.residualMint)
						require.False(
							t,
							residualExcess.IsPositive(),
							"same-block split increased residual mint: delta=%s liability=%s buffer=%s offer=%s count=%d split=%s unsplit=%s",
							deltaRatio,
							liabilityRatio,
							bufferRatio,
							offerRatio,
							splitCount,
							split.residualMint,
							unsplit.residualMint,
						)
					}
				}
			}
		}
	}

	t.Log("same-block split sweep: no split increased recipient output or residual mint")
}

func TestPhase3ARecoverySchedules(t *testing.T) {
	initial := newPhase3AState(
		"0",
		"1",
		"0",
		"0.02",
		"1",
		markettypes.DefaultPoolRecoveryPeriod,
		true,
	)
	totalOffer := phase3AFractionOfInt(initial.stableSupply, "0.5")
	schedules := []int{
		1,
		int(initial.recoveryPeriod / 4),
		int(initial.recoveryPeriod),
		int(initial.recoveryPeriod * 2),
	}

	previousOutput := math.ZeroInt()
	for _, blocks := range schedules {
		totals, err := phase3ARunAcrossBlocks(initial, totalOffer, blocks)
		require.NoError(t, err)
		require.True(
			t,
			totals.output.GTE(previousOutput),
			"spreading the same redemption over more recovery blocks reduced total output: blocks=%d output=%s previous=%s",
			blocks,
			totals.output,
			previousOutput,
		)
		previousOutput = totals.output

		outputToOffer, err := decimal.Quo(
			math.LegacyNewDecFromInt(totals.output),
			math.LegacyNewDecFromInt(totalOffer),
		)
		require.NoError(t, err)
		t.Logf(
			"recovery schedule: blocks=%d output=%s residual_mint=%s output/redeemed=%s ending_delta=%s",
			blocks,
			totals.output,
			totals.residualMint,
			outputToOffer,
			totals.endingDelta,
		)
	}
}

func TestPhase3ARecoveryFundingMix(t *testing.T) {
	initial := newPhase3AState(
		"-0.5",
		"10",
		"0.25",
		"0.02",
		"1",
		markettypes.DefaultPoolRecoveryPeriod,
		true,
	)
	totalOffer := initial.stableSupply
	maximumOutput := phase3AFractionOfInt(totalOffer, "0.98")
	for _, blocks := range []int{1, 10, 3_600, int(initial.recoveryPeriod)} {
		totals, err := phase3ARunAcrossBlocks(initial, totalOffer, blocks)
		require.NoError(t, err)
		require.Equal(t, totals.output, totals.bufferPaid.Add(totals.residualMint))
		require.True(t, totals.output.LTE(maximumOutput))
		t.Logf(
			"recovery funding mix: blocks=%d output=%s buffer_paid=%s residual_mint=%s ending_delta=%s",
			blocks,
			totals.output,
			totals.bufferPaid,
			totals.residualMint,
			totals.endingDelta,
		)
	}
}

func TestPhase3AParameterSensitivity(t *testing.T) {
	spreads := []string{"0.01", "0.02", "0.05"}
	recoveryPeriods := []uint64{
		markettypes.DefaultPoolRecoveryPeriod / 2,
		markettypes.DefaultPoolRecoveryPeriod,
		markettypes.DefaultPoolRecoveryPeriod * 2,
	}
	rates := []string{"0.5", "1", "2"}

	for _, spread := range spreads {
		for _, recoveryPeriod := range recoveryPeriods {
			for _, rate := range rates {
				initial := newPhase3AState(
					"0",
					"1",
					"0",
					spread,
					rate,
					recoveryPeriod,
					true,
				)
				totalOffer := phase3AFractionOfInt(initial.stableSupply, "0.5")
				totals, err := phase3ARunAcrossBlocks(initial, totalOffer, int(recoveryPeriod))
				require.NoError(t, err)
				t.Logf(
					"sensitivity: spread=%s recovery=%d rate=%s output=%s residual_mint=%s ending_delta=%s",
					spread,
					recoveryPeriod,
					rate,
					totals.output,
					totals.residualMint,
					totals.endingDelta,
				)
			}
		}
	}
}

func TestPhase3ABasePoolScaling(t *testing.T) {
	basePools := []int64{500_000_000_000, 1_000_000_000_000, 2_000_000_000_000}
	for _, basePool := range basePools {
		state := newPhase3AState(
			"0",
			"1",
			"0",
			"0.02",
			"1",
			markettypes.DefaultPoolRecoveryPeriod,
			true,
		)
		state.basePool = math.LegacyNewDec(basePool)
		state.delta = math.LegacyZeroDec()
		state.stableSupply = math.NewInt(basePool)
		offer := state.stableSupply.QuoRaw(2)
		result, err := state.redeem(offer)
		require.NoError(t, err)
		ratio, err := decimal.Quo(
			math.LegacyNewDecFromInt(result.output),
			math.LegacyNewDec(basePool),
		)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(basePool).QuoRaw(3), result.output)
		t.Logf("base-pool scaling: base_pool=%d output=%s output/base_pool=%s", basePool, result.output, ratio)
	}
}

func TestPhase3AExpansionRedemptionCycles(t *testing.T) {
	offerRatios := []string{"0.01", "0.1", "0.5", "1", "10", "100"}
	for _, offerRatio := range offerRatios {
		state := newPhase3AState(
			"0",
			"0.000001",
			"0",
			"0.02",
			"1",
			markettypes.DefaultPoolRecoveryPeriod,
			true,
		)
		state.stableSupply = math.ZeroInt()
		noahOffer := phase3AFractionOfInt(math.NewInt(phase3ABasePool), offerRatio)
		expansion, err := state.expand(noahOffer)
		require.NoError(t, err)
		require.True(t, expansion.IsPositive())
		deltaAfterExpansion := state.delta

		redemption, err := state.redeem(expansion)
		require.NoError(t, err)
		require.True(
			t,
			redemption.output.LT(noahOffer),
			"fixed-price expansion/redemption cycle returned at least its NOAH input: offer=%s stable=%s output=%s",
			noahOffer,
			expansion,
			redemption.output,
		)

		outputToOffer, err := decimal.Quo(
			math.LegacyNewDecFromInt(redemption.output),
			math.LegacyNewDecFromInt(noahOffer),
		)
		require.NoError(t, err)
		t.Logf(
			"cycle: noah_offer/base=%s stable_output=%s delta_after_expansion=%s redemption_output=%s output/offer=%s",
			offerRatio,
			expansion,
			deltaAfterExpansion,
			redemption.output,
			outputToOffer,
		)
	}
}

func TestPhase3AExpansionRecoveryRedemptionCycles(t *testing.T) {
	offerRatios := []string{"0.01", "0.1", "1", "10"}
	recoveryBlocks := []int{
		0,
		int(markettypes.DefaultPoolRecoveryPeriod / 4),
		int(markettypes.DefaultPoolRecoveryPeriod),
		int(markettypes.DefaultPoolRecoveryPeriod * 2),
	}

	for _, offerRatio := range offerRatios {
		for _, blocks := range recoveryBlocks {
			state := newPhase3AState(
				"0",
				"0.000001",
				"0",
				"0.02",
				"1",
				markettypes.DefaultPoolRecoveryPeriod,
				true,
			)
			state.stableSupply = math.ZeroInt()
			noahOffer := phase3AFractionOfInt(math.NewInt(phase3ABasePool), offerRatio)
			stableOutput, err := state.expand(noahOffer)
			require.NoError(t, err)
			for block := 0; block < blocks; block++ {
				require.NoError(t, state.recover())
			}

			redemption, err := state.redeem(stableOutput)
			require.NoError(t, err)
			require.True(
				t,
				redemption.output.LT(noahOffer),
				"recovery made a fixed-price cycle profitable: offer/base=%s blocks=%d offer=%s stable=%s output=%s",
				offerRatio,
				blocks,
				noahOffer,
				stableOutput,
				redemption.output,
			)
		}
	}

	state := newPhase3AState(
		"0",
		"0.000001",
		"0",
		"0.02",
		"1",
		markettypes.DefaultPoolRecoveryPeriod,
		true,
	)
	state.stableSupply = math.ZeroInt()
	cycleOffer := phase3AFractionOfInt(math.NewInt(phase3ABasePool), "0.1")
	totalOffer := math.ZeroInt()
	totalOutput := math.ZeroInt()
	for i := 0; i < 100; i++ {
		stableOutput, err := state.expand(cycleOffer)
		require.NoError(t, err)
		redemption, err := state.redeem(stableOutput)
		require.NoError(t, err)
		totalOffer = totalOffer.Add(cycleOffer)
		totalOutput = totalOutput.Add(redemption.output)
	}
	require.True(t, totalOutput.LT(totalOffer))
	t.Logf(
		"repeated cycles: count=100 total_noah_offer=%s total_redemption_output=%s ending_delta=%s",
		totalOffer,
		totalOutput,
		state.delta,
	)
}

func newPhase3AState(
	deltaRatio string,
	liabilityRatio string,
	bufferRatio string,
	minSpread string,
	baseRate string,
	recoveryPeriod uint64,
	valuationComplete bool,
) phase3AState {
	return newPhase3AStateFromRatios(
		phase3AMustDec(deltaRatio),
		phase3AMustDec(liabilityRatio),
		phase3AMustDec(bufferRatio),
		phase3AMustDec(minSpread),
		phase3AMustDec(baseRate),
		recoveryPeriod,
		valuationComplete,
	)
}

func newPhase3AStateFromRatios(
	deltaRatio math.LegacyDec,
	liabilityRatio math.LegacyDec,
	bufferRatio math.LegacyDec,
	minSpread math.LegacyDec,
	baseRate math.LegacyDec,
	recoveryPeriod uint64,
	valuationComplete bool,
) phase3AState {
	basePool := math.LegacyNewDec(phase3ABasePool)
	delta, err := decimal.Mul(basePool, deltaRatio)
	if err != nil {
		panic(err)
	}
	stableSupply := liabilityRatio.Mul(basePool).TruncateInt()
	liabilityNoah, err := decimal.Quo(math.LegacyNewDecFromInt(stableSupply), baseRate)
	if err != nil {
		panic(err)
	}
	buffer := bufferRatio.Mul(liabilityNoah).TruncateInt()

	return phase3AState{
		basePool:          basePool,
		delta:             delta,
		stableSupply:      stableSupply,
		buffer:            buffer,
		noahSupply:        math.ZeroInt(),
		minSpread:         minSpread,
		recoveryPeriod:    recoveryPeriod,
		baseRate:          baseRate,
		valuationComplete: valuationComplete,
	}
}

func (s *phase3AState) redeem(offer math.Int) (phase3ARedemption, error) {
	if !offer.IsPositive() {
		return phase3ARedemption{}, fmt.Errorf("redemption offer must be positive: %s", offer)
	}
	if offer.GT(s.stableSupply) {
		return phase3ARedemption{}, fmt.Errorf("redemption offer %s exceeds stable supply %s", offer, s.stableSupply)
	}

	pools, err := markettypes.NewEffectivePools(s.basePool, s.delta)
	if err != nil {
		return phase3ARedemption{}, err
	}
	baseOffer := math.LegacyNewDecFromInt(offer)
	updatedOfferPool, err := decimal.Add(pools.ArkPool, baseOffer)
	if err != nil {
		return phase3ARedemption{}, err
	}
	remainingAskPool, err := decimal.Quo(pools.ConstantProduct, updatedOfferPool)
	if err != nil {
		return phase3ARedemption{}, err
	}
	askBaseAmount, err := decimal.Sub(pools.NoahPool, remainingAskPool)
	if err != nil {
		return phase3ARedemption{}, err
	}
	spreadAmount, err := decimal.Sub(baseOffer, askBaseAmount)
	if err != nil {
		return phase3ARedemption{}, err
	}
	rawSpread, err := decimal.Quo(spreadAmount, baseOffer)
	if err != nil {
		return phase3ARedemption{}, err
	}
	if rawSpread.GT(math.LegacyOneDec()) {
		return phase3ARedemption{}, fmt.Errorf("raw spread exceeds one: %s", rawSpread)
	}
	appliedSpread := rawSpread
	if appliedSpread.LT(s.minSpread) {
		appliedSpread = s.minSpread
	}

	directNoah, err := decimal.Quo(baseOffer, s.baseRate)
	if err != nil {
		return phase3ARedemption{}, err
	}
	fee, err := decimal.Mul(directNoah, appliedSpread)
	if err != nil {
		return phase3ARedemption{}, err
	}
	netNoah, err := decimal.Sub(directNoah, fee)
	if err != nil {
		return phase3ARedemption{}, err
	}
	output := netNoah.TruncateInt()
	if !output.IsPositive() {
		return phase3ARedemption{}, fmt.Errorf("redemption output rounded to zero")
	}

	redeemedLiability, err := decimal.Quo(baseOffer, s.baseRate)
	if err != nil {
		return phase3ARedemption{}, err
	}
	bufferPaid := math.ZeroInt()
	liabilityBefore := math.LegacyZeroDec()
	bufferBefore := s.buffer
	if s.valuationComplete {
		liabilityBefore, err = decimal.Quo(math.LegacyNewDecFromInt(s.stableSupply), s.baseRate)
		if err != nil {
			return phase3ARedemption{}, err
		}
		coverage, err := decimal.Quo(math.LegacyNewDecFromInt(s.buffer), liabilityBefore)
		if err != nil {
			return phase3ARedemption{}, err
		}
		if coverage.GT(math.LegacyOneDec()) {
			coverage = math.LegacyOneDec()
		}
		coveredOutput, err := decimal.Mul(math.LegacyNewDecFromInt(output), coverage)
		if err != nil {
			return phase3ARedemption{}, err
		}
		bufferPaid = coveredOutput.TruncateInt()
	}
	residualMint := output.Sub(bufferPaid)

	deltaBefore := s.delta
	s.delta, err = decimal.Add(s.delta, baseOffer)
	if err != nil {
		return phase3ARedemption{}, err
	}
	if _, err := markettypes.NewEffectivePools(s.basePool, s.delta); err != nil {
		return phase3ARedemption{}, err
	}
	s.stableSupply = s.stableSupply.Sub(offer)
	s.buffer = s.buffer.Sub(bufferPaid)
	s.noahSupply = s.noahSupply.Add(residualMint)
	liabilityAfter := math.LegacyZeroDec()
	if s.valuationComplete {
		liabilityAfter, err = decimal.Quo(math.LegacyNewDecFromInt(s.stableSupply), s.baseRate)
		if err != nil {
			return phase3ARedemption{}, err
		}
	}

	return phase3ARedemption{
		output:            output,
		bufferPaid:        bufferPaid,
		residualMint:      residualMint,
		redeemedLiability: redeemedLiability,
		liabilityBefore:   liabilityBefore,
		liabilityAfter:    liabilityAfter,
		bufferBefore:      bufferBefore,
		bufferAfter:       s.buffer,
		valuationComplete: s.valuationComplete,
		rawSpread:         rawSpread,
		appliedSpread:     appliedSpread,
		deltaBefore:       deltaBefore,
		deltaAfter:        s.delta,
	}, nil
}

func (s *phase3AState) expand(noahOffer math.Int) (math.Int, error) {
	if !noahOffer.IsPositive() {
		return math.Int{}, fmt.Errorf("expansion offer must be positive: %s", noahOffer)
	}
	pools, err := markettypes.NewEffectivePools(s.basePool, s.delta)
	if err != nil {
		return math.Int{}, err
	}
	baseOffer, err := decimal.Mul(math.LegacyNewDecFromInt(noahOffer), s.baseRate)
	if err != nil {
		return math.Int{}, err
	}
	updatedOfferPool, err := decimal.Add(pools.NoahPool, baseOffer)
	if err != nil {
		return math.Int{}, err
	}
	remainingAskPool, err := decimal.Quo(pools.ConstantProduct, updatedOfferPool)
	if err != nil {
		return math.Int{}, err
	}
	askBaseAmount, err := decimal.Sub(pools.ArkPool, remainingAskPool)
	if err != nil {
		return math.Int{}, err
	}
	spreadAmount, err := decimal.Sub(baseOffer, askBaseAmount)
	if err != nil {
		return math.Int{}, err
	}
	rawSpread, err := decimal.Quo(spreadAmount, baseOffer)
	if err != nil {
		return math.Int{}, err
	}
	if rawSpread.GT(math.LegacyOneDec()) {
		return math.Int{}, fmt.Errorf("raw expansion spread exceeds one: %s", rawSpread)
	}
	appliedSpread := rawSpread
	if appliedSpread.LT(s.minSpread) {
		appliedSpread = s.minSpread
	}
	fee, err := decimal.Mul(baseOffer, appliedSpread)
	if err != nil {
		return math.Int{}, err
	}
	netStable, err := decimal.Sub(baseOffer, fee)
	if err != nil {
		return math.Int{}, err
	}
	stableOutput := netStable.TruncateInt()
	if !stableOutput.IsPositive() {
		return math.Int{}, fmt.Errorf("expansion output rounded to zero")
	}
	s.delta, err = decimal.Sub(s.delta, netStable)
	if err != nil {
		return math.Int{}, err
	}
	if _, err := markettypes.NewEffectivePools(s.basePool, s.delta); err != nil {
		return math.Int{}, err
	}
	s.stableSupply = s.stableSupply.Add(stableOutput)
	return stableOutput, nil
}

func (s *phase3AState) recover() error {
	if s.delta.IsZero() {
		return nil
	}
	regression := s.delta.QuoInt(math.NewIntFromUint64(s.recoveryPeriod))
	if regression.IsZero() {
		return nil
	}
	var err error
	s.delta, err = decimal.Sub(s.delta, regression)
	if err != nil {
		return err
	}
	_, err = markettypes.NewEffectivePools(s.basePool, s.delta)
	return err
}

func phase3ARunSameBlock(initial phase3AState, totalOffer math.Int, count int) (phase3ATotals, error) {
	state := initial
	totals := phase3ATotals{
		output:       math.ZeroInt(),
		bufferPaid:   math.ZeroInt(),
		residualMint: math.ZeroInt(),
	}
	quotient := totalOffer.QuoRaw(int64(count))
	remainder := totalOffer.ModRaw(int64(count)).Int64()
	for i := 0; i < count; i++ {
		offer := quotient
		if int64(i) < remainder {
			offer = offer.AddRaw(1)
		}
		result, err := state.redeem(offer)
		if err != nil {
			return phase3ATotals{}, err
		}
		totals.output = totals.output.Add(result.output)
		totals.bufferPaid = totals.bufferPaid.Add(result.bufferPaid)
		totals.residualMint = totals.residualMint.Add(result.residualMint)
	}
	totals.endingDelta = state.delta
	return totals, nil
}

func phase3ARunAcrossBlocks(initial phase3AState, totalOffer math.Int, blocks int) (phase3ATotals, error) {
	state := initial
	totals := phase3ATotals{
		output:       math.ZeroInt(),
		bufferPaid:   math.ZeroInt(),
		residualMint: math.ZeroInt(),
	}
	quotient := totalOffer.QuoRaw(int64(blocks))
	remainder := totalOffer.ModRaw(int64(blocks)).Int64()
	for block := 0; block < blocks; block++ {
		offer := quotient
		if int64(block) < remainder {
			offer = offer.AddRaw(1)
		}
		if offer.IsPositive() {
			result, err := state.redeem(offer)
			if err != nil {
				return phase3ATotals{}, err
			}
			totals.output = totals.output.Add(result.output)
			totals.bufferPaid = totals.bufferPaid.Add(result.bufferPaid)
			totals.residualMint = totals.residualMint.Add(result.residualMint)
		}
		if err := state.recover(); err != nil {
			return phase3ATotals{}, err
		}
	}
	totals.endingDelta = state.delta
	return totals, nil
}

func assertPhase3ARedemptionConservation(
	t *testing.T,
	stateAfter phase3AState,
	offer math.Int,
	result phase3ARedemption,
) {
	t.Helper()
	require.Equal(t, result.output, result.bufferPaid.Add(result.residualMint))
	require.True(t, result.deltaAfter.Equal(stateAfter.delta))
	require.True(t, result.deltaBefore.Add(math.LegacyNewDecFromInt(offer)).Equal(result.deltaAfter))
	require.False(t, stateAfter.stableSupply.IsNegative())
	require.False(t, stateAfter.buffer.IsNegative())
	if result.valuationComplete && result.liabilityAfter.IsPositive() {
		left, err := decimal.Mul(math.LegacyNewDecFromInt(result.bufferAfter), result.liabilityBefore)
		require.NoError(t, err)
		right, err := decimal.Mul(math.LegacyNewDecFromInt(result.bufferBefore), result.liabilityAfter)
		require.NoError(t, err)
		require.True(
			t,
			left.GTE(right),
			"Buffer coverage decreased: before=%s/%s after=%s/%s",
			result.bufferBefore,
			result.liabilityBefore,
			result.bufferAfter,
			result.liabilityAfter,
		)
	}
}

func assertPhase3AOracleValueBound(t *testing.T, result phase3ARedemption, minSpread math.LegacyDec) {
	t.Helper()
	maximumOutput := result.redeemedLiability.Sub(minSpread.Mul(result.redeemedLiability)).TruncateInt()
	require.True(
		t,
		result.output.LTE(maximumOutput),
		"redemption output %s exceeds minimum-spread oracle-value bound %s (raw spread %s, applied %s)",
		result.output,
		maximumOutput,
		result.rawSpread,
		result.appliedSpread,
	)
}

func phase3AFractionOfInt(value math.Int, fraction string) math.Int {
	return phase3AFractionOfIntByDec(value, phase3AMustDec(fraction))
}

func phase3AFractionOfIntByDec(value math.Int, fraction math.LegacyDec) math.Int {
	result := fraction.MulInt(value).TruncateInt()
	if !result.IsPositive() {
		return math.OneInt()
	}
	return result
}

func phase3ARatio(numerator, denominator int64) math.LegacyDec {
	return math.LegacyNewDec(numerator).QuoInt64(denominator)
}

func phase3AMustDec(value string) math.LegacyDec {
	return math.LegacyMustNewDecFromStr(value)
}
