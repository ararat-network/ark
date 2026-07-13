package preblock

import (
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	abcioracle "ark/abci/oracle"
	oraclemetrics "ark/abci/oracle/metrics"
)

// recordPrices records all given prices per denom and reports them as float64 metrics.
func (h *Handler) recordPrices(prices map[string]math.LegacyDec) {
	for denom, price := range prices {
		floatPrice, _ := price.Float64()
		oraclemetrics.ObservePriceForTicker(denom, floatPrice)
	}
}

// recordValidatorReports records whether each validator in the decided commit
// reported a price for each vote target denom and, if so, the price reported.
func (h *Handler) recordValidatorReports(reports []abcioracle.ValidatorReport, voteTargets []string) {
	for _, report := range reports {
		validator := report.Validator
		nilVote := report.BlockIDFlag != cometproto.BlockIDFlagCommit
		// Iterate over each vote target denom and record whether the validator reported a price for it.
		for _, denom := range voteTargets {
			// If the validator reported a nil vote, record that and skip.
			if nilVote {
				oraclemetrics.AddValidatorReportForTicker(validator.String(), denom, oraclemetrics.Absent)
				continue
			}

			// Otherwise, check if the validator reported a price for the denom.
			price, ok := report.Rates[denom]
			if !ok {
				oraclemetrics.AddValidatorReportForTicker(validator.String(), denom, oraclemetrics.MissingPrice)
				continue
			}

			// If the validator reported a price, record that price.
			floatPrice, _ := price.Float64()
			oraclemetrics.AddValidatorReportForTicker(validator.String(), denom, oraclemetrics.WithPrice)
			oraclemetrics.AddValidatorPriceForTicker(validator.String(), denom, floatPrice)
		}
	}
}
