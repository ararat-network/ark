package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	assetkeeper "github.com/ararat-network/ark/x/asset/keeper"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
)

func TestAssetActivationGenesis(t *testing.T) {
	f := newActivationFixture(t)
	ctx := f.readCtx()

	defaults := assettypes.DefaultGenesisState()
	for _, expected := range defaults.Assets {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, expected.Denom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)

		// Asset is the sole owner of native-denom Bank metadata now that oracle
		// no longer registers it alongside Tobin entries.
		metadata, found := f.app.BankKeeper.GetDenomMetaData(ctx, expected.Denom)
		require.True(t, found, "bank metadata for %s", expected.Denom)
		require.Equal(t, expected.Metadata, metadata)
	}

	denoms, err := f.app.AssetKeeper.OraclePricedDenoms(ctx)
	require.NoError(t, err)
	require.Len(t, denoms, len(defaults.Assets))

	// Every oracle-priced denom keys a feed the oracle actually carries, which is
	// the coherence asset's InitGenesis validated on the way in.
	feeds, err := f.app.OracleKeeper.GetFeeds(ctx, f.height)
	require.NoError(t, err)
	for _, denom := range denoms {
		require.Contains(t, feeds.Denoms, denom)
	}

	exported, err := f.app.AssetKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Equal(t, defaults.Assets, exported.Assets)
	require.Equal(t, defaults.EmergencyMandate, exported.EmergencyMandate)
}

func TestFeedRemovalGuardedByLiveAssets(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// A launch feed is pinned by the asset priced from it: removal would stall
	// conversion for a live denomination. The registries share one key, so the
	// guard is a point lookup rather than a scan.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.USDBaseDenom,
		})
		require.ErrorIs(t, err, oracletypes.ErrFeedReferenced)
		require.ErrorContains(t, err, "asset "+chain.USDBaseDenom)
	})

	// A feed no asset references is removable, which is what makes the guard a
	// referent check rather than a blanket freeze on the registry.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	f.advanceToFeed(goldDenom)

	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})

	feeds, err := f.app.OracleKeeper.GetFeeds(f.readCtx(), f.height+oracletypes.FeedActivationDelayBlocks)
	require.NoError(t, err)
	require.NotContains(t, feeds.Denoms, goldDenom)
}

// TestFeedRemovalGuardedByProtocolReferenceDenom pins the reference denom's only
// protection. The launch reference denomination carries no listed asset at all,
// so the feed layer's referent guard is the single thing holding its feed: if
// that guard did not hold, Market's base pool and Treasury's cap would end up
// denominated in a unit the chain no longer observes.
func TestFeedRemovalGuardedByProtocolReferenceDenom(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	referenceDenom, err := f.app.OracleKeeper.GetReferenceDenom(f.readCtx())
	require.NoError(t, err)
	require.Equal(t, chain.XDRBaseDenom, referenceDenom)

	// The reference denom names a feed, not an asset, and the registry is the proof:
	// nothing is listed under the reference denomination, so no asset-side
	// guard can be what rejects the removal below.
	_, err = f.app.AssetKeeper.Assets.Get(f.readCtx(), chain.XDRBaseDenom)
	require.Error(t, err)

	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.XDRBaseDenom,
		})
		require.ErrorIs(t, err, oracletypes.ErrFeedReferenced)
		require.ErrorContains(t, err, "protocol reference")
	})
}

// TestAssetListingTakesTwoGovernanceActs drives the whole listing path through
// real wiring. Registration answers the same admission rule as recovery and
// genesis import, so a feed addition and the registration against it cannot
// share a proposal: the feed has to be running first.
func TestAssetListingTakesTwoGovernanceActs(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// Both halves in one block is what the rule refuses. The feed is scheduled
	// and not yet in the active set, so the registration has no price source to
	// be admitted against and leaves no registry row behind.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)

		err = f.app.AssetKeeper.RegisterAsset(ctx, goldDenom)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)

		listed, err := f.app.AssetKeeper.Assets.Has(ctx, goldDenom)
		require.NoError(t, err)
		require.False(t, listed)
	})

	// The second act lands once the feed is running, which is the whole point of
	// the rule: governance has rates to watch before it votes on the listing.
	f.advanceToFeed(goldDenom)
	f.nextBlock(func(ctx sdk.Context) {
		require.NoError(t, f.app.AssetKeeper.RegisterAsset(ctx, goldDenom))

		listed, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, listed.Status)
		require.Equal(t, uint64(1), listed.Version)
	})

	// It pins the feed from that block, so the feed cannot be scheduled away
	// underneath the asset now listed against it.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.ErrorIs(t, err, oracletypes.ErrFeedReferenced)
		require.ErrorContains(t, err, "asset "+goldDenom)
	})

	pricings, err := f.app.AssetKeeper.Pricings(f.readCtx(), goldDenom)
	require.NoError(t, err)
	require.True(t, pricings[goldDenom].IsPriced())
}

