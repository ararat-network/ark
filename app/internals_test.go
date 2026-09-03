package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"

	"github.com/cosmos/cosmos-sdk/codec"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/ararat-network/ark/abci/lanes"
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

// PriorityLaneSet returns the mempool lane set the app wires.
func PriorityLaneSet() lanes.Set { return priorityLaneSet() }

// AcceptedQueries returns the Wasm query allow list the app wires.
func AcceptedQueries() wasmkeeper.AcceptedQueries { return acceptedQueries() }

// WasmQueryPlugins returns the Wasm query plugins the app wires.
func (app *ArkApp) WasmQueryPlugins(accepted wasmkeeper.AcceptedQueries) *wasmkeeper.QueryPlugins {
	return app.wasmQueryPlugins(accepted)
}

// Inner returns the router the policy layer wraps.
func (r ExecutionPolicyRouter) Inner() wasmkeeper.MessageRouter { return r.inner }

// Treasury returns the Treasury keeper the router prices tax against.
func (r ExecutionPolicyRouter) Treasury() *treasurykeeper.Keeper { return r.treasury }

// Cdc returns the codec the router decodes messages with.
func (r ExecutionPolicyRouter) Cdc() codec.Codec { return r.cdc }

// WithInner returns a copy of the router wrapping inner instead, so a test can
// observe the app's own wiring against a stand-in router.
func (r ExecutionPolicyRouter) WithInner(inner wasmkeeper.MessageRouter) ExecutionPolicyRouter {
	r.inner = inner
	return r
}
