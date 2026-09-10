package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/types"
)

// simAcquiredDenom names external custody. Holdability admits any external
// symbol without consulting the registry, so a position can be attested in one
// without the run first listing an asset.
const simAcquiredDenom = "axau-sim"

// activeReserveCommittee resolves the appointed committee when the mandate is
// active at this height and the run holds its key. A skip is the norm rather
// than a fault: an appointment may have expired, or been restored from an
// export naming an address this run never held.
func activeReserveCommittee(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
) (types.ReserveMandate, simsx.SimAccount, bool) {
	mandate, err := k.Mandate.Get(ctx)
	if err != nil {
		reporter.Skip("get reserve mandate: " + err.Error())

		return types.ReserveMandate{}, simsx.SimAccount{}, false
	}
	if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
		reporter.Skip("reserve mandate is not active at this height")

		return types.ReserveMandate{}, simsx.SimAccount{}, false
	}
	committee := testData.GetAccount(reporter, mandate.Committee)
	if reporter.IsSkipped() {
		return types.ReserveMandate{}, simsx.SimAccount{}, false
	}

	return mandate, committee, true
}

// openPosition returns an open position matching the predicate, skipping when
// none does. Every position message starts from one, and until a deployment
// has opened one there is nothing to act on.
func openPosition(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
	reason string,
	admits func(types.Position) bool,
) (types.Position, bool) {
	var candidates []types.Position
	if err := k.OpenPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		if admits(position) {
			candidates = append(candidates, position)
		}

		return false, nil
	}); err != nil {
		reporter.Skip("iterating open positions: " + err.Error())

		return types.Position{}, false
	}
	if len(candidates) == 0 {
		reporter.Skip(reason)

		return types.Position{}, false
	}

	return candidates[testData.Rand().IntInRange(0, len(candidates))], true
}

// MsgCommitteeDeployFactory opens positions through NOAH deployment within allowance and balance
// bounds. It uses half the available headroom so later deployments remain possible.
func MsgCommitteeDeployFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeDeploy] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeDeploy) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		if len(mandate.Destinations) == 0 {
			reporter.Skip("the mandate names no destination")

			return nil, nil
		}

		used, err := k.AllowanceUsed.Get(ctx)
		if err != nil {
			reporter.Skip("get allowance usage: " + err.Error())

			return nil, nil
		}

		ceiling := k.ReserveBalance(ctx)
		if remaining := mandate.DeploymentAllowance.Amount.Sub(used); remaining.LT(ceiling) {
			ceiling = remaining
		}
		ceiling = ceiling.QuoRaw(2)
		if !ceiling.IsPositive() {
			reporter.Skip("no headroom under the Reserve balance and the deployment allowance")

			return nil, nil
		}

		r := testData.Rand()
		amount, err := r.PositiveSDKIntn(ceiling)
		if err != nil {
			reporter.Skip("draw deployment amount: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeDeploy{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			// A zero position identifier opens a new position.
			PositionId:     0,
			Destination:    mandate.Destinations[r.Intn(len(mandate.Destinations))],
			Amount:         chain.NoahCoin(amount),
			Acquired:       sdk.NewCoin(simAcquiredDenom, amount),
			VenueReference: "sim-venue-" + r.StringN(8),
			Reference:      "sim-deploy-" + r.StringN(8),
		}
	}
}

// MsgCommitteeRecordUpdateFactory restates a position's attested quantity. The
// denomination cannot change, so only the amount moves.
func MsgCommitteeRecordUpdateFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeRecordUpdate] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeRecordUpdate) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		position, ok := openPosition(ctx, k, testData, reporter,
			"no open position to restate",
			func(types.Position) bool { return true },
		)
		if !ok {
			return nil, nil
		}

		r := testData.Rand()
		quantity, err := r.PositiveSDKIntn(position.Quantity.Amount.MulRaw(2))
		if err != nil {
			reporter.Skip("draw restated quantity: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeRecordUpdate{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Quantity:     sdk.NewCoin(position.Quantity.Denom, quantity),
			Reference:    "sim-restate-" + r.StringN(8),
		}
	}
}

// MsgCommitteeMarkImpairedFactory marks a sound position impaired.
func MsgCommitteeMarkImpairedFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeMarkImpaired] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeMarkImpaired) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		position, ok := openPosition(ctx, k, testData, reporter,
			"no sound position to impair",
			func(p types.Position) bool { return !p.Impaired },
		)
		if !ok {
			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeMarkImpaired{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Reference:    "sim-impair-" + testData.Rand().StringN(8),
		}
	}
}

// MsgCommitteeClearImpairmentFactory restores an impaired position.
func MsgCommitteeClearImpairmentFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeClearImpairment] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeClearImpairment) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		position, ok := openPosition(ctx, k, testData, reporter,
			"no impaired position to clear",
			func(p types.Position) bool { return p.Impaired },
		)
		if !ok {
			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeClearImpairment{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Reference:    "sim-clear-" + testData.Rand().StringN(8),
		}
	}
}

// MsgCommitteeClosePositionFactory closes a position, which is where the
// lifecycle a deployment opened ends.
func MsgCommitteeClosePositionFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeClosePosition] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeClosePosition) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		position, ok := openPosition(ctx, k, testData, reporter,
			"no open position to close",
			func(types.Position) bool { return true },
		)
		if !ok {
			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeClosePosition{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Reference:    "sim-close-" + testData.Rand().StringN(8),
		}
	}
}

