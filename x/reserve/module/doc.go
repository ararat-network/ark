// Package reserve is the strategic Reserve: custody the protocol may deploy,
// and the accounting that decides how much of it counts.
//
// It owns the strategic_reserve account and its send restriction (NOAH and
// registry members; external symbols are refused), the one-committee Reserve
// mandate, the Reserve-to-Buffer and Reserve-to-Insurance transfers
// (governance's outright, the committee's inside the mandate floor), the
// append-only quantity journal of positions (deployment, return attribution,
// impairment, closure), the recognition policy of eligibility entries with
// haircuts, jointly solved caps, and per-entry staleness, and the burn
// authority split between committee and governance. Recognition degrades to
// zero, never to a stale figure, and no valuation is stored.
//
// Treasury sizes the Reserve target and this module reports the capital it
// recognises through one one-way interface; it reads Treasury's required
// capital and shortfalls back only to bound its own burns and transfers. It
// owns no tax, target, waterfall, liability valuation, or claim.
package reserve
