// Package market is the conversion venue between NOAH and the registered
// stablecoins, and the chain's sole minter.
//
// It owns the quote: the spread over a denomination-bearing virtual pool
// (BasePool, ArkPoolDelta, and its recovery), the Tobin tax and its overrides,
// and eligibility read from x/asset. It escrows each gross offer, mints and
// burns for settlement, accumulates the block's conversion facts, hands them
// to Treasury once from its EndBlocker, and burns the overflow Treasury
// returns. The conversion mandate lets a committee move policy inside a
// governance-set corridor.
//
// Market never sees the credit split behind the burn it executes, and owns no
// tax policy, fund target, or allocation decision.
package market
