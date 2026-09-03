package app_test

// Module ordering is app wiring rather than anything the keepers can enforce:
// a reordering compiles, passes every module's tests, and changes the
// economics on every node at once. These tests pin the orders that matter.

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func requireOrderBefore(t *testing.T, order []string, first, second string) {
	t.Helper()
	firstIndex := slices.Index(order, first)
	secondIndex := slices.Index(order, second)
	require.NotEqual(t, -1, firstIndex, "%s missing from lifecycle order", first)
	require.NotEqual(t, -1, secondIndex, "%s missing from lifecycle order", second)
	require.Less(t, firstIndex, secondIndex, "%s must run before %s", first, second)
}

// TestMarketSettlesBeforeEveryOtherEndBlocker pins the ordering conversion
// settlement depends on.
//
// Market's EndBlocker is where the block's conversions are valued and placed,
// so it has to run under the lifecycle regime the block's swaps quoted under
// and before anything else moves the state it settles against. Governance
// enacts lifecycle transitions in its EndBlocker and Claims pays due claims out
// of Insurance in its own; either running first would settle the block against
// a registry or a fund balance that its conversions never saw.
//
// The order is app wiring rather than anything the keepers can enforce, which
// is why it is pinned here: a reordering compiles, passes every module's tests,
// and changes the economics on every node at once.
func TestMarketSettlesBeforeEveryOtherEndBlocker(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderEndBlockers

	marketAt := slices.Index(order, markettypes.ModuleName)
	require.GreaterOrEqual(t, marketAt, 0, "market must have an EndBlocker")
	require.Equal(t, 0, marketAt, "market must settle before every other EndBlocker: %v", order)

	// Named individually so a failure says which actor would have moved first.
	for _, later := range []string{
		govtypes.ModuleName,
		claimstypes.ModuleName,
		banktypes.ModuleName,
	} {
		at := slices.Index(order, later)
		if at < 0 {
			continue
		}
		require.Greater(t, at, marketAt, "%s must run after conversion settlement", later)
	}
}

// TestDistributionLeadsSlashingInBeginBlockers pins the one ordering the
// BeginBlock list still owes anyone: upstream's documented invariant that
// slashing runs after distribution, so nothing is left in the validator fee
// pool when it does. Treasury's old lead slot is gone with its reason — the
// reward-funding window now advances in the EndBlocker, where the collector
// holds the block's own fees.
func TestDistributionLeadsSlashingInBeginBlockers(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
	)
}

// TestOracleJailsBeforeStakingEmitsValidatorUpdates pins the EndBlock order an
// attendance jail relies on. Oracle jails in its EndBlocker and staking emits
// the block's validator-set update in its own, so Oracle ahead of staking puts
// the jail in this block's update; behind it, the jailed validator signs one
// more block. Gov stays ahead of Oracle so a live attendance-ratio change
// applies at the same settlement.
func TestOracleJailsBeforeStakingEmitsValidatorUpdates(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderEndBlockers

	requireOrderBefore(t, order, govtypes.ModuleName, oracletypes.ModuleName)
	requireOrderBefore(t, order, oracletypes.ModuleName, stakingtypes.ModuleName)
}
