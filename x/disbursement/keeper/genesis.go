package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// InitGenesis imports verified obligations and rebuilds derived indexes and founder totals.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if gs == nil {
		return fmt.Errorf("nil disbursement genesis")
	}
	if err := gs.Validate(); err != nil {
		return err
	}
	for _, t := range gs.Totals {
		if k.bank.GetBalance(ctx, k.address, t.Denom).Amount.LT(t.Reserved) {
			return fmt.Errorf("%s disbursement reservation is not backed by Bank", t.Denom)
		}
		if err := k.Totals.Set(ctx, t.Denom, t); err != nil {
			return err
		}
	}
	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	if err := k.OwnershipPolicy.Set(ctx, gs.OwnershipPolicy); err != nil {
		return err
	}
	if err := k.NextGrantID.Set(ctx, gs.NextGrantId); err != nil {
		return err
	}
	if err := k.NextJournalID.Set(ctx, gs.NextJournalId); err != nil {
		return err
	}
	if err := k.Issuance.Set(ctx, gs.Issuance); err != nil {
		return err
	}
	founders := map[string]bool{}
	founding, paid, remaining := math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	for _, f := range gs.Founders {
		operator, err := chain.ParseCanonicalValidatorAddress("founder", f.Operator)
		if err != nil {
			return err
		}
		if err := k.Founders.Set(ctx, operator, f); err != nil {
			return err
		}
		founders[f.Beneficiary] = true
		founding, err = founding.SafeAdd(f.SeatAmount)
		if err != nil {
			return err
		}
	}
	for _, p := range gs.Beneficiaries {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", p.Address)
		if err != nil {
			return err
		}
		if err := k.Beneficiaries.Set(ctx, address, p); err != nil {
			return err
		}
		if founders[p.Address] {
			paid, err = paid.SafeAdd(p.OwnershipPaid)
			if err != nil {
				return err
			}
		}
	}
	var at uint64
	if len(gs.Grants)+len(gs.Journal)+len(gs.Issuance.Entries) > 0 {
		var err error
		at, err = blockTime(ctx)
		if err != nil {
			return err
		}
	}
	for _, e := range gs.Issuance.Entries {
		if e.At > at {
			return fmt.Errorf("issuance is in the future")
		}
	}
	for _, g := range gs.Grants {
		if g.StartTime > at || (g.Cancelled && g.CutoffTime > at) || (g.Suspension != nil && g.Suspension.At > at) {
			return fmt.Errorf("grant %d has future state", g.Id)
		}
		earned, _, err := types.Accrued(g, at)
		if err != nil {
			return err
		}
		if g.Paid.GT(earned) {
			return fmt.Errorf("grant %d paid past genesis time", g.Id)
		}
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return err
		}
		if err := k.Grants.Set(ctx, g.Id, g); err != nil {
			return err
		}
		if err := k.GrantsByBeneficiary.Set(ctx, collections.Join(address, g.Id), true); err != nil {
			return err
		}
		if g.Kind == types.GrantKind_GRANT_KIND_MEMBER {
			if err := k.Members.Set(ctx, address, g.Id); err != nil {
				return err
			}
		}
		if founders[g.Beneficiary] && g.Kind == types.GrantKind_GRANT_KIND_OWNERSHIP {
			remaining, err = remaining.SafeAdd(g.Remaining)
			if err != nil {
				return err
			}
		}
	}
	// The mandate imports verbatim, expired included: an export taken after expiry must round-trip.
	if err := k.RegistrarMandate.Set(ctx, gs.RegistrarMandate); err != nil {
		return err
	}
	for _, term := range gs.VoidedTerms {
		if err := k.VoidedTerms.Set(ctx, term); err != nil {
			return err
		}
	}
	for _, e := range gs.Journal {
		if e.Time > at {
			return fmt.Errorf("journal entry %d is in the future", e.Id)
		}
		if err := k.Journal.Set(ctx, e.Id, e); err != nil {
			return err
		}
		if e.GrantId != 0 {
			if err := k.JournalByGrant.Set(ctx, collections.Join(e.GrantId, e.Id), true); err != nil {
				return err
			}
		}
	}
	if err := k.FoundingStake.Set(ctx, founding); err != nil {
		return err
	}
	if err := k.FounderPaid.Set(ctx, paid); err != nil {
		return err
	}
	return k.FounderRemaining.Set(ctx, remaining)
}

// ExportGenesis preserves absolute clocks and audit history; Bank exports the custody coins.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesisState()
	var err error
	gs.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.OwnershipPolicy, err = k.OwnershipPolicy.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextGrantId, err = k.NextGrantID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextJournalId, err = k.NextJournalID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs.Issuance, err = k.Issuance.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Founders.Walk(ctx, nil, func(_ sdk.ValAddress, f types.Founder) (bool, error) {
		gs.Founders = append(gs.Founders, f)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Beneficiaries.Walk(ctx, nil, func(_ sdk.AccAddress, p types.Beneficiary) (bool, error) {
		gs.Beneficiaries = append(gs.Beneficiaries, p)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) { gs.Grants = append(gs.Grants, g); return false, nil }); err != nil {
		return nil, err
	}
	if err := k.Totals.Walk(ctx, nil, func(_ string, t types.DenomTotals) (bool, error) { gs.Totals = append(gs.Totals, t); return false, nil }); err != nil {
		return nil, err
	}
	gs.RegistrarMandate, err = k.RegistrarMandate.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.VoidedTerms.Walk(ctx, nil, func(term uint64) (bool, error) {
		gs.VoidedTerms = append(gs.VoidedTerms, term)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Journal.Walk(ctx, nil, func(_ uint64, e types.JournalEntry) (bool, error) {
		gs.Journal = append(gs.Journal, e)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return gs, nil
}
