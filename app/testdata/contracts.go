// Package testdata holds the compiled contracts the application tests run.
package testdata

import _ "embed"

// ibcCallbacksWasm is the CosmWasm ibc-callbacks example contract, release
// v2.2.2 (sha256 541506d4…84af, matching the release's checksums.txt). It
// exports ibc_source_callback and ibc_destination_callback, which no
// contract Wasmd ships does, and requires only the iterator and stargate
// capabilities.
//
//go:embed ibc_callbacks.wasm
var ibcCallbacksWasm []byte

// IBCCallbacksContractWasm returns the ibc-callbacks contract.
func IBCCallbacksContractWasm() []byte {
	return ibcCallbacksWasm
}
