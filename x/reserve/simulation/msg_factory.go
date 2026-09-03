package simulation

import (
	"context"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/types"
)

// MsgSetReserveMandateFactory appoints the Reserve committee and states the
// terms it may deploy under. The appointee and the destination are drawn from
// the simulation's own accounts, so the committee is an address the run holds a
// key for and the destination is a real account.
func MsgSetReserveMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetReserveMandate] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetReserveMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		destination := testData.AnyAccount(reporter, simsx.ExcludeAccounts(committee))
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the Reserve authority")

			return nil, nil
		}

		r := testData.Rand()
		activation := r.Uint64InRange(1, 1_000)

		return nil, &types.MsgSetReserveMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activation,
			// Validation demands activation strictly precede expiry.
			ExpiryHeight: activation + r.Uint64InRange(1, 100_000),
			// A configured mandate must allow something and name somewhere.
			DeploymentAllowance: chain.NoahCoin(math.NewInt(int64(r.IntInRange(1, 1_000_000_000)))),
			MinimumNoahBalance:  chain.NoahCoin(math.NewInt(int64(r.IntInRange(0, 1_000_000)))),
			Destinations:        []string{destination.AddressBech32},
		}
	}
}

// MsgSetRecognitionPolicyFactory restates the standing recognition policy with
// fresh terms.
//
// The denominations come from the policy already in state rather than from a
// draw: an entry must name an external symbol whose feed is active, which is a
// condition on Oracle state this cannot see, and the standing entries are the
// set already known to satisfy it. A run whose policy is empty has nothing to
// restate and skips.
func MsgSetRecognitionPolicyFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetRecognitionPolicy] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetRecognitionPolicy) {
		var denoms []string
		if err := k.RecognitionPolicy.Walk(ctx, nil, func(denom string, _ types.EligibilityEntry) (bool, error) {
			denoms = append(denoms, denom)

			return false, nil
		}); err != nil {
			reporter.Skip("iterating recognition policy: " + err.Error())

			return nil, nil
		}
		if len(denoms) == 0 {
			reporter.Skip("no standing recognition policy to restate")

			return nil, nil
		}

		r := testData.Rand()
		// Cap ratios must sum strictly below one, so each entry takes an equal
		// share of a budget that already leaves room.
		share := math.LegacyOneDec().QuoInt64(int64(len(denoms)) * 2)
		entries := make([]types.EligibilityEntry, 0, len(denoms))
		for _, denom := range denoms {
			entries = append(entries, types.EligibilityEntry{
				Denom:               denom,
				HaircutFactor:       math.LegacyNewDecWithPrec(int64(r.IntInRange(1, 100)), 2),
				RecognitionCapRatio: share,
				MaxRateAge:          time.Duration(r.IntInRange(1, int(types.MaxRecognitionRateAge/time.Second))) * time.Second,
			})
		}

		return nil, &types.MsgSetRecognitionPolicy{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Entries:   entries,
		}
	}
}

// movableNoah returns an amount the Reserve can pay out while retaining a
// floor, and reports whether one exists. Both funding messages and the burn are
// guarded the same way: the balance must cover the movement and the floor the
// message itself states, so splitting the balance keeps their sum under it.
func movableNoah(
	ctx context.Context,
	k *keeper.Keeper,
	r *simsx.XRand,
) (moved math.Int, retained math.Int, ok bool) {
	half := k.ReserveBalance(ctx).QuoRaw(2)
	if !half.IsPositive() {
		return math.Int{}, math.Int{}, false
	}
	moved, err := r.PositiveSDKIntn(half)
	if err != nil {
		return math.Int{}, math.Int{}, false
	}

	return moved, half.Sub(moved), true
}

// MsgFundBufferFactory moves Reserve NOAH into the redemption Buffer.
func MsgFundBufferFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgFundBuffer] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgFundBuffer) {
		amount, retained, ok := movableNoah(ctx, k, testData.Rand())
		if !ok {
			reporter.Skip("reserve cannot fund while retaining a floor")

			return nil, nil
		}

		return nil, &types.MsgFundBuffer{
			Authority:             testData.ModuleAccountAddress(reporter, "gov"),
			Amount:                chain.NoahCoin(amount),
			MinimumReserveBalance: chain.NoahCoin(retained),
		}
	}
}

