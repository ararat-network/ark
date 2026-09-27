package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	k *Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl exposes the disbursement transaction handlers.
func NewMsgServerImpl(k *Keeper) types.MsgServer { return msgServer{k: k} }

func (m msgServer) authority(ctx context.Context, address string) error {
	return sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, address)
}

func addressesBatch(addresses []string) ([]sdk.AccAddress, error) {
	if len(addresses) == 0 || len(addresses) > types.MaxBatch {
		return nil, fmt.Errorf("batch must contain 1..%d addresses", types.MaxBatch)
	}
	out := make([]sdk.AccAddress, len(addresses))
	seen := map[string]bool{}
	for i, a := range addresses {
		parsed, err := chain.ParseCanonicalAccountAddress("member", a)
		if err != nil {
			return nil, err
		}
		if seen[a] {
			return nil, errors.New("duplicate member in batch")
		}
		seen[a] = true
		out[i] = parsed
	}
	return out, nil
}

func idsBatch(ids []uint64) error {
	if len(ids) == 0 || len(ids) > types.MaxBatch {
		return fmt.Errorf("batch must contain 1..%d grants", types.MaxBatch)
	}
	seen := map[uint64]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			return errors.New("zero or duplicate grant ID")
		}
		seen[id] = true
	}
	return nil
}

func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	previous, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	for _, denom := range msg.Params.CompensationDenoms {
		if !slices.Contains(previous.CompensationDenoms, denom) {
			if err := m.k.validateCompensation(ctx, denom, msg.Params); err != nil {
				return nil, err
			}
		}
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "params", msg.Authority, chain.NoahCoin(math.ZeroInt()), msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

func (m msgServer) SetGrantsMandate(ctx context.Context, msg *types.MsgSetGrantsMandate) (*types.MsgSetGrantsMandateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.SetGrantsMandate(ctx, msg); err != nil {
		return nil, err
	}
	return &types.MsgSetGrantsMandateResponse{}, nil
}

func (m msgServer) CreateGrant(ctx context.Context, msg *types.MsgCreateGrant) (*types.MsgCreateGrantResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	beneficiary, err := chain.ParseCanonicalAccountAddress("beneficiary", msg.Beneficiary)
	if err != nil {
		return nil, err
	}
	switch msg.Kind {
	case types.GrantKind_GRANT_KIND_OWNERSHIP:
		if msg.Amount.Denom != chain.NoahBaseDenom {
			return nil, errors.New("ownership grants require NOAH")
		}
	case types.GrantKind_GRANT_KIND_COMPENSATION:
		p, err := m.k.Params.Get(ctx)
		if err != nil {
			return nil, err
		}
		if err := m.k.validateCompensation(ctx, msg.Amount.Denom, p); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("governance creates ownership or compensation grants")
	}
	id, err := m.k.create(ctx, msg.Authority, msg.Kind, beneficiary, msg.Amount, msg.Schedule, msg.Reference, 0)
	if err != nil {
		return nil, err
	}
	return &types.MsgCreateGrantResponse{GrantId: id}, nil
}

// issuanceWindow drops expired entries without mutating the stored history on reads.
func (k *Keeper) issuanceWindow(ctx context.Context, p types.Params) (types.Issuance, uint64, error) {
	log, err := k.Issuance.Get(ctx)
	if err != nil {
		return log, 0, err
	}
	now, err := blockTime(ctx)
	if err != nil {
		return log, 0, err
	}
	var used uint64
	entries := make([]types.IssuanceEntry, 0, len(log.Entries))
	for _, e := range log.Entries {
		if e.At > now {
			return log, 0, errors.New("issuance entry is in the future")
		}
		if now-e.At < p.WindowSeconds {
			entries = append(entries, e)
			used += e.Count
		}
	}
	return types.Issuance{Entries: entries}, used, nil
}

func (m msgServer) CommitteeRegister(ctx context.Context, msg *types.MsgCommitteeRegister) (*types.MsgCommitteeRegisterResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	p, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		exists, err := m.k.Members.Has(ctx, address)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New("member already registered")
		}
	}
	log, used, err := m.k.issuanceWindow(ctx, p)
	if err != nil {
		return nil, err
	}
	if used+uint64(len(addresses)) > p.MaxMembers {
		return nil, errors.New("rolling member issuance limit exceeded")
	}
	needed, err := p.MemberAmount.SafeMul(math.NewInt(int64(len(addresses))))
	if err != nil {
		return nil, err
	}
	pool, err := m.k.MemberPool.Get(ctx)
	if err != nil {
		return nil, err
	}
	if pool.Open.LT(needed) {
		return nil, errors.New("member batch exceeds the open member tranche")
	}
	now, err := blockTime(ctx)
	if err != nil {
		return nil, err
	}
	if n := len(log.Entries); n > 0 && log.Entries[n-1].At == now {
		log.Entries[n-1].Count += uint64(len(addresses))
	} else {
		log.Entries = append(log.Entries, types.IssuanceEntry{At: now, Count: uint64(len(addresses))})
	}
	if err := m.k.Issuance.Set(ctx, log); err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(addresses))
	for _, address := range addresses {
		id, err := m.k.create(ctx, appointment.Committee, types.GrantKind_GRANT_KIND_MEMBER, address, chain.NoahCoin(p.MemberAmount), p.MemberSchedule, "", 0)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return &types.MsgCommitteeRegisterResponse{GrantIds: ids}, nil
}

