// Package testdata holds the compiled contracts the application tests run.
package testdata

import _ "embed"

// ibcCallbacksWasm embeds CosmWasm ibc-callbacks v2.2.2 (sha256 541506d4…84af).
// It exports source/destination callbacks and needs iterator and stargate.
//
//go:embed ibc_callbacks.wasm
var ibcCallbacksWasm []byte

// IBCCallbacksContractWasm returns the ibc-callbacks contract.
func IBCCallbacksContractWasm() []byte {
	return ibcCallbacksWasm
}

// grantWasm embeds the grant contract from contracts/grant: the optimizer
// build `make contracts-optimize` writes, the bytes a store proposal cites.
// It needs stargate and cosmwasm_2_1.
//
//go:embed grant.wasm
var grantWasm []byte

// GrantContractWasm returns the grant contract.
func GrantContractWasm() []byte {
	return grantWasm
}
