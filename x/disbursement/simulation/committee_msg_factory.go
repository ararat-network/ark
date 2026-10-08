package simulation

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// Signed factories deliver at once, and a failed delivery fails the run, so each emits only what
// the handler accepts in the state it reads.

// activeGrantsCommittee resolves the appointed committee when its mandate is active at this height
// and the run holds its key. A skip is the norm: the appointment may have expired, or been restored
// from an export naming an address this run never held.
func activeGrantsCommittee(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
) (types.GrantsMandate, simsx.SimAccount, bool) {
	mandate, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		reporter.Skip("get grants mandate: " + err.Error())
		return types.GrantsMandate{}, simsx.SimAccount{}, false
	}
	if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
		reporter.Skip("grants mandate is not active at this height")
		return types.GrantsMandate{}, simsx.SimAccount{}, false
	}
	committee := testData.GetAccount(reporter, mandate.Committee)
	if reporter.IsSkipped() {
		return types.GrantsMandate{}, simsx.SimAccount{}, false
	}
	return mandate, committee, true
}

// MsgCommitteeRegistrarFactory appoints a run-owned registrar from now to a height inside the
// committee's window.
func MsgCommitteeRegistrarFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeRegistrar] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeRegistrar) {
		mandate, committee, ok := activeGrantsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		registrar := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// Active means the committee's activation is at most this height and its expiry above it. The
		// draw is capped so a restored mandate's far expiry cannot overflow it.
		height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
		expiry := min(mandate.ExpiryHeight, height+100_000)
		return []simsx.SimAccount{committee}, &types.MsgCommitteeRegistrar{
			Committee:        mandate.Committee,
			ExpectedTerm:     mandate.Term,
			Registrar:        registrar.AddressBech32,
			ActivationHeight: height,
			ExpiryHeight:     testData.Rand().Uint64InRange(height+1, expiry+1),
		}
	}
}

// MsgRegisterMembersFactory registers a few run accounts that are not yet members, as many as the
// open tranche pays for.
func MsgRegisterMembersFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgRegisterMembers] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgRegisterMembers) {
		mandate, err := k.RegistrarMandate.Get(ctx)
		if err != nil {
			reporter.Skip("get registrar mandate: " + err.Error())
			return nil, nil
		}
		if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
			reporter.Skip("registrar mandate is not active at this height")
			return nil, nil
		}
		registrar := testData.GetAccount(reporter, mandate.Committee)
		if reporter.IsSkipped() {
			return nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get disbursement params: " + err.Error())
			return nil, nil
		}
		open, err := k.MemberTranche.Get(ctx)
		if err != nil {
			reporter.Skip("get member tranche: " + err.Error())
			return nil, nil
		}
		affordable := open.Quo(params.MemberAmount)
		if affordable.IsZero() {
			reporter.Skip("the open tranche pays for no member")
			return nil, nil
		}

		var candidates []string
		for _, account := range testData.AllAccounts() {
			registered, err := k.Members.Has(ctx, account.Address)
			if err != nil {
				reporter.Skip("checking membership: " + err.Error())
				return nil, nil
			}
			if !registered {
				candidates = append(candidates, account.Address.String())
			}
		}
		if len(candidates) == 0 {
			reporter.Skip("every run account is already a member")
			return nil, nil
		}

		r := testData.Rand()
		n := r.IntInRange(1, maxSimulatedBatch+1)
		if affordable.LT(math.NewInt(int64(n))) {
			n = int(affordable.Int64())
		}
		return []simsx.SimAccount{registrar}, &types.MsgRegisterMembers{
			Registrar:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Addresses:    sample(r, candidates, n),
			Reference:    "sim-registration-" + r.StringN(8),
		}
	}
}

// MsgCommitteeSuspendFactory suspends a few live members.
func MsgCommitteeSuspendFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeSuspend] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeSuspend) {
		mandate, committee, ok := activeGrantsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		paying, ok := liveMembers(ctx, k, false, reporter)
		if !ok {
			return nil, nil
		}
		if len(paying) == 0 {
			reporter.Skip("no paying member to suspend")
			return nil, nil
		}
		r := testData.Rand()
		return []simsx.SimAccount{committee}, &types.MsgCommitteeSuspend{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Addresses:    sample(r, paying, r.IntInRange(1, maxSimulatedBatch+1)),
		}
	}
}

// MsgCommitteeReinstateFactory resumes a few suspended members.
func MsgCommitteeReinstateFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeReinstate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeReinstate) {
		mandate, committee, ok := activeGrantsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		suspended, ok := liveMembers(ctx, k, true, reporter)
		if !ok {
			return nil, nil
		}
		if len(suspended) == 0 {
			reporter.Skip("no suspended member to reinstate")
			return nil, nil
		}
		r := testData.Rand()
		return []simsx.SimAccount{committee}, &types.MsgCommitteeReinstate{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Addresses:    sample(r, suspended, r.IntInRange(1, maxSimulatedBatch+1)),
		}
	}
}

// MsgCommitteeCompensateFactory awards NOAH compensation from at most a tenth of uncommitted
// contributor NOAH. Valuation reads oracle rates the keeper does not expose, and a run's genesis
// rates go stale, so the factory runs the handler on a discarded cache and skips if it refuses.
func MsgCommitteeCompensateFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeCompensate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeCompensate) {
		mandate, committee, ok := activeGrantsCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		contributors, ok := noahBalance(ctx, k, types.ContributorsName, reporter)
		if !ok {
			return nil, nil
		}
		tenth := contributors.Uncommitted.Amount.QuoRaw(10)
		if !tenth.IsPositive() {
			reporter.Skip("no uncommitted contributor NOAH to award")
			return nil, nil
		}
		beneficiary := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		r := testData.Rand()
		amount, err := r.PositiveSDKIntn(tenth)
		if err != nil {
			reporter.Skip("draw award: " + err.Error())
			return nil, nil
		}

		msg := &types.MsgCommitteeCompensate{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Beneficiary:  beneficiary.AddressBech32,
			Amount:       chain.NoahCoin(amount),
			Reference:    "sim-compensation-" + r.StringN(8),
			StartTime:    chain.BlockSeconds(ctx),
			Periods:      r.Uint32InRange(1, 13),
			MaxOffer:     math.ZeroInt(),
		}
		cache, _ := sdk.UnwrapSDKContext(ctx).CacheContext()
		if _, err := keeper.NewMsgServerImpl(k).CommitteeCompensate(cache, msg); err != nil {
			reporter.Skip("compensation would be refused: " + err.Error())
			return nil, nil
		}
		return []simsx.SimAccount{committee}, msg
	}
}
