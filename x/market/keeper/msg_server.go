// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/keeper/msg_server.go.
// Modified for Ark: message handling and authority checks.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"errors"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/market/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an implementation of the market MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// Sell validates the trader address and sells the offer back to the same account.
func (m msgServer) Sell(ctx context.Context, msg *types.MsgSell) (*types.MsgSellResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, errorsmod.Wrapf(errortypes.ErrInvalidAddress, "trader is invalid: %s", err)
	}

	swapCoin, swapFee, err := m.k.Sell(ctx, addr, msg.OfferCoin, msg.AskDenom, msg.MinimumReceive)
	if err != nil {
		return nil, err
	}

	return &types.MsgSellResponse{
		SwapCoin: swapCoin,
		SwapFee:  swapFee,
	}, nil
}

// Buy validates the trader address and converts the least sufficient offer back to the same account.
func (m msgServer) Buy(ctx context.Context, msg *types.MsgBuy) (*types.MsgBuyResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, errorsmod.Wrapf(errortypes.ErrInvalidAddress, "trader is invalid: %s", err)
	}

	offerCoin, swapCoin, swapFee, err := m.k.Buy(ctx, addr, msg.AskCoin, msg.OfferDenom, msg.MaximumOffer)
	if err != nil {
		return nil, err
	}

	return &types.MsgBuyResponse{
		OfferCoin: offerCoin,
		SwapCoin:  swapCoin,
		SwapFee:   swapFee,
	}, nil
}

// Settle validates the trader address and executes a settlement redemption.
func (m msgServer) Settle(ctx context.Context, msg *types.MsgSettle) (*types.MsgSettleResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Trader)
	if err != nil {
		return nil, errorsmod.Wrapf(errortypes.ErrInvalidAddress, "trader is invalid: %s", err)
	}

	redeemed, err := m.k.Settle(ctx, addr, msg.OfferCoin)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "settling %s", msg.OfferCoin)
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
	envelope, committeeAddress, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationTime, msg.ExpiryTime)
	if err != nil {
		return nil, err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(
			m.k.accountKeeper.GetAccount(ctx, committeeAddress),
			m.k.wasmKeeper.HasContractInfo(ctx, committeeAddress),
		)
	}

	updated := types.NewDisabledConversionMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		updated.MinimumPolicy = msg.MinimumPolicy
		updated.MaximumPolicy = msg.MaximumPolicy
		updated.MaxTobinTax = msg.MaxTobinTax
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		// Governance already holds the unbounded path, so appointing itself delegates nothing.
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("conversion committee must be distinct from the Market authority")
		}
		// An expired window authorises nothing and would only displace the incumbent and advance the
		// term. Genesis skips this check, so an export taken after expiry round-trips.
		if now := chain.BlockSeconds(ctx); updated.ExpiryTime <= now {
			return nil, fmt.Errorf(
				"%s expires at time %d, which is not after the current block time %d",
				types.ConversionMandateLabel,
				updated.ExpiryTime,
				now,
			)
		}
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
		if updated.MaxTobinTax.IsPositive() {
			params, err := m.k.Params.Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("getting params: %w", err)
			}
			if updated.MaxTobinTax.LT(params.DefaultTobinTax) {
				return nil, fmt.Errorf(
					"conversion mandate Tobin cap %s is below the default rate %s, which delegates an empty band",
					updated.MaxTobinTax,
					params.DefaultTobinTax,
				)
			}
		}
	}

	if err := m.k.ConversionMandate.Set(ctx, updated); err != nil {
		return nil, fmt.Errorf("setting conversion mandate: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventConversionMandateSet{
		Term:           updated.Term,
		Committee:      updated.Committee,
		ActivationTime: updated.ActivationTime,
		ExpiryTime:     updated.ExpiryTime,
		CommitteeShape: updated.CommitteeShape,
	}); err != nil {
		return nil, fmt.Errorf("emitting conversion mandate: %w", err)
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

// CommitteeUpdatePolicy applies a complete conversion policy under the live committee term and
// window, with every field inside its approved corridor.
func (m msgServer) CommitteeUpdatePolicy(ctx context.Context, msg *types.MsgCommitteeUpdatePolicy) (*types.MsgCommitteeUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee update conversion policy message")
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}

	conversionMandate, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := conversionMandate.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}
	if err := m.k.applyConversionPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeUpdatePolicyResponse{}, nil
}

// CommitteeSetTobinTax creates or replaces one per-denomination Tobin override
// as the exact appointed committee, during the active term and window, inside
// the band between the chain-wide default and the mandate's cap.
func (m msgServer) CommitteeSetTobinTax(ctx context.Context, msg *types.MsgCommitteeSetTobinTax) (*types.MsgCommitteeSetTobinTaxResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee set tobin tax message")
	}
	conversionMandate, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}

	params, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	if err := conversionMandate.ValidateTobinCandidate(params.DefaultTobinTax, msg.TobinTax); err != nil {
		return nil, err
	}

	if err := m.k.SetTobinTaxOverride(ctx, msg.Denom, msg.TobinTax); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeSetTobinTaxResponse{}, nil
}
