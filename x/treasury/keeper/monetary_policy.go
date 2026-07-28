package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
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
	term, err := current.NextTerm()
	if err != nil {
		return nil, err
	}
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

// UpdateMonetaryPolicy applies one complete reversible policy update as the
// governance authority. Governance overrides the committee mandate, so no term,
// window, or policy-bound check applies.
func (m msgServer) UpdateMonetaryPolicy(ctx context.Context, msg *types.MsgUpdateMonetaryPolicy) (*types.MsgUpdateMonetaryPolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update monetary-policy message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.applyMonetaryPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}
	return &types.MsgUpdateMonetaryPolicyResponse{}, nil
}

// CommitteeUpdateMonetaryPolicy applies one complete reversible policy update
// as the exact appointed committee, during the active term and window, with
// every field inside the mandate's bounds.
func (m msgServer) CommitteeUpdateMonetaryPolicy(ctx context.Context, msg *types.MsgCommitteeUpdateMonetaryPolicy) (*types.MsgCommitteeUpdateMonetaryPolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee update monetary-policy message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
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
	if mandate.Committee == "" || msg.Committee != mandate.Committee {
		return nil, errors.New("signer is not the exact monetary-policy committee")
	}
	if err := mandate.RequireTerm(msg.ExpectedTerm); err != nil {
		return nil, err
	}
	if !mandate.IsActive(uint64(sdkCtx.BlockHeight())) {
		return nil, errors.New("monetary mandate is not active")
	}
	if err := mandate.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}
	if err := m.k.applyMonetaryPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeUpdateMonetaryPolicyResponse{}, nil
}

// applyMonetaryPolicy validates one authorized candidate policy against live
// reward-funding capacity and stores it.
func (k *Keeper) applyMonetaryPolicy(ctx context.Context, policy types.MonetaryPolicy) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	funding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting reward funding state: %w", err)
	}
	if err := types.ValidateRewardTargetCapacity(params, funding, policy); err != nil {
		return err
	}
	if err := k.MonetaryPolicy.Set(ctx, policy); err != nil {
		return fmt.Errorf("setting monetary policy: %w", err)
	}
	return nil
}
