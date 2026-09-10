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
