package types

import (
	"fmt"
	"strings"
	time "time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewPrevote returns Prevote object
func NewPrevote(hash VoteHash, voter sdk.ValAddress, submitBlock uint64) Prevote {
	return Prevote{
		Hash:        hash.String(),
		Voter:       voter.String(),
		SubmitBlock: submitBlock,
	}
}

// NewVote creates a Vote instance
func NewVote(exchangeRates ExchangeRates, voter sdk.ValAddress) Vote {
	return Vote{
		ExchangeRates: exchangeRates,
		Voter:         voter.String(),
	}
}

// NewExchangeRate creates a ExchangeRate instance
func NewExchangeRate(denom string, rate math.LegacyDec, timestamp time.Time, height uint64) ExchangeRate {
	return ExchangeRate{
		denom,
		rate,
		timestamp,
		height,
	}
}

// ExchangeRates - array of ExchangeRate
type ExchangeRates []ExchangeRate

// ParseExchangeRates ExchangeRateTuple parser
func ParseExchangeRates(tuplesStr string) (ExchangeRates, error) {
	tuplesStr = strings.TrimSpace(tuplesStr)
	if len(tuplesStr) == 0 {
		return nil, nil
	}

	tupleStrs := strings.Split(tuplesStr, ",")
	tuples := make(ExchangeRates, len(tupleStrs))
	duplicateCheckMap := make(map[string]bool)
	for i, tupleStr := range tupleStrs {
		decCoin, err := sdk.ParseDecCoin(tupleStr)
		if err != nil {
			return nil, err
		}

		tuples[i] = ExchangeRate{
			Denom: decCoin.Denom,
			Rate:  decCoin.Amount,
		}

		if _, ok := duplicateCheckMap[decCoin.Denom]; ok {
			return nil, fmt.Errorf("duplicated denom %s", decCoin.Denom)
		}

		duplicateCheckMap[decCoin.Denom] = true
	}

	return tuples, nil
}
