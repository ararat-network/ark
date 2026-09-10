package app

import (
	"fmt"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/ararat-network/ark/app/ante"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

var (
	_ wasmkeeper.MessageRouter = executionPolicyRouter{}
	_ gmptypes.MessageRouter   = executionPolicyRouter{}
	_ icatypes.MessageRouter   = executionPolicyRouter{}
)

// executionPolicyRouter shares message policy across contracts, GMP-derived accounts, and ICA
// accounts. Each dispatch surface authenticates its sender before consulting the router.
func (app *ArkApp) executionPolicyRouter() executionPolicyRouter {
	return executionPolicyRouter{
		inner:    app.MsgServiceRouter(),
		treasury: app.TreasuryKeeper,
		bank:     app.BankKeeper,
		staking:  app.StakingKeeper,
		cdc:      app.appCodec,
	}
}

// executionPolicyRouter applies ante message policies and Treasury's tax calculator to
// execution-generated SDK messages. Signed top-level messages use BaseApp's router, so they are not
// checked or taxed twice.
type executionPolicyRouter struct {
	inner    wasmkeeper.MessageRouter
	treasury *treasurykeeper.Keeper
	bank     bankkeeper.BaseKeeper
	staking  *stakingkeeper.Keeper
	cdc      codec.Codec
}

func (r executionPolicyRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	inner := r.inner.Handler(msg)
	if inner == nil {
		// Unroutable. Return nil so the dispatching surface reports its own
		// unknown-message error rather than one about tax.
		return nil
	}
	return func(ctx sdk.Context, req sdk.Msg) (*sdk.Result, error) {
		// A vote dispatched here meets the same stake floor as a signed one,
		// authz-wrapped included — the seam the Hub's wasm-only twin of this
		// check leaves open.
		if err := ante.ValidateGovVoteMsg(ctx, r.cdc, r.staking, req, 0); err != nil {
			return nil, err
		}
		// A MultiSend dispatched here meets the same fan-out cap and pays the
		// same quadratic surcharge as a signed one.
		if err := ante.ValidateMultiSendMsg(ctx, r.cdc, req, 0); err != nil {
			return nil, err
		}
		// Charge tax before principal so the dispatch requires both. The caller's cached execution
		// branch rolls back tax and message writes together on failure.
		if err := r.collectTax(ctx, req); err != nil {
			return nil, err
		}
		return inner(ctx, req)
	}
}

// collectTax charges the sending contract for one dispatched message, on top of
// the principal that message already moves.
func (r executionPolicyRouter) collectTax(ctx sdk.Context, msg sdk.Msg) error {
	tax, _, err := r.treasury.ComputeTax(ctx, []sdk.Msg{msg})
	if err != nil {
		return fmt.Errorf("computing execution-generated transfer tax: %w", err)
	}
	if tax.IsZero() {
		return nil
	}

	// Each dispatch surface authenticates all message signers against its contract, derived
	// account, or interchain account before routing. That signer is therefore the tax payer.
	signers, _, err := r.cdc.GetMsgV1Signers(msg)
	if err != nil {
		return fmt.Errorf("resolving execution-generated tax payer: %w", err)
	}
	if len(signers) != 1 {
		return fmt.Errorf("expected exactly one signer for execution-generated tax, got %d", len(signers))
	}

	// Straight to the collector, in the same cache as the transfer that owes it
	// (D42). No feegrant here: dispatch carries no granter, so the dispatching
	// account always bears its own tax.
	return r.bank.SendCoinsFromAccountToModule(
		ctx,
		sdk.AccAddress(signers[0]),
		treasurytypes.TransferTaxCollectorName,
		tax,
	)
}