func (m msgServer) Release(ctx context.Context, msg *types.MsgRelease) (*types.MsgReleaseResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("sender", msg.Sender); err != nil {
		return nil, err
	}
	if err := idsBatch(msg.GrantIds); err != nil {
		return nil, err
	}
	paid := false
	for _, id := range msg.GrantIds {
		g, err := m.k.Grants.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		ok, err := m.k.pay(ctx, msg.Sender, g)
		if err != nil {
			return nil, err
		}
		paid = paid || ok
	}
	if !paid {
		return nil, errors.New("nothing is payable")
	}
	return &types.MsgReleaseResponse{}, nil
}

func (m msgServer) CancelGrants(ctx context.Context, msg *types.MsgCancelGrants) (*types.MsgCancelGrantsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := idsBatch(msg.GrantIds); err != nil {
		return nil, err
	}
	for _, id := range msg.GrantIds {
		g, err := m.k.Grants.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := m.k.cancel(ctx, msg.Authority, g); err != nil {
			return nil, err
		}
	}
	return &types.MsgCancelGrantsResponse{}, nil
}

func (k *Keeper) liveMember(ctx context.Context, address sdk.AccAddress) (types.Grant, error) {
	id, err := k.Members.Get(ctx, address)
	if err != nil {
		return types.Grant{}, err
	}
	g, err := k.Grants.Get(ctx, id)
	if err != nil {
		return g, err
	}
	if g.Cancelled || g.Remaining.IsZero() {
		return g, errors.New("member grant is closed")
	}
	return g, nil
}

func (m msgServer) CommitteeSuspend(ctx context.Context, msg *types.MsgCommitteeSuspend) (*types.MsgCommitteeSuspendResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	now, err := blockTime(ctx)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		g, err := m.k.liveMember(ctx, address)
		if err != nil {
			return nil, err
		}
		suspended, err := m.k.suspended(ctx, g)
		if err != nil {
			return nil, err
		}
		if suspended {
			return nil, errors.New("member already suspended")
		}
		g.Suspension = &types.Suspension{At: now, Term: appointment.Term}
		if err := m.k.Grants.Set(ctx, g.Id, g); err != nil {
			return nil, err
		}
		if err := m.k.record(ctx, g.Id, "suspended", appointment.Committee, sdk.NewCoin(g.Amount.Denom, math.ZeroInt()), g.Suspension); err != nil {
			return nil, err
		}
	}
	return &types.MsgCommitteeSuspendResponse{}, nil
}

func (m msgServer) CommitteeReinstate(ctx context.Context, msg *types.MsgCommitteeReinstate) (*types.MsgCommitteeReinstateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := m.reinstate(ctx, appointment.Committee, msg.Addresses); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeReinstateResponse{}, nil
}

func (m msgServer) ReinstateMembers(ctx context.Context, msg *types.MsgReinstateMembers) (*types.MsgReinstateMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.reinstate(ctx, msg.Authority, msg.Addresses); err != nil {
		return nil, err
	}
	return &types.MsgReinstateMembersResponse{}, nil
}

// reinstate clears effective suspensions; the committee and governance share the action under
// their own authorisation.
func (m msgServer) reinstate(ctx context.Context, actor string, batch []string) error {
	addresses, err := addressesBatch(batch)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		g, err := m.k.liveMember(ctx, address)
		if err != nil {
			return err
		}
		suspended, err := m.k.suspended(ctx, g)
		if err != nil {
			return err
		}
		if !suspended {
			return errors.New("member is not suspended")
		}
		g.Suspension = nil
		if err := m.k.Grants.Set(ctx, g.Id, g); err != nil {
			return err
		}
		if err := m.k.record(ctx, g.Id, "reinstated", actor, sdk.NewCoin(g.Amount.Denom, math.ZeroInt()), nil); err != nil {
			return err
		}
	}
	return nil
}

