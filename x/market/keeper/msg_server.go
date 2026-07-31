package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/pkg/chain"
	"ark/x/market/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	types.UnimplementedMsgServer

	k *Keeper
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// Swap validates the trader address and executes a swap back to the same account.
func (m msgServer) Swap(ctx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
	addr, err := chain.ParseCanonicalAccountAddress("trader", msg.Trader)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}

	swapCoin, swapFee, err := m.k.Swap(ctx, addr, addr, msg.OfferCoin, msg.AskDenom, msg.MinimumReceive)
	if err != nil {
		return nil, err
	}

	return &types.MsgSwapResponse{
		SwapCoin: swapCoin,
		SwapFee:  swapFee,
	}, nil
}

// SwapSend validates the sender and recipient addresses and settles the swap to the recipient.
func (m msgServer) SwapSend(ctx context.Context, msg *types.MsgSwapSend) (*types.MsgSwapSendResponse, error) {
	fromAddr, err := chain.ParseCanonicalAccountAddress("from address", msg.FromAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}
	toAddr, err := chain.ParseCanonicalAccountAddress("to address", msg.ToAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}

	swapCoin, swapFee, err := m.k.Swap(ctx, fromAddr, toAddr, msg.OfferCoin, msg.AskDenom, msg.MinimumReceive)
	if err != nil {
		return nil, err
	}

	return &types.MsgSwapSendResponse{
		SwapCoin: swapCoin,
		SwapFee:  swapFee,
	}, nil
}

// Settle validates the trader address and executes a settlement redemption.
func (m msgServer) Settle(ctx context.Context, msg *types.MsgSettle) (*types.MsgSettleResponse, error) {
	addr, err := chain.ParseCanonicalAccountAddress("trader", msg.Trader)
	if err != nil {
		return nil, sdkerrors.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}

	redeemed, err := m.k.Settle(ctx, addr, msg.OfferCoin)
	if err != nil {
		return nil, sdkerrors.Wrapf(err, "settling %s", msg.OfferCoin)
	}

	return &types.MsgSettleResponse{RedeemedCoin: redeemed}, nil
}

// UpdateParams updates the params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	// Conversion capacity is not here: depth and the recovery period are
	// committee-delegable, so they move through the capacity messages and a
	// params replacement drafted from a stale copy cannot revert an emergency
	// resize.
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// SetTobinTaxOverride records one per-denomination Tobin exception.
func (m msgServer) SetTobinTaxOverride(ctx context.Context, msg *types.MsgSetTobinTaxOverride) (*types.MsgSetTobinTaxOverrideResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.SetTobinTaxOverride(ctx, msg.Denom, msg.TobinTax); err != nil {
		return nil, err
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTobinTaxOverrideSet{
		Denom:    msg.Denom,
		TobinTax: msg.TobinTax,
	}); err != nil {
		return nil, fmt.Errorf("emitting Market tobin tax override: %w", err)
	}

	return &types.MsgSetTobinTaxOverrideResponse{}, nil
}

// RemoveTobinTaxOverride deletes one per-denomination Tobin exception.
func (m msgServer) RemoveTobinTaxOverride(ctx context.Context, msg *types.MsgRemoveTobinTaxOverride) (*types.MsgRemoveTobinTaxOverrideResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.RemoveTobinTaxOverride(ctx, msg.Denom); err != nil {
		return nil, err
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTobinTaxOverrideRemoved{
		Denom: msg.Denom,
	}); err != nil {
		return nil, fmt.Errorf("emitting Market tobin tax override removal: %w", err)
	}

	return &types.MsgRemoveTobinTaxOverrideResponse{}, nil
}

