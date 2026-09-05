package keeper_test

import (
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

// withMaxGas stamps the consensus block-gas budget the controller reads.
func (s *KeeperTestSuite) withMaxGas(maxGas int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
		Block: &cmtproto.BlockParams{MaxGas: maxGas},
	})
}

// TestEndBlockerRaisesBaseFeeAboveTarget pins the controller's up-step: a
// full block against the default half-full target moves the price up by
// exactly the adjustment rate, and the change is announced.
func (s *KeeperTestSuite) TestEndBlockerRaisesBaseFeeAboveTarget() {
	s.withMaxGas(1_000_000)
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, 1_000_000))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.1025"), price)
	s.requireTypedEvent(&types.EventBaseGasPriceUpdated{
		BaseGasPrice: math.LegacyMustNewDecFromStr("0.1025"),
	})
}

// TestEndBlockerDecaysBaseFeeToTheFloor pins the down-step and the clamp in
// one motion: an empty block moves an elevated price down by the full rate,
// and the floor catches what the decay would overshoot.
func (s *KeeperTestSuite) TestEndBlockerDecaysBaseFeeToTheFloor() {
	s.withMaxGas(1_000_000)
	s.Require().NoError(s.keeper.BaseGasPrice.Set(s.ctx, math.LegacyMustNewDecFromStr("0.101")))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	// 0.101 × 0.975 = 0.098475, below the 0.1 floor: the clamp binds.
	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(testMinBaseGasPrice, price)
}

// TestEndBlockerClampsTheStepToTheRate pins the delta clamp that makes the
// rate parameter mean "largest move per block" for every target: a low
// target would otherwise let a full block move the price by many times the
// rate.
func (s *KeeperTestSuite) TestEndBlockerClampsTheStepToTheRate() {
	s.withMaxGas(1_000_000)
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.BaseFeeTargetUtilisation = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, 1_000_000))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	// Raw delta would be 0.025 × (1M − 100k)/100k = 0.225; the clamp holds
	// the step at the rate itself.
	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.1025"), price)
}

// TestEndBlockerHoldsWithoutABlockGasBudget pins the undefined-utilisation
// verdict: MaxGas of −1 gives the controller no denominator, so the price
// holds — but the clamp still runs, so a governance floor raised above the
// held price binds the same block.
func (s *KeeperTestSuite) TestEndBlockerHoldsWithoutABlockGasBudget() {
	s.withMaxGas(-1)
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, 500_000))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(testMinBaseGasPrice, price)

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MinBaseGasPrice = math.LegacyMustNewDecFromStr("0.5")
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	price, err = s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.5"), price)
}

// TestEndBlockerZeroRateDisablesTheController pins the flat-floor mode: a
// zero adjustment rate holds the price through a full block, and holding is
// not news — no event.
func (s *KeeperTestSuite) TestEndBlockerZeroRateDisablesTheController() {
	s.withMaxGas(1_000_000)
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.BaseFeeAdjustmentRate = math.LegacyZeroDec()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, 1_000_000))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(testMinBaseGasPrice, price)
	s.requireNoTypedEvent(&types.EventBaseGasPriceUpdated{})
}

// TestTallyBlockGasSaturates pins the overflow verdict: a tally past a
// uint64 pins at full rather than wrapping, and the controller reads it as a
// maximally full block.
func (s *KeeperTestSuite) TestTallyBlockGasSaturates() {
	s.withMaxGas(1_000_000)
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, ^uint64(0)))
	s.Require().NoError(s.keeper.TallyBlockGas(s.ctx, ^uint64(0)))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.1025"), price)
}

// TestGetRequiredGasFeeCeilsInTheReferenceDenom pins the requirement's
// rounding direction and unit: it sizes a payment floor, so it rounds up,
// in the reference denom.
func (s *KeeperTestSuite) TestGetRequiredGasFeeCeilsInTheReferenceDenom() {
	params, price := s.gasPricing()

	// 15 gas at 0.1/gas is 1.5, which ceils to 2.
	required, _, err := s.keeper.GetRequiredGasFee(s.ctx, params, price, 15, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.XDRBaseDenom, 2), required)

	required, _, err = s.keeper.GetRequiredGasFee(s.ctx, params, price, 0, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(required.IsZero())
}

// gasPricing reads the two inputs GetRequiredGasFee's callers thread through.
func (s *KeeperTestSuite) gasPricing() (types.Params, math.LegacyDec) {
	s.T().Helper()
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	return params, price
}

// TestGetRequiredGasFeePricesEveryAcceptedSource pins the one requirement
// seam over its two factor sources: identity for the reference, the table
// for everything else, NOAH included — and refusal by absence otherwise,
// NOAH before derivation included. The returned factor is the cross the
// requirement priced with, which the ante's tip normalisation divides by.
func (s *KeeperTestSuite) TestGetRequiredGasFeePricesEveryAcceptedSource() {
	params, price := s.gasPricing()

	_, _, err := s.keeper.GetRequiredGasFee(s.ctx, params, price, 200_000, chain.NoahBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)

	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:         chain.USDBaseDenom,
		Factor:        math.LegacyNewDec(2),
		DerivedHeight: 5,
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, types.ConversionFactor{
		Denom:         chain.NoahBaseDenom,
		Factor:        math.LegacyMustNewDecFromStr("0.25"),
		DerivedHeight: 6,
	}))

	reference, factor, err := s.keeper.GetRequiredGasFee(s.ctx, params, price, 200_000, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000), reference)
	s.Require().Equal(math.LegacyOneDec(), factor)

	member, factor, err := s.keeper.GetRequiredGasFee(s.ctx, params, price, 200_000, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.USDBaseDenom, 40_000), member)
	s.Require().Equal(math.LegacyNewDec(2), factor)

	noah, factor, err := s.keeper.GetRequiredGasFee(s.ctx, params, price, 200_000, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 5_000), noah)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.25"), factor)

	_, _, err = s.keeper.GetRequiredGasFee(s.ctx, params, price, 200_000, chain.KRWBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)
}
