package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
)

// acceptedQueries is the set of gRPC query paths contracts may reach, each with
// the response it decodes into. Hand-written, as Osmosis, Neutron, Juno, and
// Archway keep theirs: a contract reading a value makes it an input to
// consensus and freezes the response shape for the life of the chain, so
// listing is a review rather than an annotation. Every entry must also be
// annotated module_query_safe, which the tests check.
//
// Empty at launch; populating it belongs to the Section 16.6 gate. The tax
// estimate D43 asks for is Treasury's ComputeTax query, listed here like any
// other path (D74).
func acceptedQueries() wasmkeeper.AcceptedQueries {
	return wasmkeeper.AcceptedQueries{}
}

// wasmQueryPlugins supplies the two queriers Wasmd refuses by default. Both
// read through the accept list; every other plugin stays Wasmd's own, Custom
// included, which refuses every request.
func (app *ArkApp) wasmQueryPlugins(accepted wasmkeeper.AcceptedQueries) *wasmkeeper.QueryPlugins {
	return &wasmkeeper.QueryPlugins{
		Stargate: wasmkeeper.AcceptListStargateQuerier(accepted, app.GRPCQueryRouter(), app.appCodec),
		Grpc:     wasmkeeper.AcceptListGrpcQuerier(accepted, app.GRPCQueryRouter(), app.appCodec),
	}
}
