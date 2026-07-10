package preblock

import (
	cometabci "github.com/cometbft/cometbft/abci/types"
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
func (h *Handler) recordValidatorReports(decidedCommit cometabci.CommitInfo, voteTargets map[string]math.LegacyDec) {
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
				oraclemetrics.AddValidatorReportForTicker(validator.String(), denom, oraclemetrics.Absent)
				continue
			}

			// Otherwise, check if the validator reported a price for the denom.
			price, ok := validatorPrices[denom]
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