// TestAssetRegistrationRequiresAFeed pins the other half of the admission rule.
// A denomination the oracle does not carry has no price source to be admitted
// against, and a feed on its way out is not one either.
func TestAssetRegistrationRequiresAFeed(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	f.nextBlock(func(ctx sdk.Context) {
		err := f.app.AssetKeeper.RegisterAsset(ctx, goldDenom)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)

		registered, err := f.app.AssetKeeper.Assets.Has(ctx, goldDenom)
		require.NoError(t, err)
		require.False(t, registered)
	})

	// A feed scheduled for removal is refused too: it is leaving, so an asset
	// admitted against it would be unpriced the moment the removal activates.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	f.advanceToFeed(goldDenom)
	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)

		err = f.app.AssetKeeper.RegisterAsset(ctx, goldDenom)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)
	})
}

// TestAssetLifecycleActivationThroughRetirement walks one newly registered
// commodity from an empty denomination to a tombstone, asserting at each step
// what crosses a module boundary: what Bank owns, when consensus completes an
// activation, when Treasury starts taxing, what Market permits, and what the
// feed layer releases. The single-module transitions inside each step are the
// keeper suite's business; this is about the seams holding in sequence.
func TestAssetLifecycleActivationThroughRetirement(t *testing.T) {
	f := newActivationFixture(t)
	oracleMsgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	assetQueryServer := assetkeeper.NewQueryServerImpl(f.app.AssetKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// Registration is the asset-Bank seam, and it refuses a denomination that
	// already exists in any form: supply nobody accounted for, or metadata
	// another owner wrote. Both checks run before the feed check, so neither
	// needs a feed to prove.
	f.nextBlock(func(ctx sdk.Context) {
		const preSupplied = "aplatinum"
		require.NoError(t, f.app.BankKeeper.MintCoins(
			ctx,
			markettypes.ModuleName,
			sdk.NewCoins(sdk.NewInt64Coin(preSupplied, 1)),
		))
		err := f.app.AssetKeeper.RegisterAsset(ctx, preSupplied)
		require.ErrorIs(t, err, assettypes.ErrAssetSupplyNotZero)

		const preDescribed = "acopper"
		f.app.BankKeeper.SetDenomMetaData(ctx, chain.NativeAssetMetadata(preDescribed))
		err = f.app.AssetKeeper.RegisterAsset(ctx, preDescribed)
		require.ErrorIs(t, err, assettypes.ErrAssetAlreadyExists)
		require.ErrorContains(t, err, "Bank metadata")
	})

	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	f.advanceToFeed(goldDenom)

	// Registration derives the metadata from the denomination, so Bank's record
	// is settled the moment the asset exists. There is no mistaken variant to
	// correct here because the proposal never supplied a value that could be
	// wrong.
	derived := chain.NativeAssetMetadata(goldDenom)
	f.nextBlock(func(ctx sdk.Context) {
		// Registration is admission: the feed is live, so the asset is ACTIVE in
		// this block.
		require.NoError(t, f.app.AssetKeeper.RegisterAsset(ctx, goldDenom))

		registered, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, registered.Status)

		stored, found := f.app.BankKeeper.GetDenomMetaData(ctx, goldDenom)
		require.True(t, found)
		require.Equal(t, derived, stored)
	})

	// The next block's BeginBlocker rebuilt the caps and saw the new member.
	activated, err := f.app.AssetKeeper.Assets.Get(f.readCtx(), goldDenom)
	require.NoError(t, err)
	require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, activated.Status)
	hasCap, err := f.app.TreasuryKeeper.ConversionFactors.Has(f.readCtx(), goldDenom)
	require.NoError(t, err)
	require.True(t, hasCap, "activated asset joined the cap set")

	// Bank's record is the asset's permanent identity: derived once at
	// registration, carried through activation, and reachable by no message.
	taxRate := math.LegacyNewDecWithPrec(1, 3)
	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		stored, found := f.app.BankKeeper.GetDenomMetaData(ctx, goldDenom)
		require.True(t, found)
		require.Equal(t, derived, stored)

		// Treasury taxes it, which is the whole point of joining the oracle-priced
		// set: the new denomination is protocol-convertible money now.
		params, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		params.TransferTaxRate = taxRate
		require.NoError(t, f.app.TreasuryKeeper.Params.Set(ctx, params))
		// The launch reference cap clamps tax to one base unit; lift the
		// ceiling so the assertion reads the rate, not the clamp.
		treasurytestutil.SetDerivedTaxCap(t, f.app.TreasuryKeeper, ctx, goldDenom, math.NewInt(1_000_000))

		sendAmount := math.NewInt(1_000_000)
		tax, _, err := f.app.TreasuryKeeper.ComputeTax(ctx, []sdk.Msg{&banktypes.MsgSend{
			FromAddress: f.trader.String(),
			ToAddress:   f.trader.String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(goldDenom, sendAmount)),
		}})
		require.NoError(t, err)
		require.Equal(
			t,
			sdk.NewCoins(sdk.NewCoin(goldDenom, taxRate.MulInt(sendAmount).TruncateInt())),
			tax,
		)

		// And Market issues it: conversion into a new denomination is the only
		// way its supply comes into existence.
		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       goldDenom,
			MinimumReceive: sdk.NewCoin(goldDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin
		require.True(t, acquired.IsPositive())
	})

	// Halting issuance closes the entrance and leaves the exit open, which is
	// what makes the residual bound below a judgement about holders who chose
	// not to leave rather than about holders who could not.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.HaltIssuance(ctx, goldDenom, asset.Version))

		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1)),
			AskDenom:       goldDenom,
			MinimumReceive: sdk.NewCoin(goldDenom, math.OneInt()),
		})
		require.ErrorIs(t, err, markettypes.ErrIneligibleAsset)
	})

	// Retirement refuses to derecognize more than governance approved.
	residual := math.NewInt(1_000)
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		err = f.app.AssetKeeper.FinaliseRetirement(ctx, goldDenom, asset.Version, residual)
		require.ErrorIs(t, err, assettypes.ErrAssetSupplyNotZero)
		require.ErrorContains(t, err, "exceeds approved residual bound")

		// Redeem down to the dust the bound covers. The exit was open the whole
		// time the halt was in force, so this is the ordinary conversion path.
		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(goldDenom, acquired.Amount.Sub(residual)),
			AskDenom:       chain.NoahBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.NoahBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		require.Equal(t, residual, f.app.BankKeeper.GetSupply(ctx, goldDenom).Amount)
	})

	var retiredVersion uint64
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.FinaliseRetirement(ctx, goldDenom, asset.Version, residual))
		retiredVersion = asset.Version + 1

		retired, err := f.app.AssetKeeper.Assets.Get(ctx, goldDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_RETIRED, retired.Status)

		// The residual is disclosed as derecognized, not quietly dropped.
		history, err := assetQueryServer.ResolutionHistory(ctx, &assettypes.QueryResolutionHistoryRequest{
			Denom: goldDenom,
		})
		require.NoError(t, err)
		require.Len(t, history.ResolutionRecords, 1)
		record := history.ResolutionRecords[0]
		require.Equal(t, assettypes.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL, record.Kind)
		require.Equal(t, retiredVersion, record.Version)
		require.Equal(t, sdk.NewCoin(goldDenom, residual), record.OutstandingSupply)

		// Derecognition is an accounting act, not a confiscation: the balances
		// still move.
		recipient := sdk.AccAddress([]byte("residual-holder....."))
		require.NoError(t, f.app.BankKeeper.SendCoins(
			ctx,
			f.trader,
			recipient,
			sdk.NewCoins(sdk.NewCoin(goldDenom, residual)),
		))
		require.Equal(t, residual, f.app.BankKeeper.GetBalance(ctx, recipient, goldDenom).Amount)
	})

	// Treasury stops recognising it as exposure in the same motion: a retired
	// denomination is invisible to the liability report. Its cap stays, though
	// — the residual just changed hands above, and a transfer tax that
	// exempted derecognized supply would make it the cheapest money to move.
	f.nextBlock(nil)
	readCtx := f.readCtx()
	hasCap, err = f.app.TreasuryKeeper.ConversionFactors.Has(readCtx, goldDenom)
	require.NoError(t, err)
	require.True(t, hasCap, "retired denomination keeps its cap")

	// A retired asset references nothing, so the feed the chain no longer needs
	// is finally removable — the same guard that vetoed removal while it lived.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	for range oracletypes.FeedActivationDelayBlocks + 2 {
		f.nextBlock(nil)
	}

	readCtx = f.readCtx()
	feeds, err := f.app.OracleKeeper.GetFeeds(readCtx, f.height)
	require.NoError(t, err)
	require.NotContains(t, feeds.Denoms, goldDenom)
	hasRate, err := f.app.OracleKeeper.ExchangeRate.Has(readCtx, goldDenom)
	require.NoError(t, err)
	require.False(t, hasRate, "removal pruned the rate")

	// The tombstone is permanent both ways: the denomination cannot be
	// re-registered, and retirement is terminal, so nothing carries the asset
	// back out under the name either. A successor uses a new denomination.
	f.nextBlock(func(ctx sdk.Context) {
		err := f.app.AssetKeeper.RegisterAsset(ctx, goldDenom)
		require.ErrorIs(t, err, assettypes.ErrAssetAlreadyExists)

		err = f.app.AssetKeeper.RecoverAsset(ctx, goldDenom, retiredVersion)
		require.ErrorIs(t, err, assettypes.ErrInvalidAssetTransition)
	})
}
