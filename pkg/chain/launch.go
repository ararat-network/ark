package chain

// BootstrapNoahUSDPrice is the launch placeholder for NOAH's price, stated
// once, in US dollars: one NOAH is one dollar. The sidecar's default bootstrap
// quotes it as the NOAH/USD leg until a provider serves the pair, and
// Treasury's default genesis derives its NOAH conversion factor from it, so
// the oracle's first NOAH price and the gas seed it replaces agree. Dollars
// because every sidecar route hubs through NOAH/USD, the XDR reference
// included. A launch genesis and a validator's sidecar config both override
// it; this is only the value nobody chose.
const BootstrapNoahUSDPrice = "1"
