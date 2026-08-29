package app

import (
	"testing"

	gmp "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func TestGMPWiring(t *testing.T) {
	arkApp := Setup(t, false)

	require.NotNil(t, arkApp.GMPKeeper)
	require.Contains(t, arkApp.ModuleManager.Modules, gmptypes.ModuleName)

	// GMP is a v2-only application and carries no middleware of its own.
	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(gmptypes.PortID))
	require.IsType(
		t,
		&gmp.IBCModule{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(gmptypes.PortID),
	)
	require.False(t, arkApp.IBCKeeper.PortKeeper.Router.HasRoute(gmptypes.ModuleName),
		"GMP has no Classic route")

	// It holds no module account: a derived account is an ordinary account, and
	// the module itself never takes custody.
	require.NotContains(t, arkApp.AccountKeeper.GetModulePermissions(), gmptypes.ModuleName)

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		gmptypes.ModuleName,
	)
}

// D48's substance: GMP and the contract runtime dispatch through one router, so
// a derived account is charged execution-generated tax on exactly the terms a
// contract is. This pins that they are the same construction rather than two
// that happen to agree today.
func TestGMPSharesTheContractTaxRouter(t *testing.T) {
	arkApp := Setup(t, false)

	router := arkApp.treasuryMessageRouter()
	require.Equal(t, arkApp.MsgServiceRouter(), router.inner)
	require.Equal(t, arkApp.TreasuryKeeper, router.treasury)
	require.Equal(t, arkApp.appCodec, router.cdc)
}

// A derived account pays tax from its own balance, on top of the principal it
// moves — the same charge a contract takes, through the same wrapper. GMP has
// already refused any message not signed by the derived account before this
// point, so the signer the router bills is that account by construction.
func TestGMPDerivedAccountPaysExecutionTax(t *testing.T) {
	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{})

	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Set(ctx, policy))
	setDerivedTaxCap(t, arkApp, ctx, chain.USDBaseDenom, math.NewInt(10_000))

	// Stands in for the account GMP derives for a remote caller: an ordinary
	// account holding an ordinary balance.
	derived := authtypes.NewModuleAddress("gmp-derived-stand-in")
	funds := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000))
	require.NoError(t, arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, funds))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, markettypes.ModuleName, derived, funds))

	inner := &stubRouter{}
	router := arkApp.treasuryMessageRouter()
	router.inner = inner

	msg := &banktypes.MsgSend{
		FromAddress: derived.String(),
		ToAddress:   authtypes.NewModuleAddress("recipient").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}
	_, err := router.Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.True(t, inner.called)

	collector := arkApp.AccountKeeper.GetModuleAddress(treasurytypes.StabilityTaxCollectorName)
	require.Equal(t, math.NewInt(100), arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(9_900), arkApp.BankKeeper.GetBalance(ctx, derived, chain.USDBaseDenom).Amount)
}
