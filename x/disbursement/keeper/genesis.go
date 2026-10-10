package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// InitGenesis imports verified obligations and rebuilds the grant indexes. It checks what Validate
// cannot see before it writes module state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if gs == nil {
		return errors.New("nil disbursement genesis")
	}
	if err := gs.Validate(); err != nil {
		return fmt.Errorf("invalid disbursement genesis state: %w", err)
	}
	// Governance holds the unbounded path already, so an appointment naming the authority delegates
	// nothing; the message path re-checks every later replacement.
	if gs.GrantsMandate.Committee == k.authority {
		return errors.New("grants committee must be distinct from the disbursement authority")
	}
	if gs.RegistrarMandate.Committee == k.authority {
		return errors.New("registrar must be distinct from the disbursement authority")
	}
	// InitChain runs at height zero for a chain starting at one, and seat assembly stamps the
	// founder's awards at the first block, so the later of the two bounds the stamps, as in Oracle.
	at, height := chain.BlockSeconds(ctx), uint64(max(sdk.UnwrapSDKContext(ctx).BlockHeight(), 1))
	// Validate ties every grant clock to a journal stamp, so these bound the grants too.
	for _, e := range gs.GrantJournal {
		if e.Time > at || e.Height > height {
			return fmt.Errorf("grant entry %d is in the future", e.Id)
		}
	}
	for _, e := range gs.BeneficiaryJournal {
		if e.Time > at || e.Height > height {
			return fmt.Errorf("beneficiary entry %d is in the future", e.Id)
		}
	}
	// A payee Bank blocks imports: the blocked list can change between an export and its relaunch,
	// and the block defers the payment as it would any refused send. A pool account does not: no
	// message can name one, and paying a pool to itself would journal principal as paid while it
	// stayed in custody.
	for _, g := range gs.Grants {
		for _, address := range []string{g.Beneficiary, g.Payee} {
			parsed, err := chain.ParseCanonicalAccountAddress("grant address", address)
			if err != nil {
				return fmt.Errorf("grant %d: %w", g.Id, err)
			}
			if pool, isPool := k.poolAddresses[string(parsed)]; isPool {
				return fmt.Errorf("grant %d names the %s account", g.Id, pool)
			}
		}
		earned, _, err := g.Accrued(at)
		if err != nil {
			return err
		}
		if g.Paid.GT(earned) {
			return fmt.Errorf("grant %d paid past genesis time", g.Id)
		}
		if g.Amount.Denom != chain.NoahBaseDenom {
			registered, err := k.assetKeeper.HasAsset(ctx, g.Amount.Denom)
			if err != nil {
				return fmt.Errorf("grant %d pays %s: checking the asset registry: %w", g.Id, g.Amount.Denom, err)
			}
			if !registered {
				return fmt.Errorf("grant %d pays %s: not a registered asset", g.Id, g.Amount.Denom)
			}
		}
	}
	// Bank genesis bypasses the send restriction, so import holds each account to its admission rule;
	// registry membership is permanent, so an export cannot fail it. Bank must also back each
	// account's commitments: its reservations and, in the member account, the open tranche.
	committed := make(map[string]sdk.Coins, len(gs.Books))
	for _, b := range gs.Books {
		committed[b.Account] = b.Reserved
	}
	committed[types.MembersName] = committed[types.MembersName].Add(chain.NoahCoin(gs.MemberTranche))
	for _, account := range types.PoolAccountNames() {
		if k.accountKeeper.GetModuleAccount(ctx, account) == nil {
			return fmt.Errorf("%s module account has not been set", account)
		}
		balances := k.bankKeeper.GetAllBalances(ctx, k.accountKeeper.GetModuleAddress(account))
		for _, coin := range balances {
			if coin.Denom == chain.NoahBaseDenom {
				continue
			}
			if account == types.MembersName {
				return fmt.Errorf("the member account holds %s; it pays NOAH only", coin.Denom)
			}
			registered, err := k.assetKeeper.HasAsset(ctx, coin.Denom)
			if err != nil {
				return fmt.Errorf("the contributor account holds %s: checking the asset registry: %w", coin.Denom, err)
			}
			if !registered {
				return fmt.Errorf("the contributor account holds %s: not a registered asset", coin.Denom)
			}
		}
		if !balances.IsAllGTE(committed[account]) {
			return fmt.Errorf("%s commitments %s are not backed by Bank", account, committed[account])
		}
	}

	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}
	if err := k.NextGrantID.Set(ctx, gs.NextGrantId); err != nil {
		return err
	}
	if err := k.NextGrantEntryID.Set(ctx, gs.NextGrantEntryId); err != nil {
		return err
	}
	if err := k.NextBeneficiaryEntryID.Set(ctx, gs.NextBeneficiaryEntryId); err != nil {
		return err
	}
	for _, p := range gs.Beneficiaries {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", p.Address)
		if err != nil {
			return err
		}
		if err := k.Beneficiaries.Set(ctx, address, p); err != nil {
			return err
		}
	}
	for _, g := range gs.Grants {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return err
		}
		if err := k.Grants.Set(ctx, g.Id, g); err != nil {
			return err
		}
		if err := k.index(ctx, address, g); err != nil {
			return err
		}
		if g.DueTime != 0 {
			if err := k.Due.Set(ctx, collections.Join(g.DueTime, g.Id)); err != nil {
				return err
			}
		}
	}
	for _, b := range gs.Books {
		if err := k.Books.Set(ctx, b.Account, b); err != nil {
			return err
		}
	}
	// The mandates import verbatim, expired included: an export taken after expiry must round-trip.
	if err := k.GrantsMandate.Set(ctx, gs.GrantsMandate); err != nil {
		return err
	}
	if err := k.CompensationUsed.Set(ctx, gs.CompensationUsed); err != nil {
		return err
	}
	if err := k.MemberTranche.Set(ctx, gs.MemberTranche); err != nil {
		return err
	}
	if err := k.RegistrarMandate.Set(ctx, gs.RegistrarMandate); err != nil {
		return err
	}
	for _, e := range gs.GrantJournal {
		if err := k.GrantJournal.Set(ctx, collections.Join(e.GrantId, e.Id), e); err != nil {
			return err
		}
	}
	for _, e := range gs.BeneficiaryJournal {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary entry", e.Beneficiary)
		if err != nil {
			return err
		}
		if err := k.BeneficiaryJournal.Set(ctx, collections.Join(address, e.Id), e); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis preserves absolute clocks and audit history; Bank exports the custody coins.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesisState()
	var err error
	gs.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextGrantId, err = k.NextGrantID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextGrantEntryId, err = k.NextGrantEntryID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	gs.NextBeneficiaryEntryId, err = k.NextBeneficiaryEntryID.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Beneficiaries.Walk(ctx, nil, func(_ sdk.AccAddress, p types.Beneficiary) (bool, error) {
		gs.Beneficiaries = append(gs.Beneficiaries, p)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
		gs.Grants = append(gs.Grants, g)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Books.Walk(ctx, nil, func(_ string, b types.AccountBook) (bool, error) {
		gs.Books = append(gs.Books, b)
		return false, nil
	}); err != nil {
		return nil, err
	}
	gs.GrantsMandate, err = k.GrantsMandate.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.CompensationUsed, err = k.CompensationUsed.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.MemberTranche, err = k.MemberTranche.Get(ctx)
	if err != nil {
		return nil, err
	}
	gs.RegistrarMandate, err = k.RegistrarMandate.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.GrantJournal.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], e types.GrantEntry) (bool, error) {
		gs.GrantJournal = append(gs.GrantJournal, e)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.BeneficiaryJournal.Walk(ctx, nil, func(_ collections.Pair[sdk.AccAddress, uint64], e types.BeneficiaryEntry) (bool, error) {
		gs.BeneficiaryJournal = append(gs.BeneficiaryJournal, e)
		return false, nil
	}); err != nil {
		return nil, err
	}
	return gs, nil
}
