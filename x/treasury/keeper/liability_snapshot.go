package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// liabilityValuationGas is the flat gas charged for one transaction-time
// aggregate-liability valuation, the preblocker having done the real scan as
// unmetered block work. Transient stores opened through the store service are
// metered with the KV gas config — 1,000 flat plus 3 per key and value byte,
// so a typical valuation reads 1,069 and the largest storable LegacyDec
// 1,291. Recalibrate if the app customises store gas configs or adds a second
// key.
const liabilityValuationGas = 2_000

// liabilityValuationKey holds the block's claimable-liability snapshot: one
// completeness flag byte followed by a marshalled LegacyDec. The value is the
// liability that can currently redeem; the flag records whether some
// unclaimable exposure was excluded. Rates are fixed at preblock, so neither
// can change within the block.
var liabilityValuationKey = []byte{0x01}

const (
	// liabilitySnapshotIncomplete marks a snapshot whose block excluded some
	// recognised exposure — a member without a fresh feed, or suspended supply
	// without a plan. That supply cannot itself redeem, so the aggregate stays
	// an honest denominator for every draw that can arrive.
	liabilitySnapshotIncomplete byte = 0x00
	// liabilitySnapshotComplete marks a snapshot that valued every recognised
	// liability.
	liabilitySnapshotComplete byte = 0x01
)

// cachedLiabilityValue returns the current block's claimable liability and
// whether it covers every recognised liability, charging a flat fee and
// evaluating the lookup itself against a free meter. Do not free-meter the
// query path: FundStatus builds the partition directly so node query gas limits
// keep bounding its work.
func (k Keeper) cachedLiabilityValue(ctx context.Context, rates oracletypes.RateSet) (math.LegacyDec, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.GasMeter().ConsumeGas(liabilityValuationGas, "treasury liability valuation")

	freeCtx := sdkCtx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	liability, complete, found, err := k.loadLiabilityValuation(freeCtx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if found {
		return liability, complete, nil
	}

	partition, err := k.liabilityPartitionValue(freeCtx, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	recognised, err := partition.recognizedNoah()
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if err := k.storeLiabilitySnapshot(freeCtx, recognised, partition.complete); err != nil {
		return math.LegacyDec{}, false, err
	}

	// This branch is only reached after InvalidateRegistryCache dropped the
	// block's snapshot. Without a second disclosure the block would report
	// itself healthy in the block an asset failed.
	if !partition.complete {
		if err := freeCtx.EventManager().EmitTypedEvent(&types.EventLiabilityIncomplete{
			ClaimableLiability:       chain.NoahDecCoin(recognised),
			StaleMemberSupply:        partition.stale,
			UntrustedSuspendedSupply: partition.untrusted,
		}); err != nil {
			return math.LegacyDec{}, false, fmt.Errorf("emitting Treasury incomplete liability event: %w", err)
		}
	}
	return recognised, partition.complete, nil
}

// loadLiabilityValuation reads the block's recorded snapshot: the claimable
// aggregate, its completeness flag, and whether the block has valued at all.
func (k Keeper) loadLiabilityValuation(ctx context.Context) (math.LegacyDec, bool, bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilityValuationKey)
	if err != nil {
		return math.LegacyDec{}, false, false, fmt.Errorf("getting cached liability: %w", err)
	}
	if bz == nil {
		return math.LegacyDec{}, false, false, nil
	}
	if len(bz) < 2 {
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability is truncated")
	}
	var complete bool
	switch bz[0] {
	case liabilitySnapshotComplete:
		complete = true
	case liabilitySnapshotIncomplete:
		complete = false
	default:
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability flag %#x is invalid", bz[0])
	}

	var liability math.LegacyDec
	if err := liability.Unmarshal(bz[1:]); err != nil {
		return math.LegacyDec{}, false, false, fmt.Errorf("decoding cached liability: %w", err)
	}
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability is invalid")
	}
	return liability, complete, true, nil
}

func (k Keeper) storeLiabilitySnapshot(ctx context.Context, liability math.LegacyDec, complete bool) error {
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return fmt.Errorf("cannot cache invalid liability")
	}
	encoded, err := liability.Marshal()
	if err != nil {
		return fmt.Errorf("encoding cached liability: %w", err)
	}
	flag := liabilitySnapshotIncomplete
	if complete {
		flag = liabilitySnapshotComplete
	}
	value := make([]byte, 0, len(encoded)+1)
	value = append(value, flag)
	value = append(value, encoded...)

	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityValuationKey, value); err != nil {
		return fmt.Errorf("setting cached liability: %w", err)
	}
	return nil
}