// MsgCommitteeBurnSurplusFactory burns Reserve NOAH the capital requirement no
// longer claims. The surplus is what the keeper computes against Treasury's
// requirement, so nothing else bounds the draw.
func MsgCommitteeBurnSurplusFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeBurnSurplus] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeBurnSurplus) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}

		surplus, err := k.BurnableSurplus(ctx)
		if err != nil {
			reporter.Skip("get burnable surplus: " + err.Error())

			return nil, nil
		}
		if !surplus.IsPositive() {
			reporter.Skip("the Reserve holds no burnable surplus")

			return nil, nil
		}

		amount, err := testData.Rand().PositiveSDKIntn(surplus)
		if err != nil {
			reporter.Skip("draw burn amount: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeBurnSurplus{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Amount:       chain.NoahCoin(amount),
		}
	}
}

// ledgerEntry returns an entry of the given kind whose position is still
// addressable, together with that position. Corrections and reversals both name
// a prior entry, and until deployments have written some there is nothing to
// name.
func ledgerEntry(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
	reason string,
	kind types.EntryKind,
	admits func(types.AccountingEntry) bool,
) (types.AccountingEntry, types.Position, bool) {
	var entries []types.AccountingEntry
	if err := k.Ledger.Walk(ctx, nil, func(_ uint64, entry types.AccountingEntry) (bool, error) {
		if entry.Kind == kind && admits(entry) {
			entries = append(entries, entry)
		}

		return false, nil
	}); err != nil {
		reporter.Skip("iterating the ledger: " + err.Error())

		return types.AccountingEntry{}, types.Position{}, false
	}

	r := testData.Rand()
	// Walk the candidates in a random order so a position that has since been
	// closed does not pin the draw to one entry.
	for range len(entries) {
		entry := entries[r.IntInRange(0, len(entries))]
		position, err := k.OpenPositions.Get(ctx, entry.PositionId)
		if err == nil {
			return entry, position, true
		}
	}

	reporter.Skip(reason)

	return types.AccountingEntry{}, types.Position{}, false
}

// MsgCommitteeAttributeReturnFactory books value coming back from a position,
// which is how a deployment is recovered. The returned coin is NOAH and stays
// inside the Reserve's holding, since the handler checks the movement against
// what the Reserve actually holds in that denomination.
func MsgCommitteeAttributeReturnFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeAttributeReturn] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeAttributeReturn) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		position, ok := openPosition(ctx, k, testData, reporter,
			"no open position to attribute a return to",
			func(types.Position) bool { return true },
		)
		if !ok {
			return nil, nil
		}

		held := k.ReserveBalance(ctx)
		if !held.IsPositive() {
			reporter.Skip("the Reserve holds nothing to attribute")

			return nil, nil
		}

		r := testData.Rand()
		returned, err := r.PositiveSDKIntn(held)
		if err != nil {
			reporter.Skip("draw returned amount: " + err.Error())

			return nil, nil
		}
		remaining, err := r.PositiveSDKIntn(position.Quantity.Amount)
		if err != nil {
			reporter.Skip("draw remaining quantity: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeAttributeReturn{
			Committee:         mandate.Committee,
			ExpectedTerm:      mandate.Term,
			PositionId:        position.PositionId,
			ReturnedCoin:      chain.NoahCoin(returned),
			RemainingQuantity: sdk.NewCoin(position.Quantity.Denom, remaining),
			Reference:         "sim-return-" + r.StringN(8),
		}
	}
}

// MsgCommitteeCorrectPositionFactory restates a prior entry. Any entry on an
// addressable position may be corrected, so the deployment that opened it is
// the usual subject.
func MsgCommitteeCorrectPositionFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeCorrectPosition] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeCorrectPosition) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		entry, position, ok := ledgerEntry(ctx, k, testData, reporter,
			"no deployment entry on an open position to correct",
			types.EntryKind_ENTRY_KIND_DEPLOYMENT,
			func(types.AccountingEntry) bool { return true },
		)
		if !ok {
			return nil, nil
		}

		r := testData.Rand()
		quantity, err := r.PositiveSDKIntn(position.Quantity.Amount.MulRaw(2))
		if err != nil {
			reporter.Skip("draw corrected quantity: " + err.Error())

			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeCorrectPosition{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Corrects:     entry.EntryId,
			// Restating in the position's own denomination needs no holdability
			// check, which the handler applies only to a change of denomination.
			Quantity:       sdk.NewCoin(position.Quantity.Denom, quantity),
			VenueReference: "",
			Reference:      "sim-correct-" + r.StringN(8),
		}
	}
}

// MsgCommitteeReverseReturnFactory withdraws a return attribution. Only a
// return entry can be reversed, and only once.
func MsgCommitteeReverseReturnFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeReverseReturn] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeReverseReturn) {
		mandate, committee, ok := activeReserveCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		entry, position, ok := ledgerEntry(ctx, k, testData, reporter,
			"no unreversed return attribution on an open position",
			types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
			func(candidate types.AccountingEntry) bool {
				reversed, err := k.ReversedReturns.Has(ctx, candidate.EntryId)

				return err == nil && !reversed
			},
		)
		if !ok {
			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeReverseReturn{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			PositionId:   position.PositionId,
			Reverses:     entry.EntryId,
			Reference:    "sim-reverse-" + testData.Rand().StringN(8),
		}
	}
}
