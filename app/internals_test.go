package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"

	"github.com/cosmos/cosmos-sdk/codec"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// This file exposes the app's unexported wiring to the app_test package. It is
// a test file, so nothing here reaches the binary.

// ExecutionPolicyRouter is the execution-path message router under test.
type ExecutionPolicyRouter = executionPolicyRouter

// NewExecutionPolicyRouter builds a router around inner, so a test can observe
// what the policy layer hands the router it wraps.
func NewExecutionPolicyRouter(
	inner wasmkeeper.MessageRouter,
	treasury *treasurykeeper.Keeper,
	bank bankkeeper.BaseKeeper,
	staking *stakingkeeper.Keeper,
	cdc codec.Codec,
) ExecutionPolicyRouter {
	return executionPolicyRouter{
		inner:    inner,
		treasury: treasury,
		bank:     bank,
		staking:  staking,
		cdc:      cdc,
	}
}

// ExecutionPolicyRouter returns the router the app wires.
func (app *ArkApp) ExecutionPolicyRouter() ExecutionPolicyRouter {
	return app.executionPolicyRouter()
}

// AcceptedQueries returns the Wasm query allow list the app wires.
func AcceptedQueries() wasmkeeper.AcceptedQueries { return acceptedQueries() }

// WasmQueryPlugins returns the Wasm query plugins the app wires.
func (app *ArkApp) WasmQueryPlugins(accepted wasmkeeper.AcceptedQueries) *wasmkeeper.QueryPlugins {
	return app.wasmQueryPlugins(accepted)
}

// WasmMessageHandler returns the contract message handler the app wires.
func (app *ArkApp) WasmMessageHandler() wasmkeeper.Messenger { return app.wasmMessageHandler() }

// ContractCapabilities returns the capability list the app hands the VM.
func ContractCapabilities() []string { return contractCapabilities() }

// Inner returns the router the policy layer wraps.
func (r ExecutionPolicyRouter) Inner() wasmkeeper.MessageRouter { return r.inner }

// Treasury returns the Treasury keeper the router prices tax against.
func (r ExecutionPolicyRouter) Treasury() *treasurykeeper.Keeper { return r.treasury }

// Cdc returns the codec the router decodes messages with.
func (r ExecutionPolicyRouter) Cdc() codec.Codec { return r.cdc }
