package delegator_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// OracleSuite is the price pipeline over a real network: four validators,
// each with its own sidecar, report rates through vote extensions, the
// chain aggregates them, and a conversion clears against the result.
type OracleSuite struct {
	*delegator.Suite
}

func (s *OracleSuite) SetupSuite() {
	if !s.Env.PriceFeed {
		s.T().Skip("TEST_PRICEFEED is off")
	}
	s.Suite.SetupSuite()
	// Pinned, axdr prices from the sidecars' bootstrap legs within blocks; live, it waits on an FX
	// venue.
	s.Require().NoError(s.Chain.WaitForExchangeRate(s.GetContext(), "axdr", 4*time.Minute))
}

func (s *OracleSuite) TestEveryValidatorReportsRates() {
	votes, err := s.Chain.WaitForPricedVoteExtensions(s.GetContext(), 2*time.Minute)
	s.Require().NoError(err)
	for _, vote := range votes {
		s.Require().Equal("BLOCK_ID_FLAG_COMMIT", vote.BlockIDFlag)
	}
	rates, err := s.Chain.ExchangeRates(s.GetContext())
	s.Require().NoError(err)
	s.Require().Contains(rates, "ausd")
	s.Require().Contains(rates, "axdr")
	if s.Env.PriceFeedLive {
		return
	}
	// Every sidecar reports the same pinned legs, so each feed is its leg times USD/NOAH. The chain
	// tallies a feed as a cross against the reference, whose two roundings can move the last place.
	usd := sdkmath.LegacyMustNewDecFromStr(rates["ausd"])
	for _, leg := range chainsuite.PinnedPrices {
		denom := "a" + strings.ToLower(strings.TrimSuffix(leg.Pair, "/USD"))
		rate, ok := rates[denom]
		if !ok {
			continue
		}
		want := sdkmath.LegacyMustNewDecFromStr(leg.Price).Mul(usd)
		got := sdkmath.LegacyMustNewDecFromStr(rate)
		s.Require().True(got.Sub(want).Abs().LTE(want.QuoInt64(1_000_000_000_000)), "%s: %s, pinned %s", denom, got, want)
	}
}

func (s *OracleSuite) TestGasPriceSheetPricesNoah() {
	sheet, err := s.Chain.QueryJSON(s.GetContext(), "@this", "treasury", "gas-prices")
	s.Require().NoError(err)
	s.Require().Equal("axdr", sheet.Get("reference_gas_price.denom").String())
	s.Require().NotEmpty(sheet.Get(`gas_prices.#(denom=="anoah").gas_price`).String(), sheet.Raw)
}

func (s *OracleSuite) TestConversionClears() {
	ctx := s.GetContext()
	trader := s.Wallet
	before, err := s.Chain.GetBalance(ctx, trader.FormattedAddress(), "ausd")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, trader.KeyName(), "market", "sell", chainsuite.NOAHCoin(1), "ausd")
	s.Require().NoError(err)
	// Settlement runs in the EndBlocker after placement.
	s.Require().NoError(testutil.WaitForBlocks(ctx, 2, s.Chain))
	after, err := s.Chain.GetBalance(ctx, trader.FormattedAddress(), "ausd")
	s.Require().NoError(err)
	s.Require().True(after.GT(before), "the conversion settled nothing: before %s, after %s", before, after)
}

func TestOracle(t *testing.T) {
	s := &OracleSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
		ChainSpec: &interchaintest.ChainSpec{
			NumValidators: &chainsuite.FourValidators,
		},
	})}}
	suite.Run(t, s)
}
