// Package treasury is the balance-sheet and economic-policy module: what the
// protocol takes in, pays out, and holds against its liabilities.
//
// It owns the transfer tax (rate, reference cap, and the per-denomination
// caps derived from the conversion-factor table) and the one calculator every
// taxed surface prices through; reward funding, which meets the validator and
// Oracle block targets over a funding window from tax first and the subsidy
// pool second; the liability partition (gross, self-held, net) and the three
// fund targets scaled by the exposure multiplier; the expansion waterfall and
// the coverage draw, run once per block over Market's conversion totals; the
// base-fee controller; and the economic-policy committee mandate. Its accounts
// are subsidy_pool and redemption_buffer, which admit NOAH alone, and
// transfer_tax_collector, which holds tax until settlement routes it.
//
// It reads rates and the reference denomination from x/oracle, membership and
// lifecycle from x/asset, and recognised capital from x/claims and x/reserve
// through one-way interfaces. It never quotes, mints, burns, holds pool state,
// adjudicates a claim, or touches Reserve custody: it says what each fund
// needs, and each fund says what it has.
package treasury