// SetConversionMandate replaces or disables the conversion committee appointment.
// Every replacement advances the term, so a transaction prepared under the
// previous appointment can never apply under this one.
func (m msgServer) SetConversionMandate(ctx context.Context, msg *types.MsgSetConversionMandate) (*types.MsgSetConversionMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set conversion mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.ConversionMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting conversion mandate: %w", err)
	}
	term, err := current.NextTerm()
	if err != nil {
		return nil, err
	}

	updated := types.NewDisabledConversionMandate(term)
	if msg.Committee != "" {
		updated.Committee = msg.Committee
		updated.ActivationHeight = msg.ActivationHeight
		updated.ExpiryHeight = msg.ExpiryHeight
		updated.MinimumPolicy = msg.MinimumPolicy
		updated.MaximumPolicy = msg.MaximumPolicy
		// An absent cap is a capacity-only appointment: the constructor's zero
		// survives, delegating no Tobin power.
		if !msg.MaxTobinTax.IsNil() {
			updated.MaxTobinTax = msg.MaxTobinTax
		}
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		// Governance holds the unbounded path already, so a committee that is
		// also the authority would be a delegation to nobody while reading as a
		// live fast path.
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("conversion committee must be distinct from Market authority")
		}
		// An expiry already behind the chain is a delegation the committee could
		// never exercise, for the same reason a corridor in the wrong unit is:
		// the appointment reads as live and authorizes nothing. The window is
		// half-open, so a proposal landing exactly on its own expiry is already
		// too late. Genesis deliberately applies no such check — an import must
		// carry a mandate that expired before the export.
		if updated.ExpiryHeight <= uint64(sdkCtx.BlockHeight()) {
			return nil, fmt.Errorf(
				"conversion mandate expires at height %d, which is not above the current height %d",
				updated.ExpiryHeight,
				sdkCtx.BlockHeight(),
			)
		}
		// A corridor in any unit other than the live pool's could never
		// authorize an action. Rejecting it here turns a stillborn appointment
		// into a failed proposal instead of a committee discovering it has no
		// usable power at the moment it musters.
		capacity, err := m.k.ConversionPolicy.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting conversion policy: %w", err)
		}
		if updated.MinimumPolicy.BasePool.Denom != capacity.BasePool.Denom {
			return nil, fmt.Errorf(
				"conversion bounds are denominated in %s, not the live base pool %s",
				updated.MinimumPolicy.BasePool.Denom,
				capacity.BasePool.Denom,
			)
		}
	}

	if err := m.k.ConversionMandate.Set(ctx, updated); err != nil {
		return nil, fmt.Errorf("setting conversion mandate: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventConversionMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
	}); err != nil {
		return nil, fmt.Errorf("emitting Market conversion mandate: %w", err)
	}

	return &types.MsgSetConversionMandateResponse{}, nil
}

// UpdatePolicy applies one complete capacity update as the governance
// authority. Governance overrides the committee mandate, so no term, window, or
// corridor check applies — only the denomination guard every capacity write
// passes.
func (m msgServer) UpdatePolicy(ctx context.Context, msg *types.MsgUpdatePolicy) (*types.MsgUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update conversion policy message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.applyConversionPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}

	return &types.MsgUpdatePolicyResponse{}, nil
}

// CommitteeUpdatePolicy applies one complete capacity update as the
// exact appointed committee, during the active term and window, with every
// field inside the mandate's corridor.
//
// This is the emergency path: resizing depth and the recovery period is the
// peg-defence action a depeg reaches for, and a governance voting period is
// longer than the window in which it matters.
func (m msgServer) CommitteeUpdatePolicy(ctx context.Context, msg *types.MsgCommitteeUpdatePolicy) (*types.MsgCommitteeUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee update conversion policy message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	conversionMandate, err := m.k.ConversionMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting conversion mandate: %w", err)
	}
	if err := conversionMandate.Authorise(
		msg.Committee,
		msg.ExpectedTerm,
		uint64(sdkCtx.BlockHeight()),
	); err != nil {
		return nil, fmt.Errorf("%s: %w", types.ConversionMandateLabel, err)
	}
	if err := conversionMandate.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}
	if err := m.k.applyConversionPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeUpdatePolicyResponse{}, nil
}

// CommitteeRaiseTobinTax creates or replaces one per-denomination Tobin
// override as the exact appointed committee, during the active term and
// window, at or above the denomination's current effective rate and at most
// the mandate's cap.
//
// This is the per-denomination containment rung between the chain-wide depth
// throttle and an asset suspension: when one market's plausible oracle error
// outgrows its rate, the committee widens that buffer without freezing the
// asset. The raise persists across mandate replacement the way an applied
// policy does; only governance lowers or removes it.
func (m msgServer) CommitteeRaiseTobinTax(ctx context.Context, msg *types.MsgCommitteeRaiseTobinTax) (*types.MsgCommitteeRaiseTobinTaxResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee raise tobin tax message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	conversionMandate, err := m.k.ConversionMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting conversion mandate: %w", err)
	}
	if err := conversionMandate.Authorise(
		msg.Committee,
		msg.ExpectedTerm,
		uint64(sdkCtx.BlockHeight()),
	); err != nil {
		return nil, fmt.Errorf("%s: %w", types.ConversionMandateLabel, err)
	}
	effective, err := m.k.GetTobinTax(ctx, msg.Denom)
	if err != nil {
		return nil, err
	}
	if err := conversionMandate.ValidateTobinRaise(effective, msg.TobinTax); err != nil {
		return nil, err
	}
	if err := m.k.SetTobinTaxOverride(ctx, msg.Denom, msg.TobinTax); err != nil {
		return nil, err
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTobinTaxOverrideSet{
		Denom:    msg.Denom,
		TobinTax: msg.TobinTax,
	}); err != nil {
		return nil, fmt.Errorf("emitting Market tobin tax override: %w", err)
	}

	return &types.MsgCommitteeRaiseTobinTaxResponse{}, nil
}
