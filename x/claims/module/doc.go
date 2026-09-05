// Package claims is Insurance: the custody, the mandate, and the record behind
// every claim the protocol pays.
//
// It owns the claims_insurance account and its send restriction (NOAH alone),
// the Claims mandate under which a committee submits within a fixed gross
// term allowance and governance submits without one, the immutable claim
// record, the reservation that encumbers each pending amount, and the shared
// cancellation period. Claims settle from its EndBlocker, last in the block,
// once their closing height passes; nothing pays out on a message.
//
// Treasury sizes the Insurance target; this module reports the capital it
// recognises, balance less reservation, through one one-way interface. It
// owns no tax, target, waterfall, valuation, or Reserve authority.
package claims
