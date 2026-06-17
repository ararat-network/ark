package oracle

import (
	cometabci "github.com/cometbft/cometbft/abci/types"
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	noahmetrics "noah/pkg/metrics"
)

// recordPrices records all given prices per denom and reports them as float64 metrics.
func (h *PreBlockHandler) recordPrices(prices map[string]math.LegacyDec) {
	for denom, price := range prices {
		floatPrice, _ := price.Float64()
		noahmetrics.ObservePriceForTicker(denom, floatPrice)
	}
}

// recordValidatorReports records whether each validator in the decided commit
// reported a price for each vote target denom and, if so, the price reported.
func (h *PreBlockHandler) recordValidatorReports(decidedCommit cometabci.CommitInfo, voteTargets map[string]math.LegacyDec) {
	// Iterate over each validator in the commit.
	for _, vote := range decidedCommit.Votes {
		var nilVote bool
		validator := sdk.ConsAddress(vote.Validator.Address)
		// If the validator voted nil, record that status.
		if vote.BlockIdFlag != cometproto.BlockIDFlagCommit {
			nilVote = true
		}
		// Iterate over each vote target denom and record whether the validator reported a price for it.
		validatorPrices := h.pa.GetPricesForValidator(validator)
		for denom := range voteTargets {
			// If the validator reported a nil vote, record that and skip.
			if nilVote {
				noahmetrics.AddValidatorReportForTicker(validator.String(), denom, noahmetrics.Absent)
				continue
			}

			// Otherwise, check if the validator reported a price for the denom.
			price, ok := validatorPrices[denom]
			if !ok {
				noahmetrics.AddValidatorReportForTicker(validator.String(), denom, noahmetrics.MissingPrice)
				continue
			}

			// If the validator reported a price, record that price.
			floatPrice, _ := price.Float64()
			noahmetrics.AddValidatorReportForTicker(validator.String(), denom, noahmetrics.WithPrice)
			noahmetrics.AddValidatorPriceForTicker(validator.String(), denom, floatPrice)
		}
	}
}
