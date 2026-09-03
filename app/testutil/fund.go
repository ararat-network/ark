package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
	markettypes "github.com/ararat-network/ark/x/market/types"
)

// Market is the one module account holding the mint permission, so every
// fixture that needs coins mints through it.
const mintModule = markettypes.ModuleName

// FundAccount mints coins and sends them to addr.
func FundAccount(tb testing.TB, arkApp *app.ArkApp, ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	tb.Helper()

	require.NoError(tb, arkApp.BankKeeper.MintCoins(ctx, mintModule, coins))
	require.NoError(tb, arkApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, mintModule, addr, coins))
}

// FundModule mints coins and sends them to a module account.
func FundModule(tb testing.TB, arkApp *app.ArkApp, ctx sdk.Context, module string, coins sdk.Coins) {
	tb.Helper()

	require.NoError(tb, arkApp.BankKeeper.MintCoins(ctx, mintModule, coins))
	require.NoError(tb, arkApp.BankKeeper.SendCoinsFromModuleToModule(ctx, mintModule, module, coins))
}