func (m msgServer) VoidSuspensions(ctx context.Context, msg *types.MsgVoidSuspensions) (*types.MsgVoidSuspensionsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	current, err := m.k.GrantsMandate.Get(ctx)
	if err != nil {
		return nil, err
	}
	// The live term is never voided: re-appoint the committee first, which advances the term, so a
	// suspension made after the void lands under a term the void does not touch.
	if msg.Term == 0 || msg.Term >= current.Term {
		return nil, errors.New("only a replaced committee term can be voided")
	}
	voided, err := m.k.VoidedTerms.Has(ctx, msg.Term)
	if err != nil {
		return nil, err
	}
	if voided {
		return nil, errors.New("committee term already voided")
	}
	if err := m.k.VoidedTerms.Set(ctx, msg.Term); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "void_suspensions", msg.Authority, chain.NoahCoin(math.ZeroInt()), map[string]uint64{"term": msg.Term}); err != nil {
		return nil, err
	}
	return &types.MsgVoidSuspensionsResponse{}, nil
}

func (m msgServer) SetPayee(ctx context.Context, msg *types.MsgSetPayee) (*types.MsgSetPayeeResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	g, err := m.k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		return nil, err
	}
	if g.Remaining.IsZero() {
		return nil, errors.New("grant has no remaining entitlement")
	}
	controller := g.Beneficiary
	if g.Kind != types.GrantKind_GRANT_KIND_MEMBER {
		address, err := chain.ParseCanonicalAccountAddress("beneficiary", g.Beneficiary)
		if err != nil {
			return nil, err
		}
		p, err := m.k.Beneficiaries.Get(ctx, address)
		if err != nil {
			return nil, err
		}
		controller = p.Controller
	}
	if msg.Sender != controller {
		return nil, errors.New("only the member or contributor controller may change the payee")
	}
	payee, err := chain.ParseCanonicalAccountAddress("payee", msg.Payee)
	if err != nil {
		return nil, err
	}
	if m.k.bank.BlockedAddr(payee) {
		return nil, errors.New("payee cannot receive funds")
	}
	previous := g.Payee
	g.Payee = msg.Payee
	if err := m.k.Grants.Set(ctx, g.Id, g); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, g.Id, "payee", msg.Sender, sdk.NewCoin(g.Amount.Denom, math.ZeroInt()), map[string]string{"previous": previous, "payee": g.Payee}); err != nil {
		return nil, err
	}
	return &types.MsgSetPayeeResponse{}, nil
}

func (m msgServer) SetController(ctx context.Context, msg *types.MsgSetController) (*types.MsgSetControllerResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", msg.Beneficiary)
	if err != nil {
		return nil, err
	}
	p, err := m.k.Beneficiaries.Get(ctx, address)
	if err != nil {
		return nil, err
	}
	if err := m.authority(ctx, msg.Sender); err != nil && msg.Sender != p.Controller {
		return nil, errors.New("only governance or the current controller may rotate control")
	}
	if _, err := chain.ParseCanonicalAccountAddress("controller", msg.Controller); err != nil {
		return nil, err
	}
	previous := p.Controller
	p.Controller = msg.Controller
	if err := m.k.Beneficiaries.Set(ctx, address, p); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "controller", msg.Sender, chain.NoahCoin(math.ZeroInt()), map[string]string{"beneficiary": p.Address, "previous": previous, "controller": p.Controller}); err != nil {
		return nil, err
	}
	return &types.MsgSetControllerResponse{}, nil
}

func (m msgServer) ReturnUnallocated(ctx context.Context, msg *types.MsgReturnUnallocated) (*types.MsgReturnUnallocatedResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Amount.Validate(); err != nil {
		return nil, err
	}
	if err := types.ValidateAmount(msg.Amount.Amount, true); err != nil {
		return nil, err
	}
	switch msg.Pool {
	case types.Pool_POOL_UNSPECIFIED:
		available, err := m.k.unallocated(ctx, msg.Amount.Denom)
		if err != nil {
			return nil, err
		}
		if available.LT(msg.Amount.Amount) {
			return nil, errors.New("return would consume committed funds")
		}
	case types.Pool_POOL_CONTRIBUTORS:
		if msg.Amount.Denom != chain.NoahBaseDenom {
			return nil, errors.New("the contributor pool holds NOAH")
		}
		p, err := m.k.ContributorPool.Get(ctx)
		if err != nil {
			return nil, err
		}
		if p.Unallocated.LT(msg.Amount.Amount) {
			return nil, errors.New("return exceeds the contributor pool")
		}
		// The unopened part goes first, so a return leaves the open tranche whole when it can.
		if unopened := p.Unallocated.Sub(p.Open); msg.Amount.Amount.GT(unopened) {
			p.Open = p.Open.Sub(msg.Amount.Amount.Sub(unopened))
		}
		p.Unallocated = p.Unallocated.Sub(msg.Amount.Amount)
		if err := m.k.ContributorPool.Set(ctx, p); err != nil {
			return nil, err
		}
	case types.Pool_POOL_MEMBERS:
		return nil, errors.New("the member pool has no exit")
	default:
		return nil, errors.New("unknown distribution pool")
	}
	if err := m.k.distribution.FundCommunityPool(ctx, sdk.NewCoins(msg.Amount), m.k.address); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "return_unallocated", msg.Authority, msg.Amount, map[string]string{"pool": msg.Pool.String()}); err != nil {
		return nil, err
	}
	return &types.MsgReturnUnallocatedResponse{}, nil
}

