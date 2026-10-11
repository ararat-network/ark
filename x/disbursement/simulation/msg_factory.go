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

// Governance factories build proposals, which execute after the voting period against whatever
// state then holds; one that fails there fails its proposal, not the run.

// maxSimulatedBatch keeps batch messages small; the handlers' own limit is types.MaxBatch.
const maxSimulatedBatch = 3

// noahBalance reads one account's NOAH entry from the balance query: custody, reservations, the
// uncommitted pool, and the open tranche.
func noahBalance(ctx context.Context, k *keeper.Keeper, account string, reporter simsx.SimulationReporter) (types.AccountBalance, bool) {
	res, err := keeper.NewQueryServerImpl(k).Balance(ctx, &types.QueryBalanceRequest{Denom: chain.NoahBaseDenom})
	if err != nil {
		reporter.Skip("query disbursement balance: " + err.Error())
		return types.AccountBalance{}, false
	}
	for _, entry := range res.Accounts {
		if entry.Account == account {
			return entry, true
		}
	}
	reporter.Skip("no NOAH balance entry for " + account)
	return types.AccountBalance{}, false
}

// sample draws up to n distinct items at random.
func sample[T any](r *simsx.XRand, items []T, n int) []T {
	n = min(n, len(items))
	picked := make([]T, 0, n)
	for _, i := range r.Perm(len(items))[:n] {
		picked = append(picked, items[i])
	}
	return picked
}

// MsgUpdateParamsFactory proposes a fresh set of simulation-scale parameters.
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    RandomisedParams(testData.Rand().Rand),
		}
	}
}

// MsgSetGrantsMandateFactory appoints a run-owned committee with an allowance. The appointment
// clears the registrar, which the new committee may appoint again.
func MsgSetGrantsMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetGrantsMandate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetGrantsMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses the authority as committee, and a drawn account could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the disbursement authority")
			return nil, nil
		}

		r := testData.Rand()
		now := chain.BlockSeconds(ctx)
		activation := now + r.Uint64InRange(1, 1_000)
		return nil, &types.MsgSetGrantsMandate{
			Authority:      authority,
			Committee:      committee.AddressBech32,
			ActivationTime: activation,
			// Validation demands activation strictly precede expiry, and the
			// handler demands expiry sit above the current block time.
			ExpiryTime:            activation + r.Uint64InRange(1, 100_000),
			CompensationAllowance: math.NewInt(r.Int63n(maxSimulatedAllowance) + 1),
		}
	}
}

// MsgOpenTrancheFactory opens part of the member pool not yet open to registration.
func MsgOpenTrancheFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgOpenTranche] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgOpenTranche) {
		members, ok := noahBalance(ctx, k, types.MembersName, reporter)
		if !ok {
			return nil, nil
		}
		open, err := k.MemberTranche.Get(ctx)
		if err != nil {
			reporter.Skip("get member tranche: " + err.Error())
			return nil, nil
		}
		room := members.Uncommitted.Amount.Sub(open)
		if !room.IsPositive() {
			reporter.Skip("the member pool is already open in full")
			return nil, nil
		}
		amount, err := testData.Rand().PositiveSDKIntn(room)
		if err != nil {
			reporter.Skip("draw tranche: " + err.Error())
			return nil, nil
		}
		return nil, &types.MsgOpenTranche{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Amount:    amount,
		}
	}
}

// MsgReturnUnallocatedFactory returns at most a tenth of uncommitted contributor NOAH, so the pool
// keeps funding awards for the rest of the run.
func MsgReturnUnallocatedFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgReturnUnallocated] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgReturnUnallocated) {
		contributors, ok := noahBalance(ctx, k, types.ContributorsName, reporter)
		if !ok {
			return nil, nil
		}
		tenth := contributors.Uncommitted.Amount.QuoRaw(10)
		if !tenth.IsPositive() {
			reporter.Skip("no uncommitted contributor NOAH to return")
			return nil, nil
		}
		amount, err := testData.Rand().PositiveSDKIntn(tenth)
		if err != nil {
			reporter.Skip("draw return: " + err.Error())
			return nil, nil
		}
		return nil, &types.MsgReturnUnallocated{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Amount:    chain.NoahCoin(amount),
		}
	}
}

