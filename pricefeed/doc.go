// Package pricefeed holds the two halves of the price feed and the contract
// between them.
//
// pricefeed/api is the generated transport: the PriceFeed service the
// sidecar serves and the node's client calls. pricefeed/client is the node
// side; pricefeed/sidecar is the process a validator runs beside the node.
//
// # Compatibility
//
// The contract is the proto package ark.pricefeed.v1: what a sidecar must
// serve and what a node will accept. Within v1 every change is additive, a
// new field or RPC and never a changed or removed one, so a sidecar built
// before the change still answers and a node built before it still reads the
// answer. A change that cannot be made additively is a v2 service, and a node
// that needs it refuses an older sidecar by the Unimplemented status the
// sidecar returns. Each price is the compact encoding in pkg/encoding, the
// bytes the vote extension carries, so that encoding is part of the contract
// too.
//
// The build version a sidecar reports in every response identifies the build
// for operators. It says nothing about compatibility: the node and the
// sidecar release on their own cadences, so the node logs and exports it and
// never gates on it.
package pricefeed