func (m msgServer) OpenTranche(ctx context.Context, msg *types.MsgOpenTranche) (*types.MsgOpenTrancheResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if err := types.ValidateAmount(msg.Amount, true); err != nil {
		return nil, err
	}
	item, err := m.k.pool(msg.Pool)
	if err != nil {
		return nil, err
	}
	p, err := item.Get(ctx)
	if err != nil {
		return nil, err
	}
	if p.Open, err = p.Open.SafeAdd(msg.Amount); err != nil {
		return nil, err
	}
	if p.Open.GT(p.Unallocated) {
		return nil, errors.New("tranche exceeds its pool")
	}
	if err := item.Set(ctx, p); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "open_tranche", msg.Authority, chain.NoahCoin(msg.Amount), map[string]string{"pool": msg.Pool.String()}); err != nil {
		return nil, err
	}
	return &types.MsgOpenTrancheResponse{}, nil
}

func (m msgServer) AuthoriseConversion(ctx context.Context, msg *types.MsgAuthoriseConversion) (*types.MsgAuthoriseConversionResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if msg.Denom == chain.NoahBaseDenom {
		return nil, errors.New("conversion orders sell NOAH for another denomination")
	}
	p, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.k.validateCompensation(ctx, msg.Denom, p); err != nil {
		return nil, err
	}
	if err := types.ValidateAmount(msg.Amount, true); err != nil {
		return nil, err
	}
	if err := m.k.draw(ctx, types.Pool_POOL_CONTRIBUTORS, chain.NoahCoin(msg.Amount)); err != nil {
		return nil, err
	}
	order, err := m.k.ConversionOrders.Get(ctx, msg.Denom)
	if errors.Is(err, collections.ErrNotFound) {
		order, err = types.ConversionOrder{Denom: msg.Denom, Remaining: math.ZeroInt()}, nil
	}
	if err != nil {
		return nil, err
	}
	if order.Remaining, err = order.Remaining.SafeAdd(msg.Amount); err != nil {
		return nil, err
	}
	order.MaxSpread = msg.MaxSpread
	if err := order.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.ConversionOrders.Set(ctx, order.Denom, order); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "authorise_conversion", msg.Authority, chain.NoahCoin(msg.Amount), map[string]string{"denom": order.Denom, "max_spread": order.MaxSpread.String()}); err != nil {
		return nil, err
	}
	return &types.MsgAuthoriseConversionResponse{}, nil
}

func (m msgServer) CancelConversion(ctx context.Context, msg *types.MsgCancelConversion) (*types.MsgCancelConversionResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	order, err := m.k.ConversionOrders.Get(ctx, msg.Denom)
	if err != nil {
		return nil, fmt.Errorf("conversion order %s: %w", msg.Denom, err)
	}
	if err := m.k.refund(ctx, types.Pool_POOL_CONTRIBUTORS, order.Remaining); err != nil {
		return nil, err
	}
	if err := m.k.ConversionOrders.Remove(ctx, order.Denom); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "cancel_conversion", msg.Authority, chain.NoahCoin(order.Remaining), map[string]string{"denom": order.Denom}); err != nil {
		return nil, err
	}
	return &types.MsgCancelConversionResponse{}, nil
}

