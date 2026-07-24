package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/treasury/types"
)

// SetMonetaryMandate replaces or disables the bounded committee
// appointment. Treasury derives a new term for every replacement.
func (m msgServer) SetMonetaryMandate(ctx context.Context, msg *types.MsgSetMonetaryMandate) (*types.MsgSetMonetaryMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set monetary mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	term := current.Term + 1
	mandate := types.NewDisabledMonetaryMandate(term)
	if msg.Committee != "" {
		mandate.Committee = msg.Committee
		mandate.ActivationHeight = msg.ActivationHeight
		mandate.ExpiryHeight = msg.ExpiryHeight
		mandate.MinimumPolicy = msg.MinimumPolicy
		mandate.MaximumPolicy = msg.MaximumPolicy
		if err := mandate.Validate(); err != nil {
			return nil, err
		}
		if sdk.ValidateAuthority(sdkCtx, m.k.authority, mandate.Committee) == nil {
			return nil, errors.New("monetary-policy committee must be distinct from Treasury authority")
		}
		claimsMandate, err := m.k.ClaimsMandate.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting Claims mandate: %w", err)
		}
		if mandate.Committee == claimsMandate.Committee {
			return nil, errors.New("monetary-policy committee must be distinct from Claims committee")
		}
	}

	if err := m.k.MonetaryMandate.Set(ctx, mandate); err != nil {
		return nil, fmt.Errorf("setting monetary mandate: %w", err)
	}
	return &types.MsgSetMonetaryMandateResponse{}, nil
}

// UpdateMonetaryPolicy applies one complete reversible policy update. The
// effective governance authority may override the committee mandate; all other
// signers must match the current bounded committee appointment exactly.
func (m msgServer) UpdateMonetaryPolicy(ctx context.Context, msg *types.MsgUpdateMonetaryPolicy) (*types.MsgUpdateMonetaryPolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update monetary-policy message")
	}
	if _, err := types.ParseCanonicalAccountAddress("monetary-policy signer", msg.Signer); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	mandate, err := m.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	if sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Signer) != nil {
		if mandate.Committee == "" || msg.Signer != mandate.Committee {
			return nil, errors.New("signer is neither Treasury authority nor the exact monetary-policy committee")
		}
		if msg.ExpectedTerm != mandate.Term {
			return nil, fmt.Errorf("monetary mandate term mismatch: expected %d, got %d", mandate.Term, msg.ExpectedTerm)
		}
		if !mandate.IsActive(sdkCtx.BlockHeight()) {
			return nil, errors.New("monetary mandate is not active")
		}
		if err := mandate.ValidatePolicy(msg.Policy); err != nil {
			return nil, err
		}
	}
	params, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	funding, err := m.k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward funding state: %w", err)
	}
	if err := types.ValidateRewardTargetCapacity(params, funding, msg.Policy); err != nil {
		return nil, err
	}

	if err := m.k.MonetaryPolicy.Set(ctx, msg.Policy); err != nil {
		return nil, fmt.Errorf("setting monetary policy: %w", err)
	}
	return &types.MsgUpdateMonetaryPolicyResponse{}, nil
}
