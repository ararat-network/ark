package integration

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservekeeper "github.com/ararat-network/ark/x/reserve/keeper"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// goldExternal is the Reserve's claim on the gold series: the feed prices the
// series, the claim names the custody the fund attests to.
const goldExternal = goldDenom + "-x"

// TestReserveCommitteeTransferBoundIsTreasurys checks real Reserve actions use Treasury's live
// shortfall, contrasting bounded committee refill with governance transfer on the same state.
func TestReserveCommitteeTransferBoundIsTreasurys(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	msgServer := reservekeeper.NewMsgServerImpl(arkApp.ReserveKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	committee := sdk.AccAddress([]byte("reserve-committee---")).String()

	// Every listed denomination carries a rate, so the aggregate values
	// completely and the targets can be sized at all.
	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	// Stablecoin in circulation is the liability every fund target scales.
	stableSupply := math.NewInt(1_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, stableSupply)),
	))
	// The mint bypasses Market and needs no priming: every committee bound
	// below folds the registry itself, at the moment it is asked.
	policy, err := arkApp.TreasuryKeeper.EconomicPolicy.Get(ctx)
	require.NoError(t, err)
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.1")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.05")
	require.NoError(t, arkApp.TreasuryKeeper.EconomicPolicy.Set(ctx, policy))

	// The Reserve holds far more than either gap, so what stops a transfer is
	// the destination's target and never the source running dry.
	reserveSeed := math.NewInt(500_000_000)
	apptestutil.FundModule(
		t,
		arkApp,
		ctx,
		reservetypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, reserveSeed)),
	)

	_, err = msgServer.SetReserveMandate(ctx, &reservetypes.MsgSetReserveMandate{
		Authority:           authority,
		Committee:           committee,
		ActivationHeight:    uint64(ctx.BlockHeight()),
		ExpiryHeight:        uint64(ctx.BlockHeight()) + 10_000,
		DeploymentAllowance: sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1_000_000)),
		MinimumNoahBalance:  sdk.NewCoin(chain.NoahBaseDenom, math.ZeroInt()),
		Destinations:        []string{sdk.AccAddress([]byte("deployment-target---")).String()},
	})
	require.NoError(t, err)

	mandate, err := arkApp.ReserveKeeper.Mandate.Get(ctx)
	require.NoError(t, err)
	term := mandate.Term

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	bufferBefore := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount

	// The bound is Treasury's own answer, read here exactly as the handler
	// reads it. A drained Buffer against live liability owes something.
	gap, err := arkApp.TreasuryKeeper.RedemptionBufferShortfall(ctx)
	require.NoError(t, err)
	require.True(t, gap.IsPositive(), "expected a positive Buffer shortfall, got %s", gap)

	// One anoah past Treasury's figure is refused, which is what proves the
	// ceiling is that figure and not something the Reserve chose.
	_, err = msgServer.CommitteeFundBuffer(ctx, &reservetypes.MsgCommitteeFundBuffer{
		Committee:    committee,
		ExpectedTerm: term,
		Amount:       sdk.NewCoin(chain.NoahBaseDenom, gap.AddRaw(1)),
	})
	require.ErrorContains(t, err, "exceeds the")

	// Filling the gap exactly is permitted, and the coins really move.
	resp, err := msgServer.CommitteeFundBuffer(ctx, &reservetypes.MsgCommitteeFundBuffer{
		Committee:    committee,
		ExpectedTerm: term,
		Amount:       sdk.NewCoin(chain.NoahBaseDenom, gap),
	})
	require.NoError(t, err)
	require.True(t, resp.RemainingShortfall.Amount.IsZero())
	require.Equal(
		t,
		bufferBefore.Add(gap),
		arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount,
	)

	// The power exhausted itself at the target line: Treasury now reports the
	// Buffer as funded, so the same message is refused for a single anoah.
	refreshed, err := arkApp.TreasuryKeeper.RedemptionBufferShortfall(ctx)
	require.NoError(t, err)
	require.True(t, refreshed.IsZero(), "expected the shortfall closed, got %s", refreshed)
	_, err = msgServer.CommitteeFundBuffer(ctx, &reservetypes.MsgCommitteeFundBuffer{
		Committee:    committee,
		ExpectedTerm: term,
		Amount:       sdk.NewCoin(chain.NoahBaseDenom, math.OneInt()),
	})
	require.ErrorContains(t, err, "shortfall 0anoah")

	// Governance is bounded by nothing of the kind: the same overfunded Buffer
	// takes an unbounded commitment. This is the asymmetry the two channels
	// exist for, and it is what keeps a governance transfer available in the
	// states where the committee's bound cannot be computed at all.
	_, err = msgServer.FundBuffer(ctx, &reservetypes.MsgFundBuffer{
		Authority:             authority,
		Amount:                sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1_000)),
		MinimumReserveBalance: sdk.NewCoin(chain.NoahBaseDenom, math.ZeroInt()),
	})
	require.NoError(t, err)

	// Insurance is measured on its own target through its own message, and
	// filling the Buffer did nothing to that gap.
	insuranceGap, err := arkApp.TreasuryKeeper.InsuranceShortfall(ctx)
	require.NoError(t, err)
	require.True(t, insuranceGap.IsPositive())

	insuranceAddress := authtypes.NewModuleAddress(claimstypes.InsuranceName)
	insuranceBefore := arkApp.BankKeeper.GetBalance(ctx, insuranceAddress, chain.NoahBaseDenom).Amount
	_, err = msgServer.CommitteeFundInsurance(ctx, &reservetypes.MsgCommitteeFundInsurance{
		Committee:    committee,
		ExpectedTerm: term,
		Amount:       sdk.NewCoin(chain.NoahBaseDenom, insuranceGap),
	})
	require.NoError(t, err)
	require.Equal(
		t,
		insuranceBefore.Add(insuranceGap),
		arkApp.BankKeeper.GetBalance(ctx, insuranceAddress, chain.NoahBaseDenom).Amount,
	)

	// Neither transfer consumed deployment allowance: they moved capital
	// between protocol funds rather than converting it into external exposure.
	used, err := arkApp.ReserveKeeper.AllowanceUsed.Get(ctx)
	require.NoError(t, err)
	require.True(t, used.IsZero())
}