func (m msgServer) Convert(ctx context.Context, msg *types.MsgConvert) (*types.MsgConvertResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("sender", msg.Sender); err != nil {
		return nil, err
	}
	// Proposals execute after Market's EndBlocker has settled the block, which would strand the escrow.
	if msg.Sender == m.k.authority {
		return nil, errors.New("governance authorises conversions; a transaction executes them")
	}
	if err := types.ValidateAmount(msg.Amount, true); err != nil {
		return nil, err
	}
	order, err := m.k.ConversionOrders.Get(ctx, msg.Denom)
	if err != nil {
		return nil, fmt.Errorf("conversion order %s: %w", msg.Denom, err)
	}
	if msg.Amount.GT(order.Remaining) {
		return nil, errors.New("amount exceeds the conversion order")
	}
	p, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.k.validateCompensation(ctx, msg.Denom, p); err != nil {
		return nil, err
	}
	output, fee, err := m.k.market.Swap(ctx, m.k.address, m.k.address, chain.NoahCoin(msg.Amount), msg.Denom, sdk.Coin{})
	if err != nil {
		return nil, err
	}
	// Output plus fee is the gross quote Market already formed, so neither the sum nor the product can
	// leave range; truncating the cap refuses a tie.
	gross := math.LegacyNewDecFromInt(output.Amount).Add(fee.Amount)
	if fee.Amount.GT(order.MaxSpread.MulTruncate(gross)) {
		return nil, fmt.Errorf("realised spread %s of %s exceeds the order's cap", fee, gross)
	}
	order.Remaining = order.Remaining.Sub(msg.Amount)
	if order.Remaining.IsZero() {
		err = m.k.ConversionOrders.Remove(ctx, order.Denom)
	} else {
		err = m.k.ConversionOrders.Set(ctx, order.Denom, order)
	}
	if err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "convert", msg.Sender, chain.NoahCoin(msg.Amount), map[string]string{"output": output.String(), "fee": fee.String()}); err != nil {
		return nil, err
	}
	return &types.MsgConvertResponse{Output: output, Fee: fee}, nil
}

func (m msgServer) CommitteeCompensate(ctx context.Context, msg *types.MsgCommitteeCompensate) (*types.MsgCommitteeCompensateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	beneficiary, err := chain.ParseCanonicalAccountAddress("beneficiary", msg.Beneficiary)
	if err != nil {
		return nil, err
	}
	person, err := m.k.Beneficiaries.Get(ctx, beneficiary)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	if err == nil && person.Seat.IsPositive() {
		return nil, errors.New("compensation to a seat holder is a governance decision")
	}
	// The delay lets governance cancel a stolen key's awards before anything accrues.
	if len(msg.Schedule) == 0 || msg.Schedule[0].Length < appointment.MinFirstPeriod {
		return nil, errors.New("first period is shorter than the mandate's minimum")
	}
	if err := msg.Amount.Validate(); err != nil {
		return nil, err
	}
	used, count, err := m.k.termUsage(ctx, appointment.Term)
	if err != nil {
		return nil, err
	}
	if count >= types.MaxTermGrants {
		return nil, fmt.Errorf("the term has made its %d awards", types.MaxTermGrants)
	}
	total, err := used.AmountOf(msg.Amount.Denom).SafeAdd(msg.Amount.Amount)
	if err != nil {
		return nil, err
	}
	if total.GT(appointment.CompensationAllowance.AmountOf(msg.Amount.Denom)) {
		return nil, errors.New("award exceeds the term's compensation allowance")
	}
	p, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.k.validateCompensation(ctx, msg.Amount.Denom, p); err != nil {
		return nil, err
	}
	id, err := m.k.create(ctx, appointment.Committee, types.GrantKind_GRANT_KIND_COMPENSATION, beneficiary, msg.Amount, msg.Schedule, msg.Reference, appointment.Term)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeCompensateResponse{GrantId: id}, nil
}

func (m msgServer) CancelTermGrants(ctx context.Context, msg *types.MsgCancelTermGrants) (*types.MsgCancelTermGrantsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	current, err := m.k.GrantsMandate.Get(ctx)
	if err != nil {
		return nil, err
	}
	if msg.Term == 0 || msg.Term > current.Term {
		return nil, errors.New("no such committee term")
	}
	var ids []uint64
	if err := m.k.TermGrants.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](msg.Term), func(key collections.Pair[uint64, uint64], _ sdk.Coin) (bool, error) {
		ids = append(ids, key.K2())
		return false, nil
	}); err != nil {
		return nil, err
	}
	for _, id := range ids {
		g, err := m.k.Grants.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := m.k.cancel(ctx, msg.Authority, g); err != nil {
			return nil, fmt.Errorf("grant %d: %w", id, err)
		}
	}
	if err := m.k.record(ctx, 0, "cancel_term_grants", msg.Authority, chain.NoahCoin(math.ZeroInt()), map[string]uint64{"term": msg.Term}); err != nil {
		return nil, err
	}
	return &types.MsgCancelTermGrantsResponse{}, nil
}
