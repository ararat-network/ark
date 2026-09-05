// Package oracle holds consensus prices and the registry of what is priced.
//
// Rates arrive through the vote-extension pipeline in abci/oracle and are
// applied in the preblocker, so every reader in a block sees one set; each is
// NOAH per unit of its denomination. The module owns the feed registry, where
// a feed is keyed by the denomination it prices and consumers register guards
// against its removal; the protocol reference denomination; validator
// attendance and participation scores; and reward settlement, which pays out
// every denomination its account holds. A feed creates no liability;
// registering an asset in x/asset does.
//
// It takes no fiscal decision and mints nothing.
package oracle
