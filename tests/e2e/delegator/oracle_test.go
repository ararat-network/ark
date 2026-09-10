package delegator_test

import (
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

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
	s.Require().NoError(s.Chain.WaitForExchangeRate(s.GetContext(), "ausd", 4*time.Minute))
}

func (s *OracleSuite) TestEveryValidatorReportsRates() {
	votes, err := s.Chain.VoteExtensions(s.GetContext(), 0)
	s.Require().NoError(err)
	s.Require().Len(votes, len(s.Chain.Validators))
	for _, vote := range votes {
		s.Require().Equal("BLOCK_ID_FLAG_COMMIT", vote.BlockIDFlag)
		s.Require().NotEmpty(vote.Rates, "validator %s reported no rates", vote.ValidatorAddress)
	}
	rates, err := s.Chain.ExchangeRates(s.GetContext())
	s.Require().NoError(err)
	s.Require().Contains(rates, "ausd")
	s.Require().Contains(rates, "axdr")
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
	_, err = s.Node().ExecTx(ctx, trader.KeyName(), "market", "swap", chainsuite.NOAHCoin(1), "ausd")
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
