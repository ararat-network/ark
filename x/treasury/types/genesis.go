package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

// NewGenesisState creates a Treasury genesis state.
func NewGenesisState(
	params Params,
	conversionFactors []ConversionFactor,
	rewardFunding RewardFundingState,
	monetaryMandate MonetaryMandate,
	monetaryPolicy MonetaryPolicy,
	exposureState ExposureState,
	exposureUpdatePending bool,
	baseGasPrice math.LegacyDec,
) *GenesisState {
	return &GenesisState{
		Params:                 params,
		ConversionFactors:      append([]ConversionFactor(nil), conversionFactors...),
		RewardFunding:          rewardFunding,
		MonetaryMandate:        monetaryMandate,
		MonetaryPolicy:         monetaryPolicy,
		ExposureState:          exposureState,
		ExposureRefreshPending: exposureUpdatePending,
		BaseGasPrice:           baseGasPrice,
	}
}

// defaultUSDPerXDR converts the dollar-stated placeholder into the table's
// unit. The IMF cross was 1.3709 on 2026-09-02, rounded up because the factor
// sizes a fee requirement. XDR drifts a percent or two a year against the
// dollar, and the seed governs gas only until the first reference rate, so
// the snapshot needs no upkeep.
var defaultUSDPerXDR = math.LegacyMustNewDecFromStr("1.371")

// DefaultNoahConversionFactor seeds the numeraire's cross, NOAH base units
// per reference base unit, so gas is payable in NOAH from the first block.
// Nothing else is: a member factor needs a rate to exist, and the reference
// is a unit of account nobody holds. It is chain.BootstrapNoahUSDPrice in XDR
// terms, so the gas seed and the oracle's first NOAH price agree; a launch
// genesis overrides it with the opening price, and the first reference rate
// re-derives it. Rounded up for the same reason as the cross.
var DefaultNoahConversionFactor = defaultUSDPerXDR.QuoRoundUp(math.LegacyMustNewDecFromStr(chain.BootstrapNoahUSDPrice))

// DefaultGenesisState returns the safe, unconfigured Treasury genesis state:
// default policy, the NOAH cross seeded, member factors left to derivation.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultParams(),
		[]ConversionFactor{{Denom: chain.NoahBaseDenom, Factor: DefaultNoahConversionFactor}},
		DefaultRewardFundingState(),
		DefaultMonetaryMandate(),
		DefaultMonetaryPolicy(),
		DefaultExposureState(),
		false,
		DefaultMinBaseGasPrice,
	)
}

// DefaultRewardFundingState returns canonical empty reward-funding accounting.
// The keeper resets to it whenever a window settles, so the zero state has to
// mean the same thing at genesis and mid-chain.
func DefaultRewardFundingState() RewardFundingState {
	return RewardFundingState{
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: math.ZeroInt(),
	}
}

// Validate checks the context-free Treasury genesis invariants. Fund balances
// and Oracle-dependent tax-cap coverage are validated by the keeper.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.MonetaryPolicy.Validate(); err != nil {
		return err
	}

	hasNoah := false
	for i, factor := range gs.ConversionFactors {
		// The numeraire's cross is the one entry that is not a priced denom:
		// NOAH carries no feed, so it is matched exactly rather than
		// validated.
		if factor.Denom == chain.NoahBaseDenom {
			hasNoah = true
		} else if err := chain.ValidatePricedDenom(factor.Denom); err != nil {
			return fmt.Errorf("conversion factor denom %q is invalid: %w", factor.Denom, err)
		}
		// Strictly positive: the derived cap floors at one base unit, so a
		// zero factor could only ever have been written by a bug, and a
		// negative one prices nothing.
		if factor.Factor.IsNil() || !factor.Factor.IsPositive() {
			return fmt.Errorf("conversion factor for %s must be positive", factor.Denom)
		}
		// A factor is deliberately not compared against live rates. One kept
		// after its feed went dark is anchored to the rate it was last
		// derived under, by design.
		if i > 0 && factor.Denom <= gs.ConversionFactors[i-1].Denom {
			return fmt.Errorf("genesis conversion factors must be sorted by unique denom")
		}
	}
	// Mandatory, unlike every member entry: before the first reference rate
	// NOAH is the only denomination anyone holds that the fee gate could
	// accept, and the gate prices from this table alone. Without the cross a
	// chain launches unable to pay for the proposal that would fix it.
	if !hasNoah {
		return fmt.Errorf("conversion factors must include the NOAH cross %s", chain.NoahBaseDenom)
	}

	if gs.RewardFunding.ValidatorTarget.IsNil() {
		return errors.New("validator target must be set")
	}
	if gs.RewardFunding.ValidatorTarget.IsNegative() {
		return errors.New("validator target must be zero or positive")
	}
	if gs.RewardFunding.OracleTarget.IsNil() {
		return errors.New("oracle target must be set")
	}
	if gs.RewardFunding.OracleTarget.IsNegative() {
		return errors.New("oracle target must be zero or positive")
	}
	if gs.RewardFunding.ValidatorFeeValue.IsNil() {
		return errors.New("validator fee value must be set")
	}
	if gs.RewardFunding.ValidatorFeeValue.IsNegative() {
		return errors.New("validator fee value must be zero or positive")
	}
	if gs.RewardFunding.BlocksRemaining == 0 &&
		(!gs.RewardFunding.ValidatorTarget.IsZero() ||
			!gs.RewardFunding.OracleTarget.IsZero() ||
			!gs.RewardFunding.ValidatorFeeValue.IsZero()) {
		return errors.New("empty reward funding window must use the default state")
	}
	// An imported window arrives mid-accrual rather than being reached one
	// block at a time, so the ceiling the accrual gets from MaxBlockRewardTarget
	// has to be imposed here directly. The bound is a whole window's worth of
	// either target, which is the most the running state could legitimately
	// hold.
	maxAccrued := MaxBlockRewardTarget.Mul(math.NewIntFromUint64(MaxRewardFundingWindow))
	for _, accrued := range []struct {
		name  string
		value math.Int
	}{
		{"validator target", gs.RewardFunding.ValidatorTarget},
		{"oracle target", gs.RewardFunding.OracleTarget},
		{"validator fee value", gs.RewardFunding.ValidatorFeeValue},
	} {
		if accrued.value.GT(maxAccrued) {
			return fmt.Errorf("%s must not exceed %s: %s", accrued.name, maxAccrued, accrued.value)
		}
	}
	if err := gs.MonetaryMandate.Validate(); err != nil {
		return err
	}
	if err := gs.ExposureState.Validate(); err != nil {
		return err
	}

	if gs.BaseGasPrice.IsNil() {
		return errors.New("base gas price must be set")
	}
	if !gs.BaseGasPrice.IsInValidRange() {
		return errors.New("base gas price is not representable")
	}
	// The cross-field bounds are the controller's own invariant: the update
	// clamps every write into [MinBaseGasPrice, MaxBaseGasPrice], so a genesis
	// outside that band is a state no run of the controller could have
	// produced.
	if gs.BaseGasPrice.LT(gs.Params.MinBaseGasPrice) || gs.BaseGasPrice.GT(MaxBaseGasPrice) {
		return fmt.Errorf(
			"base gas price must be between the floor %s and %s: %s",
			gs.Params.MinBaseGasPrice,
			MaxBaseGasPrice,
			gs.BaseGasPrice,
		)
	}

	return nil
}
