package keeper

import (
	"context"
	"errors"
	"fmt"
	stdmath "math"
	"slices"

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

func (m msgServer) registrar(ctx context.Context, address string) (types.Params, error) {
	p, err := m.k.Params.Get(ctx)
	if err != nil {
		return p, err
	}
	if p.Registrar == "" || address != p.Registrar {
		return p, errors.New("only the current registrar may act")
	}
	return p, nil
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
	id, err := m.k.create(ctx, msg.Authority, msg.Kind, beneficiary, msg.Amount, msg.Schedule, msg.Reference)
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

func (m msgServer) RegisterMembers(ctx context.Context, msg *types.MsgRegisterMembers) (*types.MsgRegisterMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	p, err := m.registrar(ctx, msg.Registrar)
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
	available, err := m.k.unallocated(ctx, chain.NoahBaseDenom)
	if err != nil {
		return nil, err
	}
	if available.LT(needed) {
		return nil, errors.New("member batch is not fully funded")
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
		id, err := m.k.create(ctx, msg.Registrar, types.GrantKind_GRANT_KIND_MEMBER, address, chain.NoahCoin(p.MemberAmount), p.MemberSchedule, "")
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return &types.MsgRegisterMembersResponse{GrantIds: ids}, nil
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

func (m msgServer) SuspendMembers(ctx context.Context, msg *types.MsgSuspendMembers) (*types.MsgSuspendMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if _, err := m.registrar(ctx, msg.Registrar); err != nil {
		return nil, err
	}
	addresses, err := addressesBatch(msg.Addresses)
	if err != nil {
		return nil, err
	}
	registrar, err := chain.ParseCanonicalAccountAddress("registrar", msg.Registrar)
	if err != nil {
		return nil, err
	}
	epoch, err := m.k.epoch(ctx, registrar)
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
		g.Suspension = &types.Suspension{At: now, Registrar: msg.Registrar, Epoch: epoch}
		if err := m.k.Grants.Set(ctx, g.Id, g); err != nil {
			return nil, err
		}
		if err := m.k.record(ctx, g.Id, "suspended", msg.Registrar, sdk.NewCoin(g.Amount.Denom, math.ZeroInt()), g.Suspension); err != nil {
			return nil, err
		}
	}
	return &types.MsgSuspendMembersResponse{}, nil
}

func (m msgServer) ReinstateMembers(ctx context.Context, msg *types.MsgReinstateMembers) (*types.MsgReinstateMembersResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Sender); err != nil {
		if _, err := m.registrar(ctx, msg.Sender); err != nil {
			return nil, err
		}
	}
	addresses, err := addressesBatch(msg.Addresses)
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
		if !suspended {
			return nil, errors.New("member is not suspended")
		}
		g.Suspension = nil
		if err := m.k.Grants.Set(ctx, g.Id, g); err != nil {
			return nil, err
		}
		if err := m.k.record(ctx, g.Id, "reinstated", msg.Sender, sdk.NewCoin(g.Amount.Denom, math.ZeroInt()), nil); err != nil {
			return nil, err
		}
	}
	return &types.MsgReinstateMembersResponse{}, nil
}

func (m msgServer) VoidSuspensions(ctx context.Context, msg *types.MsgVoidSuspensions) (*types.MsgVoidSuspensionsResponse, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if err := m.authority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	registrar, err := chain.ParseCanonicalAccountAddress("registrar", msg.Registrar)
	if err != nil {
		return nil, err
	}
	epoch, err := m.k.epoch(ctx, registrar)
	if err != nil {
		return nil, err
	}
	if epoch == stdmath.MaxUint64 {
		return nil, errors.New("registrar epoch exhausted")
	}
	if err := m.k.RegistrarEpochs.Set(ctx, registrar, epoch+1); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "void_suspensions", msg.Authority, chain.NoahCoin(math.ZeroInt()), types.RegistrarEpoch{Registrar: msg.Registrar, Epoch: epoch + 1}); err != nil {
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
	available, err := m.k.unallocated(ctx, msg.Amount.Denom)
	if err != nil {
		return nil, err
	}
	if available.LT(msg.Amount.Amount) {
		return nil, errors.New("return would consume reserved obligations")
	}
	if err := m.k.distribution.FundCommunityPool(ctx, sdk.NewCoins(msg.Amount), m.k.address); err != nil {
		return nil, err
	}
	if err := m.k.record(ctx, 0, "return_unallocated", msg.Authority, msg.Amount, nil); err != nil {
		return nil, err
	}
	return &types.MsgReturnUnallocatedResponse{}, nil
}
