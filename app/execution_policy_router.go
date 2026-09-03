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

// executionPolicyRouter builds the one execution router every dispatch
// surface shares: contracts, the accounts GMP derives for a remote caller
// (D48), and interchain accounts under the ICA host. Each authenticates its
// own sender before routing — Wasmd against the contract, GMP against the
// derived account, the ICA host against the channel's interchain account — so
// by the time the router runs the signer is provably the party to charge, and
// no surface needs its own policy path.
func (app *ArkApp) executionPolicyRouter() executionPolicyRouter {
	return executionPolicyRouter{
		inner:    app.MsgServiceRouter(),
		treasury: app.TreasuryKeeper,
		bank:     app.BankKeeper,
		staking:  app.StakingKeeper,
		cdc:      app.appCodec,
	}
}

// executionPolicyRouter is the execution-path twin of the ante chain: it
// applies the message policies ante applies to signed transactions — the
// gov-vote stake floor, the MultiSend fan-out guard, and the transfer tax —
// to messages a contract, derived account, or interchain account dispatches.
//
// It wraps the router rather than Wasmd's messenger so it sees the SDK message
// Wasmd's own encoder produced, which is what keeps a second message model out
// of Ark (D41, D43). It owns no rate or cap arithmetic: every figure comes
// from the ante package's validators and Treasury's canonical calculator.
//
// Only execution-generated messages reach it. Signed top-level messages route
// through BaseApp's own router and stay ante-owned, so nothing is checked or
// taxed twice.
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
		// Tax first. It makes principal plus tax the effective requirement, so
		// an underfunded contract fails before any recipient output moves. A
		// failure here or below unwinds both together: Wasmd dispatches inside
		// a cached store, so the rollback D42 requires is where this sits, not
		// something this function performs.
		if err := r.collectTax(ctx, req); err != nil {
			return nil, err
		}
		return inner(ctx, req)
	}
}

// collectTax charges the sending contract for one dispatched message, on top of
// the principal that message already moves.
func (r executionPolicyRouter) collectTax(ctx sdk.Context, msg sdk.Msg) error {
	tax, err := r.treasury.ComputeTax(ctx, []sdk.Msg{msg})
	if err != nil {
		return fmt.Errorf("computing execution-generated transfer tax: %w", err)
	}
	if tax.IsZero() {
		return nil
	}

	// The payer is the message's signer. Every surface has already refused a
	// message whose signers are not exactly its authenticated account — the
	// contract, the derived account, the interchain account — and does so
	// before consulting this router, so the signer is the party to charge by
	// construction and needs no separate plumbing.
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
