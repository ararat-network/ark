package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

type msgServer struct {
	k *Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl exposes the disbursement transaction handlers.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
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

// compensation checks a compensation award's inputs, the same whoever grants it, and returns the
// beneficiary and the grant for the caller to attribute.
func (m msgServer) compensation(ctx context.Context, beneficiary string, amount sdk.Coin, start uint64, periods uint32, reference string) (sdk.AccAddress, types.Grant, error) {
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", beneficiary)
	if err != nil {
		return nil, types.Grant{}, err
	}
	if err := amount.Validate(); err != nil {
		return nil, types.Grant{}, err
	}
	if err := chain.ValidateAmount(amount.Amount, true); err != nil {
		return nil, types.Grant{}, err
	}
	if err := m.k.validateCompensation(ctx, amount.Denom); err != nil {
		return nil, types.Grant{}, err
	}
	schedule, err := m.k.compensationSchedule(ctx, start, periods)
	if err != nil {
		return nil, types.Grant{}, err
	}
	value, err := m.k.awardValue(ctx, amount)
	if err != nil {
		return nil, types.Grant{}, err
	}
	return address, types.Grant{
		Kind:       types.GrantKind_GRANT_KIND_COMPENSATION,
		Amount:     amount,
		Schedule:   schedule,
		StartTime:  start,
		Reference:  reference,
		AwardValue: value,
	}, nil
}

func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil update params message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
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

func (m msgServer) SetGrantsMandate(ctx context.Context, msg *types.MsgSetGrantsMandate) (*types.MsgSetGrantsMandateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil set grants mandate message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.SetGrantsMandate(ctx, msg); err != nil {
		return nil, err
	}
	return &types.MsgSetGrantsMandateResponse{}, nil
}

func (m msgServer) OpenTranche(ctx context.Context, msg *types.MsgOpenTranche) (*types.MsgOpenTrancheResponse, error) {
	if msg == nil {
		return nil, errors.New("nil open tranche message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := chain.ValidateAmount(msg.Amount, true); err != nil {
		return nil, err
	}
	tranche, err := m.k.MemberTranche.Get(ctx)
	if err != nil {
		return nil, err
	}
	if tranche, err = tranche.SafeAdd(msg.Amount); err != nil {
		return nil, err
	}
	pool, err := m.k.uncommitted(ctx, types.MembersName, chain.NoahBaseDenom)
	if err != nil {
		return nil, err
	}
	if tranche.GT(pool) {
		return nil, errors.New("tranche exceeds the member pool")
	}
	if err := m.k.MemberTranche.Set(ctx, tranche); err != nil {
		return nil, err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTrancheOpened{
		Amount:        msg.Amount,
		MemberTranche: tranche,
	}); err != nil {
		return nil, fmt.Errorf("disbursement tranche event: %w", err)
	}
	return &types.MsgOpenTrancheResponse{}, nil
}

func (m msgServer) ReturnUnallocated(ctx context.Context, msg *types.MsgReturnUnallocated) (*types.MsgReturnUnallocatedResponse, error) {
	if msg == nil {
		return nil, errors.New("nil return unallocated message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Amount.Validate(); err != nil {
		return nil, err
	}
	if err := chain.ValidateAmount(msg.Amount.Amount, true); err != nil {
		return nil, err
	}
	// The member account has no exit; the contributor account returns what no grant holds.
	available, err := m.k.uncommitted(ctx, types.ContributorsName, msg.Amount.Denom)
	if err != nil {
		return nil, err
	}
	if available.LT(msg.Amount.Amount) {
		return nil, errors.New("return exceeds uncommitted contributor custody")
	}
	if err := m.k.distributionKeeper.FundCommunityPool(ctx, sdk.NewCoins(msg.Amount), m.k.accountKeeper.GetModuleAddress(types.ContributorsName)); err != nil {
		return nil, err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventUnallocatedReturned{
		Denom:  msg.Amount.Denom,
		Amount: msg.Amount.Amount,
	}); err != nil {
		return nil, fmt.Errorf("disbursement return event: %w", err)
	}
	return &types.MsgReturnUnallocatedResponse{}, nil
}

func (m msgServer) GrantOwnership(ctx context.Context, msg *types.MsgGrantOwnership) (*types.MsgGrantOwnershipResponse, error) {
	if msg == nil {
		return nil, errors.New("nil grant ownership message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	beneficiary, err := chain.ParseCanonicalAccountAddress("beneficiary", msg.Beneficiary)
	if err != nil {
		return nil, err
	}
	if err := chain.ValidateAmount(msg.Amount, true); err != nil {
		return nil, err
	}
	schedule, err := m.k.ownershipSchedule(ctx, msg.StartTime)
	if err != nil {
		return nil, err
	}
	id, err := m.k.create(ctx, beneficiary, types.Grant{
		Kind:       types.GrantKind_GRANT_KIND_OWNERSHIP,
		Amount:     chain.NoahCoin(msg.Amount),
		Schedule:   schedule,
		StartTime:  msg.StartTime,
		Reference:  msg.Reference,
		CreatedBy:  msg.Authority,
		AwardValue: math.ZeroInt(),
	}, math.ZeroInt())
	if err != nil {
		return nil, err
	}
	return &types.MsgGrantOwnershipResponse{GrantId: id}, nil
}

func (m msgServer) Compensate(ctx context.Context, msg *types.MsgCompensate) (*types.MsgCompensateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil compensate message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	beneficiary, g, err := m.compensation(ctx, msg.Beneficiary, msg.Amount, msg.StartTime, msg.Periods, msg.Reference)
	if err != nil {
		return nil, err
	}
	g.CreatedBy = msg.Authority
	id, err := m.k.create(ctx, beneficiary, g, msg.MaxOffer)
	if err != nil {
		return nil, err
	}
	return &types.MsgCompensateResponse{GrantId: id}, nil
}

func (m msgServer) CancelGrants(ctx context.Context, msg *types.MsgCancelGrants) (*types.MsgCancelGrantsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil cancel grants message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := idsBatch(msg.GrantIds); err != nil {
		return nil, err
	}
	for _, id := range msg.GrantIds {
		g, err := m.k.getGrant(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := m.k.cancel(ctx, msg.Authority, g); err != nil {
			return nil, err
		}
	}
	return &types.MsgCancelGrantsResponse{}, nil
}

func (m msgServer) ReinstateMembers(ctx context.Context, msg *types.MsgReinstateMembers) (*types.MsgReinstateMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil reinstate members message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if err := m.k.reinstate(ctx, msg.Authority, address); err != nil {
			return nil, err
		}
	}
	return &types.MsgReinstateMembersResponse{}, nil
}

func (m msgServer) CommitteeRegistrar(ctx context.Context, msg *types.MsgCommitteeRegistrar) (*types.MsgCommitteeRegistrarResponse, error) {
	if msg == nil {
		return nil, errors.New("nil committee registrar message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := m.k.SetRegistrarMandate(ctx, appointment, msg.Registrar, msg.ActivationTime, msg.ExpiryTime); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeRegistrarResponse{}, nil
}

func (m msgServer) RegisterMembers(ctx context.Context, msg *types.MsgRegisterMembers) (*types.MsgRegisterMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil register members message")
	}
	appointment, err := m.k.AuthoriseRegistrar(ctx, msg.Registrar, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := chain.ValidateReference("batch reference", msg.Reference, true); err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	p, err := m.k.Params.Get(ctx)
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
	needed, err := p.MemberAmount.SafeMul(math.NewInt(int64(len(addresses))))
	if err != nil {
		return nil, err
	}
	tranche, err := m.k.MemberTranche.Get(ctx)
	if err != nil {
		return nil, err
	}
	if tranche.LT(needed) {
		return nil, errors.New("member batch exceeds the open member tranche")
	}
	// A member's clock is the registration itself.
	now := chain.BlockSeconds(ctx)
	ids := make([]uint64, 0, len(addresses))
	for _, address := range addresses {
		id, err := m.k.create(ctx, address, types.Grant{
			Kind:        types.GrantKind_GRANT_KIND_MEMBER,
			Amount:      chain.NoahCoin(p.MemberAmount),
			Schedule:    p.MemberSchedule,
			StartTime:   now,
			Reference:   msg.Reference,
			CreatedBy:   appointment.Committee,
			MandateTerm: appointment.Term,
			AwardValue:  math.ZeroInt(),
		}, math.ZeroInt())
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return &types.MsgRegisterMembersResponse{GrantIds: ids}, nil
}

func (m msgServer) CommitteeSuspend(ctx context.Context, msg *types.MsgCommitteeSuspend) (*types.MsgCommitteeSuspendResponse, error) {
	if msg == nil {
		return nil, errors.New("nil committee suspend message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if err := m.k.suspend(ctx, appointment.Committee, address); err != nil {
			return nil, err
		}
	}
	return &types.MsgCommitteeSuspendResponse{}, nil
}

func (m msgServer) CommitteeReinstate(ctx context.Context, msg *types.MsgCommitteeReinstate) (*types.MsgCommitteeReinstateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil committee reinstate message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if err := m.k.reinstate(ctx, appointment.Committee, address); err != nil {
			return nil, err
		}
	}
	return &types.MsgCommitteeReinstateResponse{}, nil
}

func (m msgServer) CommitteeCompensate(ctx context.Context, msg *types.MsgCommitteeCompensate) (*types.MsgCommitteeCompensateResponse, error) {
	if msg == nil {
		return nil, errors.New("nil committee compensate message")
	}
	appointment, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	// The committee awards compensation only; ownership is governance's alone (D113).
	beneficiary, g, err := m.compensation(ctx, msg.Beneficiary, msg.Amount, msg.StartTime, msg.Periods, msg.Reference)
	if err != nil {
		return nil, err
	}
	// An award charges at least its value, so one past the allowance is refused here, before it can
	// trade; create charges what it drew once the buy is known.
	if _, err := m.k.usageAfter(ctx, g.AwardValue); err != nil {
		return nil, err
	}
	g.CreatedBy = appointment.Committee
	g.MandateTerm = appointment.Term
	id, err := m.k.create(ctx, beneficiary, g, msg.MaxOffer)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeCompensateResponse{GrantId: id}, nil
}

func (m msgServer) SetPayee(ctx context.Context, msg *types.MsgSetPayee) (*types.MsgSetPayeeResponse, error) {
	if msg == nil {
		return nil, errors.New("nil set payee message")
	}
	g, err := m.k.getGrant(ctx, msg.GrantId)
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
		p, err := m.k.getBeneficiary(ctx, address)
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
	if _, isPool := m.k.poolAddresses[string(payee)]; isPool || m.k.bankKeeper.BlockedAddr(payee) {
		return nil, errors.New("payee cannot receive funds")
	}
	if msg.Payee == g.Payee {
		return nil, errors.New("payee is unchanged")
	}
	g.Payee = msg.Payee
	// A payee that refused is replaced; the block looks again at once rather than at the retry, but
	// never sooner than the grant may be due.
	floor := max(g.StartTime, g.CreatedTime+g.Schedule.FirstLength)
	if now := chain.BlockSeconds(ctx); g.DueTime > now && now >= floor {
		if err := m.k.setDueTime(ctx, &g, now); err != nil {
			return nil, err
		}
	}
	if err := m.k.writeGrant(ctx, g, types.GrantEntry{
		GrantId: g.Id,
		Action:  types.GrantAction_GRANT_ACTION_PAYEE_CHANGED,
		Actor:   msg.Sender,
		Amount:  sdk.NewCoin(g.Amount.Denom, math.ZeroInt()),
		Subject: g.Payee,
	}); err != nil {
		return nil, err
	}
	return &types.MsgSetPayeeResponse{}, nil
}

func (m msgServer) SetController(ctx context.Context, msg *types.MsgSetController) (*types.MsgSetControllerResponse, error) {
	if msg == nil {
		return nil, errors.New("nil set controller message")
	}
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", msg.Beneficiary)
	if err != nil {
		return nil, err
	}
	p, err := m.k.getBeneficiary(ctx, address)
	if err != nil {
		return nil, err
	}
	governance := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Sender) == nil
	if !governance && msg.Sender != p.Controller {
		return nil, errors.New("only governance or the current controller may rotate control")
	}
	if _, err := chain.ParseCanonicalAccountAddress("controller", msg.Controller); err != nil {
		return nil, err
	}
	if msg.Controller == p.Controller {
		return nil, errors.New("controller is unchanged")
	}
	p.Controller = msg.Controller
	if err := m.k.Beneficiaries.Set(ctx, address, p); err != nil {
		return nil, err
	}
	if err := m.k.recordBeneficiary(ctx, types.BeneficiaryEntry{
		Beneficiary: p.Address,
		Actor:       msg.Sender,
		Controller:  p.Controller,
	}); err != nil {
		return nil, err
	}
	return &types.MsgSetControllerResponse{}, nil
}