// MsgFundInsuranceFactory moves Reserve NOAH into Insurance. It is
// MsgFundBuffer's twin and is guarded identically.
func MsgFundInsuranceFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgFundInsurance] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgFundInsurance) {
		amount, retained, ok := movableNoah(ctx, k, testData.Rand())
		if !ok {
			reporter.Skip("reserve cannot fund while retaining a floor")

			return nil, nil
		}

		return nil, &types.MsgFundInsurance{
			Authority:             testData.ModuleAccountAddress(reporter, "gov"),
			Amount:                chain.NoahCoin(amount),
			MinimumReserveBalance: chain.NoahCoin(retained),
		}
	}
}

// MsgBurnReserveAssetsFactory burns Reserve NOAH. Only NOAH is burnt: the
// handler's balance guard reads the NOAH balance, so any other denomination
// would be a burn this cannot size.
func MsgBurnReserveAssetsFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgBurnReserveAssets] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgBurnReserveAssets) {
		amount, retained, ok := movableNoah(ctx, k, testData.Rand())
		if !ok {
			reporter.Skip("reserve cannot burn while retaining a floor")

			return nil, nil
		}

		return nil, &types.MsgBurnReserveAssets{
			Authority:             testData.ModuleAccountAddress(reporter, "gov"),
			Amounts:               sdk.NewCoins(chain.NoahCoin(amount)),
			MinimumReserveBalance: chain.NoahCoin(retained),
		}
	}
}

// The position messages below are governance's own route to the transitions the
// committee also holds. They became reachable once committee deployment opened
// positions to act on; before that, every one of them skipped every draw.

// MsgMarkImpairedFactory marks a sound position impaired on governance's
// authority.
func MsgMarkImpairedFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgMarkImpaired] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgMarkImpaired) {
		position, ok := openPosition(ctx, k, testData, reporter,
			"no sound position to impair",
			func(p types.Position) bool { return !p.Impaired },
		)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgMarkImpaired{
			Authority:  testData.ModuleAccountAddress(reporter, "gov"),
			PositionId: position.PositionId,
			Reference:  "sim-gov-impair-" + testData.Rand().StringN(8),
		}
	}
}

// MsgClearImpairmentFactory restores an impaired position on governance's
// authority.
func MsgClearImpairmentFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgClearImpairment] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgClearImpairment) {
		position, ok := openPosition(ctx, k, testData, reporter,
			"no impaired position to clear",
			func(p types.Position) bool { return p.Impaired },
		)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgClearImpairment{
			Authority:  testData.ModuleAccountAddress(reporter, "gov"),
			PositionId: position.PositionId,
			Reference:  "sim-gov-clear-" + testData.Rand().StringN(8),
		}
	}
}

// MsgClosePositionFactory closes a position on governance's authority.
func MsgClosePositionFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgClosePosition] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgClosePosition) {
		position, ok := openPosition(ctx, k, testData, reporter,
			"no open position to close",
			func(types.Position) bool { return true },
		)
		if !ok {
			return nil, nil
		}

		return nil, &types.MsgClosePosition{
			Authority:  testData.ModuleAccountAddress(reporter, "gov"),
			PositionId: position.PositionId,
			Reference:  "sim-gov-close-" + testData.Rand().StringN(8),
		}
	}
}

// MsgCorrectPositionFactory restates a prior entry on governance's authority.
func MsgCorrectPositionFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCorrectPosition] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCorrectPosition) {
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

		return nil, &types.MsgCorrectPosition{
			Authority:  testData.ModuleAccountAddress(reporter, "gov"),
			PositionId: position.PositionId,
			Corrects:   entry.EntryId,
			// Restating in the position's own denomination needs no holdability
			// check, which the handler applies only to a change of denomination.
			Quantity:       sdk.NewCoin(position.Quantity.Denom, quantity),
			VenueReference: "",
			Reference:      "sim-gov-correct-" + r.StringN(8),
		}
	}
}

// MsgReverseReturnFactory withdraws a return attribution on governance's
// authority. Only a return entry can be reversed, and only once.
func MsgReverseReturnFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgReverseReturn] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgReverseReturn) {
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

		return nil, &types.MsgReverseReturn{
			Authority:  testData.ModuleAccountAddress(reporter, "gov"),
			PositionId: position.PositionId,
			Reverses:   entry.EntryId,
			Reference:  "sim-gov-reverse-" + testData.Rand().StringN(8),
		}
	}
}