// TestReserveFeedGuardBlocksRemoval checks app wiring registers Reserve's guard. The external feed
// has no Asset record, isolating Reserve as the sole foreign blocker.
func TestReserveFeedGuardBlocksRemoval(t *testing.T) {
	f := newActivationFixture(t)
	oracleMsgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	reserveMsgServer := reservekeeper.NewMsgServerImpl(f.app.ReserveKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// A feed with no asset behind it: priced, unlisted, exactly what the
	// Reserve may hold externally.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	f.advanceToFeed(goldDenom)

	// Removal is free while nothing claims it, and this is the control: the
	// rejection below has to come from the entry, not from the fixture.
	readCtx := f.readCtx()
	referents, err := f.app.ReserveKeeper.FeedReferents(readCtx, goldDenom)
	require.NoError(t, err)
	require.Empty(t, referents)

	f.nextBlock(func(ctx sdk.Context) {
		_, err := reserveMsgServer.SetRecognitionPolicy(ctx, &reservetypes.MsgSetRecognitionPolicy{
			Authority: authority,
			Entries: []reservetypes.EligibilityEntry{{
				Denom:               goldExternal,
				HaircutFactor:       math.LegacyMustNewDecFromStr("0.5"),
				RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.25"),
				MaxRateAge:          time.Hour,
			}},
		})
		require.NoError(t, err)
	})

	// The feed now prices capital the fund reports to Treasury, so removing it
	// would shrink recognised capital with nothing in the proposal saying so.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.ErrorIs(t, err, oracletypes.ErrFeedReferenced)
		require.ErrorContains(t, err, reservetypes.ModuleName)
		require.ErrorContains(t, err, "recognition policy")
	})

	// Delisting releases it: the guard enforces an order of operations rather
	// than a veto — stop counting the asset, then stop pricing it.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := reserveMsgServer.SetRecognitionPolicy(ctx, &reservetypes.MsgSetRecognitionPolicy{
			Authority: authority,
		})
		require.NoError(t, err)
	})
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
}

// TestReserveOpenPositionFeedGuardClearsOnGovernanceClosure checks governance can close a position
// and release its real Oracle feed guard without appointing a committee.
func TestReserveOpenPositionFeedGuardClearsOnGovernanceClosure(t *testing.T) {
	f := newActivationFixture(t)
	oracleMsgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	reserveMsgServer := reservekeeper.NewMsgServerImpl(f.app.ReserveKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	committee := sdk.AccAddress([]byte("reserve-committee---")).String()
	destination := sdk.AccAddress([]byte("deployment-target---")).String()

	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
	f.advanceToFeed(goldDenom)

	// Fund the Reserve and appoint a committee that can deploy into gold.
	f.nextBlock(func(ctx sdk.Context) {
		reserveSeed := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1_000_000)))
		// Market is the only account that may mint NOAH, so seeding goes through
		// it exactly as real expansion principal would.
		apptestutil.FundModule(t, f.app, ctx, reservetypes.StrategicReserveName, reserveSeed)
		_, err := reserveMsgServer.SetReserveMandate(ctx, &reservetypes.MsgSetReserveMandate{
			Authority:           authority,
			Committee:           committee,
			ActivationHeight:    uint64(ctx.BlockHeight()),
			ExpiryHeight:        uint64(ctx.BlockHeight()) + 10_000,
			DeploymentAllowance: sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1_000_000)),
			MinimumNoahBalance:  sdk.NewCoin(chain.NoahBaseDenom, math.ZeroInt()),
			Destinations:        []string{destination},
		})
		require.NoError(t, err)
	})

	var positionID uint64
	f.nextBlock(func(ctx sdk.Context) {
		mandate, err := f.app.ReserveKeeper.Mandate.Get(ctx)
		require.NoError(t, err)
		resp, err := reserveMsgServer.CommitteeDeploy(ctx, &reservetypes.MsgCommitteeDeploy{
			Committee:      committee,
			ExpectedTerm:   mandate.Term,
			Destination:    destination,
			Amount:         sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(400_000)),
			Acquired:       sdk.NewCoin(goldExternal, math.NewInt(40)),
			VenueReference: "custodian-alpha",
			Reference:      "tx-0x01",
		})
		require.NoError(t, err)
		positionID = resp.PositionId
	})

	// The open position claims the feed even though the asset was never listed
	// for recognition: this claim is about the ledger, not about capital.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.ErrorIs(t, err, oracletypes.ErrFeedReferenced)
		require.ErrorContains(t, err, reservetypes.ModuleName)
		require.ErrorContains(t, err, "open position")
	})

	// Governance revokes the mandate, leaving no appointed committee to close the position.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := reserveMsgServer.SetReserveMandate(ctx, &reservetypes.MsgSetReserveMandate{
			Authority: authority,
			Committee: "",
		})
		require.NoError(t, err)
	})

	f.nextBlock(func(ctx sdk.Context) {
		_, err := reserveMsgServer.ClosePosition(ctx, &reservetypes.MsgClosePosition{
			Authority:  authority,
			PositionId: positionID,
			Reference:  "asset-wind-down",
		})
		require.NoError(t, err)
	})

	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     goldDenom,
		})
		require.NoError(t, err)
	})
}