// contributorAward draws a beneficiary and at most a tenth of uncommitted contributor NOAH.
func contributorAward(ctx context.Context, k *keeper.Keeper, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) (simsx.SimAccount, math.Int, bool) {
	contributors, ok := noahBalance(ctx, k, types.ContributorsName, reporter)
	if !ok {
		return simsx.SimAccount{}, math.Int{}, false
	}
	tenth := contributors.Uncommitted.Amount.QuoRaw(10)
	if !tenth.IsPositive() {
		reporter.Skip("no uncommitted contributor NOAH to award")
		return simsx.SimAccount{}, math.Int{}, false
	}
	beneficiary := testData.AnyAccount(reporter)
	if reporter.IsSkipped() {
		return simsx.SimAccount{}, math.Int{}, false
	}
	amount, err := testData.Rand().PositiveSDKIntn(tenth)
	if err != nil {
		reporter.Skip("draw award: " + err.Error())
		return simsx.SimAccount{}, math.Int{}, false
	}
	return beneficiary, amount, true
}

// MsgGrantOwnershipFactory awards ownership from at most a tenth of uncommitted contributor NOAH. Its
// clock may not start before the award, which executes after the vote, so it starts days ahead.
func MsgGrantOwnershipFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgGrantOwnership] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgGrantOwnership) {
		beneficiary, amount, ok := contributorAward(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		r := testData.Rand()
		return nil, &types.MsgGrantOwnership{
			Authority:   testData.ModuleAccountAddress(reporter, "gov"),
			Beneficiary: beneficiary.AddressBech32,
			Amount:      amount,
			Reference:   "sim-grant-" + r.StringN(8),
			StartTime:   chain.BlockSeconds(ctx) + r.Uint64InRange(86_400, 30*86_400),
		}
	}
}

// MsgCompensateFactory awards compensation in NOAH from at most a tenth of uncommitted contributor NOAH.
func MsgCompensateFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCompensate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCompensate) {
		beneficiary, amount, ok := contributorAward(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		r := testData.Rand()
		return nil, &types.MsgCompensate{
			Authority:   testData.ModuleAccountAddress(reporter, "gov"),
			Beneficiary: beneficiary.AddressBech32,
			Amount:      chain.NoahCoin(amount),
			Reference:   "sim-grant-" + r.StringN(8),
			StartTime:   chain.BlockSeconds(ctx),
			Periods:     r.Uint32InRange(1, 13),
			MaxOffer:    math.ZeroInt(),
		}
	}
}

// liveGrants lists the IDs of grants that still pay: not cancelled, with principal remaining.
func liveGrants(ctx context.Context, k *keeper.Keeper, reporter simsx.SimulationReporter) ([]uint64, bool) {
	var ids []uint64
	if err := k.Grants.Walk(ctx, nil, func(id uint64, g types.Grant) (bool, error) {
		if !g.Cancelled() && g.Remaining.IsPositive() {
			ids = append(ids, id)
		}
		return false, nil
	}); err != nil {
		reporter.Skip("iterating grants: " + err.Error())
		return nil, false
	}
	return ids, true
}

// MsgCancelGrantsFactory cancels a few grants that still pay.
func MsgCancelGrantsFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCancelGrants] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCancelGrants) {
		ids, ok := liveGrants(ctx, k, reporter)
		if !ok {
			return nil, nil
		}
		if len(ids) == 0 {
			reporter.Skip("no live grant to cancel")
			return nil, nil
		}
		r := testData.Rand()
		return nil, &types.MsgCancelGrants{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			GrantIds:  sample(r, ids, r.IntInRange(1, maxSimulatedBatch+1)),
		}
	}
}

// liveMembers lists live members' addresses, suspended or not as asked.
func liveMembers(ctx context.Context, k *keeper.Keeper, suspended bool, reporter simsx.SimulationReporter) ([]string, bool) {
	var addresses []string
	if err := k.Members.Walk(ctx, nil, func(_ sdk.AccAddress, id uint64) (bool, error) {
		g, err := k.Grants.Get(ctx, id)
		if err != nil {
			return true, err
		}
		if !g.Cancelled() && g.Remaining.IsPositive() && g.Suspended() == suspended {
			addresses = append(addresses, g.Beneficiary)
		}
		return false, nil
	}); err != nil {
		reporter.Skip("iterating members: " + err.Error())
		return nil, false
	}
	return addresses, true
}

// MsgReinstateMembersFactory resumes a few suspended members.
func MsgReinstateMembersFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgReinstateMembers] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgReinstateMembers) {
		suspended, ok := liveMembers(ctx, k, true, reporter)
		if !ok {
			return nil, nil
		}
		if len(suspended) == 0 {
			reporter.Skip("no suspended member to reinstate")
			return nil, nil
		}
		r := testData.Rand()
		return nil, &types.MsgReinstateMembers{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Addresses: sample(r, suspended, r.IntInRange(1, maxSimulatedBatch+1)),
		}
	}
}
