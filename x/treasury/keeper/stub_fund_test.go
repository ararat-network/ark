package keeper_test

import (
	"context"

	"cosmossdk.io/math"
)

// stubFund reports a fixed recognised capital for a committee-operated fund.
// The benchmarks use it because they measure Treasury's own valuation and
// waterfall work, and a real operating module would only add an unrelated store
// read to every sample. Behavioural tests use the gomock keepers instead.
type stubFund struct {
	recognised math.Int
}

func (s stubFund) RecognisedCapital(context.Context) (math.Int, error) {
	if s.recognised.IsNil() {
		return math.ZeroInt(), nil
	}
	return s.recognised, nil
}

// CreditToTarget answers the nominal shortfall: the stub holds nothing capped.
func (s stubFund) CreditToTarget(ctx context.Context, target math.Int) (math.Int, error) {
	recognised, err := s.RecognisedCapital(ctx)
	if err != nil {
		return math.Int{}, err
	}
	if target.LTE(recognised) {
		return math.ZeroInt(), nil
	}
	return target.Sub(recognised), nil
}