// TestReserveAndMarketValueARateIdentically checks both modules multiply by NOAH-per-unit rates.
// Distinct external and registered denominations share the same rate because one denomination
// cannot be both recognisable and convertible.
func TestReserveAndMarketValueARateIdentically(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	msgServer := reservekeeper.NewMsgServerImpl(arkApp.ReserveKeeper)
	marketQuery := marketkeeper.NewQueryServerImpl(arkApp.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// externalAsset is an external claim priced at 0.25 NOAH per unit, making inverted-rate
	// valuation visibly different from multiplication.
	const (
		externalFeed  = "aext"
		externalAsset = externalFeed + "-x"
	)
	rate := math.LegacyNewDecWithPrec(25, 2)
	// The base pool is denominated in XDR, so a NOAH pair quote reads that feed
	// as well as the offer's.
	// Listing a claim requires its series to be a live feed, the same rule asset
	// registration answers to.
	feeds, err := arkApp.OracleKeeper.Feeds.Get(ctx)
	require.NoError(t, err)
	feeds.Denoms = append(feeds.Denoms, externalFeed)
	sort.Strings(feeds.Denoms)
	require.NoError(t, arkApp.OracleKeeper.Feeds.Set(ctx, feeds))

	for _, denom := range []string{externalFeed, chain.USDBaseDenom, chain.XDRBaseDenom} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           rate,
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	quantity := math.NewInt(1_000_000)

	// The Reserve's leg. A full haircut leaves the credit as the raw valuation.
	_, err = msgServer.SetRecognitionPolicy(ctx, &reservetypes.MsgSetRecognitionPolicy{
		Authority: authority,
		Entries: []reservetypes.EligibilityEntry{{
			Denom:               externalAsset,
			HaircutFactor:       math.LegacyOneDec(),
			RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.9"),
			MaxRateAge:          time.Hour,
		}},
	})
	require.NoError(t, err)

	// Enough NOAH that the cap ratio cannot bind: this test is about the
	// valuation, and a clipped credit would measure the solve instead.
	base := math.NewInt(1_000_000_000)
	seed := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, base))
	apptestutil.FundModule(t, arkApp, ctx, reservetypes.StrategicReserveName, seed)
	// External custody is attested in an open position and priced through its derived feed. No Bank
	// coin is involved; Reserve admission rejects external symbols.
	require.NoError(t, arkApp.ReserveKeeper.OpenPositions.Set(ctx, 1, reservetypes.Position{
		PositionId:     1,
		Quantity:       sdk.NewCoin(externalAsset, quantity),
		Deployed:       sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(1)),
		Returned:       sdk.NewCoin(chain.NoahBaseDenom, math.ZeroInt()),
		VenueReference: "custodian-alpha",
		OpenedHeight:   1,
	}))

	recognised, err := arkApp.ReserveKeeper.RecognisedCapital(ctx)
	require.NoError(t, err)
	credit := recognised.Sub(base)

	// The Market's leg, at the same rate on a registry member. The gross is the
	// quote before the spread, which applySpread splits into exactly these two
	// parts — the fee carries the truncation dust, so the sum is the whole
	// pre-spread figure and nothing is lost in reassembling it.
	quote, err := marketQuery.Swap(ctx, &markettypes.QuerySwapRequest{
		OfferCoin: sdk.NewCoin(chain.USDBaseDenom, quantity).String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	require.NoError(t, err)
	gross := quote.SwapFee.Amount.Add(math.LegacyNewDecFromInt(quote.SwapCoin.Amount))

	require.Equal(t, gross.TruncateInt(), credit)
	// And the figure itself, so the test still names the direction if both
	// modules were ever wrong together: a million units at four to the NOAH are
	// worth a quarter of a million anoah, not four million.
	require.Equal(t, math.NewInt(250_000), credit)
}
