package chain

// BootstrapNoahPerUSD is the launch placeholder for the dollar's price in
// NOAH, stated once: one dollar is one NOAH. It carries the orientation every
// oracle rate carries. The sidecar's default bootstrap seeds it as the
// USD/NOAH leg until a provider serves the pair, and Treasury's default
// genesis derives its NOAH conversion factor from it, so the oracle's first
// NOAH price and the gas seed it replaces agree. Dollars because every
// sidecar route hubs through USD/NOAH, the XDR reference included. A launch
// genesis and a validator's sidecar config both override it; this is only the
// value nobody chose.
const BootstrapNoahPerUSD = "1"
