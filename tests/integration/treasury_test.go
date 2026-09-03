package integration

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimskeeper "github.com/ararat-network/ark/x/claims/keeper"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func TestMarketTreasurySettlementMaintainsBlockLiability(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Now(),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	// The reference unit is never traded — no asset is listed against it — but
	// it denominates Market's base pool, so every Noah quote reads its rate.
	for _, denom := range []string{
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		chain.EURBaseDenom,
	} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 200)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		trader,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		treasurytypes.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
	))
	initialStableSupply := math.NewInt(1_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, initialStableSupply)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		trader,
		sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, initialStableSupply)),
	))
	// The mint above stands in for supply that existed before this block. It
	// bypasses Market and needs no priming: settlement folds the registry at the
	// end of the block, so it counts this supply the same way it counts what the
	// block's own conversions created.
	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	expansion, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		AskDenom:       chain.USDBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
	})
	require.NoError(t, err)
	require.True(t, expansion.SwapCoin.IsPositive())
	require.Equal(
		t,
		initialStableSupply.Add(expansion.SwapCoin.Amount),
		arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
	)

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	bufferBefore := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
	redemptionOffer := sdk.NewCoin(
		chain.USDBaseDenom,
		math.NewInt(500),
	)
	redemption, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      redemptionOffer,
		AskDenom:       chain.NoahBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
	})
	require.NoError(t, err)
	require.True(t, redemption.SwapCoin.IsPositive())
	require.Equal(
		t,
		initialStableSupply.Add(expansion.SwapCoin.Amount).Sub(redemptionOffer.Amount),
		arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
	)
	// The Buffer has not moved yet: a redemption mints its whole output and the
	// Buffer's share of it is funded once, for the whole block, at settlement.
	require.Equal(t, bufferBefore, arkApp.BankKeeper.GetBalance(
		ctx,
		bufferAddress,
		chain.NoahBaseDenom,
	).Amount)

	require.NoError(t, arkApp.MarketKeeper.EndBlocker(ctx))

	require.True(t, arkApp.BankKeeper.GetBalance(
		ctx,
		bufferAddress,
		chain.NoahBaseDenom,
	).Amount.LT(bufferBefore))
}

func TestTreasuryFundRestrictionsRunThroughBank(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	goCtx := ctx
	bankMsgServer := bankkeeper.NewMsgServerImpl(arkApp.BankKeeper)

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x41}, 20))
	unrelated := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	// axdr is deliberately not in the asset registry's default genesis, so it is
	// the external counterpart to aeur throughout: the Reserve admits registry
	// members by membership and still refuses unlisted external custody.
	userFunding := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.EURBaseDenom, 100),
		sdk.NewInt64Coin(chain.XDRBaseDenom, 100),
	)
	protocolFunding := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 20),
		sdk.NewInt64Coin(chain.EURBaseDenom, 20),
	)
	requiredFunding := userFunding.Add(protocolFunding...)
	require.NoError(t, arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, requiredFunding))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		sender,
		userFunding,
	))
	noahSupply := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom)
	eurSupply := arkApp.BankKeeper.GetSupply(ctx, chain.EURBaseDenom)

	for _, moduleName := range treasurytypes.FundAccountNames() {
		fundAddress := authtypes.NewModuleAddress(moduleName)
		_, err := bankMsgServer.Send(goCtx, &banktypes.MsgSend{
			FromAddress: sender.String(),
			ToAddress:   fundAddress.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
		})
		require.NoError(t, err, moduleName)

		balanceBefore := arkApp.BankKeeper.GetAllBalances(ctx, fundAddress)
		_, err = bankMsgServer.Send(goCtx, &banktypes.MsgSend{
			FromAddress: sender.String(),
			ToAddress:   fundAddress.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 1)),
		})
		require.Error(t, err, moduleName)
		require.Equal(t, balanceBefore, arkApp.BankKeeper.GetAllBalances(ctx, fundAddress))

		_, err = bankMsgServer.Send(goCtx, &banktypes.MsgSend{
			FromAddress: sender.String(),
			ToAddress:   fundAddress.String(),
			Amount: sdk.NewCoins(
				sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
				sdk.NewInt64Coin(chain.EURBaseDenom, 1),
			),
		})
		require.Error(t, err, moduleName)
		require.Equal(t, balanceBefore, arkApp.BankKeeper.GetAllBalances(ctx, fundAddress))
	}

	_, err := bankMsgServer.Send(goCtx, &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   unrelated.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	})
	require.NoError(t, err)

	// The Reserve refuses unlisted external custody, and a MultiSend carrying it
	// fails whole: the leg to an unrelated account must not settle either.
	senderSdrBefore := arkApp.BankKeeper.GetBalance(ctx, sender, chain.XDRBaseDenom)
	unrelatedSdrBefore := arkApp.BankKeeper.GetBalance(ctx, unrelated, chain.XDRBaseDenom)
	reserveAddress := authtypes.NewModuleAddress(reservetypes.StrategicReserveName)
	reserveSdrBefore := arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.XDRBaseDenom)
	_, err = bankMsgServer.MultiSend(goCtx, &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: sender.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 2)),
		}},
		Outputs: []banktypes.Output{
			{Address: unrelated.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1))},
			{Address: reserveAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1))},
		},
	})
	require.Error(t, err)
	require.Equal(t, senderSdrBefore, arkApp.BankKeeper.GetBalance(ctx, sender, chain.XDRBaseDenom))
	require.Equal(t, unrelatedSdrBefore, arkApp.BankKeeper.GetBalance(ctx, unrelated, chain.XDRBaseDenom))
	require.Equal(t, reserveSdrBefore, arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.XDRBaseDenom))

	// aeur is a registry member, so the same shape settles: Ark-issued paper is
	// protocol liability wherever it sits, earns no recognition credit, and the
	// Reserve is the one account with a committee able to retire it. No
	// eligibility listing is involved — the policy cannot name a member at all.
	reserveEurBefore := arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.EURBaseDenom)
	_, err = bankMsgServer.MultiSend(goCtx, &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: sender.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 2)),
		}},
		Outputs: []banktypes.Output{
			{Address: unrelated.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 1))},
			{Address: reserveAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 1))},
		},
	})
	require.NoError(t, err)
	require.Equal(
		t,
		reserveEurBefore.Add(sdk.NewInt64Coin(chain.EURBaseDenom, 1)),
		arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.EURBaseDenom),
	)

	// Insurance is named separately: it is no longer a Treasury fund account,
	// but its restriction admits the same positive anoah-only deposit, and the
	// withdrawal below needs it funded.
	fundedAccounts := append(treasurytypes.FundAccountNames(), claimstypes.InsuranceName)
	for _, moduleName := range fundedAccounts {
		require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
			ctx,
			markettypes.ModuleName,
			moduleName,
			sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
		))
	}
	require.Error(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		claimstypes.InsuranceName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 1)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		claimstypes.InsuranceName,
		unrelated,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	))

	require.Equal(t, noahSupply, arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom))
	require.Equal(t, eurSupply, arkApp.BankKeeper.GetSupply(ctx, chain.EURBaseDenom))
}

// TestTreasurySettlementRoutesWrittenOffTaxToReserve drives a reward-funding
// settlement through the real Bank keeper while the collector holds tax of a
// written-off asset. The routing crosses the Treasury send restriction with
// non-NOAH coins, so this must run against the real restriction wiring: keeper
// tests mock Bank and structurally cannot catch a restriction rejecting the
// module's own settlement transfer, which would fail EndBlock and halt the
// chain.
func TestTreasurySettlementRoutesWrittenOffTaxToReserve(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 2})

	require.NoError(t, arkApp.AssetKeeper.Assets.Set(ctx, chain.USDBaseDenom, assettypes.Asset{
		Denom:    chain.USDBaseDenom,
		Metadata: banktypes.Metadata{Base: chain.USDBaseDenom},
		Status:   assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		Version:  3,
	}))
	taxCoins := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 4))
	require.NoError(t, arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, taxCoins))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		treasurytypes.StabilityTaxCollectorName,
		taxCoins,
	))

	funding := treasurytypes.DefaultRewardFundingState()
	funding.BlocksRemaining = 1
	require.NoError(t, arkApp.TreasuryKeeper.RewardFunding.Set(ctx, funding))

	require.NoError(t, arkApp.TreasuryKeeper.EndBlocker(ctx))
	require.Equal(
		t,
		math.NewInt(4),
		arkApp.BankKeeper.GetBalance(
			ctx,
			authtypes.NewModuleAddress(reservetypes.StrategicReserveName),
			chain.USDBaseDenom,
		).Amount,
	)
	require.True(t, arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName),
		chain.USDBaseDenom,
	).Amount.IsZero())
}

func TestTreasuryGovernanceFundAndPolicyConfiguration(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	voter := treasuryGovernanceVoter(t, arkApp, ctx)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	claimsCommittee := sdk.AccAddress(bytes.Repeat([]byte{0x43}, 20))
	monetaryCommittee := sdk.AccAddress(bytes.Repeat([]byte{0x46}, 20))

	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 150)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		reservetypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		claimstypes.InsuranceName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)),
	))

	transfer := &reservetypes.MsgFundBuffer{
		Authority:             authority,
		Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 40),
		MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 60),
	}
	claimsMandateUpdate := &claimstypes.MsgSetClaimsMandate{
		Authority:           authority,
		Committee:           claimsCommittee.String(),
		ActivationHeight:    1,
		ExpiryHeight:        1_000_000,
		CommitteeClaimLimit: sdk.NewInt64Coin(chain.NoahBaseDenom, 50),
	}
	minimumPolicy := treasurytypes.DefaultMonetaryPolicy()
	maximumPolicy := treasurytypes.MonetaryPolicy{
		StabilityTaxRate:            math.LegacyMustNewDecFromStr("0.1"),
		ValidatorBlockRewardTarget:  math.NewInt(10),
		OracleBlockRewardTarget:     math.NewInt(10),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
	}
	monetaryMandateUpdate := &treasurytypes.MsgSetMonetaryMandate{
		Authority:        authority,
		Committee:        monetaryCommittee.String(),
		ActivationHeight: 1,
		ExpiryHeight:     1_000_000,
		MinimumPolicy:    minimumPolicy,
		MaximumPolicy:    maximumPolicy,
	}
	ctx, proposal := executeTreasuryProposal(
		t,
		arkApp,
		ctx,
		voter,
		transfer,
		claimsMandateUpdate,
		monetaryMandateUpdate,
	)
	require.Equal(t, govv1.StatusPassed, proposal.Status)
	require.Equal(t, "/ark.reserve.v1.MsgFundBuffer", proposal.Messages[0].TypeUrl)
	require.Equal(t, "/ark.claims.v1.MsgSetClaimsMandate", proposal.Messages[1].TypeUrl)
	require.Equal(t, "/ark.treasury.v1.MsgSetMonetaryMandate", proposal.Messages[2].TypeUrl)
	require.Equal(
		t,
		math.NewInt(60),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(reservetypes.StrategicReserveName), chain.NoahBaseDenom).Amount,
	)
	require.Equal(
		t,
		math.NewInt(40),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName), chain.NoahBaseDenom).Amount,
	)
	require.Equal(
		t,
		math.NewInt(50),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(claimstypes.InsuranceName), chain.NoahBaseDenom).Amount,
	)
	storedClaimsMandate, err := arkApp.ClaimsKeeper.ClaimsMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, claimsCommittee.String(), storedClaimsMandate.Committee)
	require.Equal(t, uint64(1), storedClaimsMandate.Term)
	require.Equal(t, sdk.NewInt64Coin(chain.NoahBaseDenom, 50), storedClaimsMandate.CommitteeClaimLimit)
	storedMonetaryMandate, err := arkApp.TreasuryKeeper.MonetaryMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), storedMonetaryMandate.Term)
	require.Equal(t, monetaryCommittee.String(), storedMonetaryMandate.Committee)

	submission := &claimstypes.MsgCommitteeSubmitClaim{
		Committee:    claimsCommittee.String(),
		ExpectedTerm: storedClaimsMandate.Term,
		Reference:    "incident",
		Recipient:    sdk.AccAddress(bytes.Repeat([]byte{0x45}, 20)).String(),
		Amount:       sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
	}
	submissionResponse, err := claimskeeper.NewMsgServerImpl(arkApp.ClaimsKeeper).CommitteeSubmitClaim(ctx, submission)
	require.NoError(t, err)
	pendingClaim, err := arkApp.ClaimsKeeper.Claims.Get(ctx, submissionResponse.ClaimId)
	require.NoError(t, err)
	governancePolicy := treasurytypes.DefaultMonetaryPolicy()
	governancePolicy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	ctx, cancelProposal := executeTreasuryProposal(t, arkApp, ctx, voter,
		&treasurytypes.MsgUpdatePolicy{
			Authority: authority,
			Policy:    governancePolicy,
		},
		&claimstypes.MsgCancelClaim{
			Authority: authority,
			ClaimId:   pendingClaim.ClaimId,
		})
	require.Equal(t, govv1.StatusPassed, cancelProposal.Status)
	require.Equal(t, "/ark.treasury.v1.MsgUpdatePolicy", cancelProposal.Messages[0].TypeUrl)
	require.Equal(t, "/ark.claims.v1.MsgCancelClaim", cancelProposal.Messages[1].TypeUrl)
	storedMonetaryPolicy, err := arkApp.TreasuryKeeper.MonetaryPolicy.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, governancePolicy.InsuranceTargetRatio, storedMonetaryPolicy.InsuranceTargetRatio)
	cancelledClaim, err := arkApp.ClaimsKeeper.Claims.Get(ctx, pendingClaim.ClaimId)
	require.NoError(t, err)
	require.Equal(t, claimstypes.ClaimStatus_CLAIM_STATUS_CANCELLED, cancelledClaim.Status)
	require.Equal(t, claimstypes.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE, cancelledClaim.CancelledBy)
	insuranceReserved, err := arkApp.ClaimsKeeper.InsuranceReserved.Get(ctx)
	require.NoError(t, err)
	require.True(t, insuranceReserved.IsZero())

	govGenesis, err := govkeeper.ExportGenesis(ctx, arkApp.GovKeeper)
	require.NoError(t, err)
	var exportedProposal *govv1.Proposal
	for _, candidate := range govGenesis.Proposals {
		if candidate.Id == proposal.Id {
			exportedProposal = candidate
			break
		}
	}
	require.NotNil(t, exportedProposal)
	require.Equal(t, proposal.Messages, exportedProposal.Messages)
	treasuryGenesis, err := arkApp.TreasuryKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, treasuryGenesis.Validate())
	// The claim audit record exports from x/claims now. Both exports must be
	// independently valid: the cross-invariants tying a cancelled claim to its
	// released reservation live entirely inside the claims genesis.
	claimsGenesis, err := arkApp.ClaimsKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, claimsGenesis.Validate())
	require.Len(t, claimsGenesis.Claims, 1)

	// The successful EndBlock commitment is immediately visible to the next
	// complete-valuation redemption; Treasury reads the live Buffer balance.
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.EURBaseDenom, 10)),
	))
	require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, chain.EURBaseDenom, oracletypes.ExchangeRate{
		Denom:          chain.EURBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: ctx.BlockTime(),
	}))
	// The mint above bypasses Market and needs no priming: settlement folds the
	// registry itself, so it values the new supply as soon as its rate exists.
	// A one-unit redemption draws its whole output from a Buffer that dwarfs
	// the aggregate.
	burn, err := arkApp.TreasuryKeeper.SettleConversions(ctx, markettypes.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(1),
		RedeemedValue:     math.LegacyOneDec(),
	})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1), burn)

	// Governance executes proposal messages in one cache. The first transfer
	// would succeed alone, but the second violates its floor, so both roll back.
	reserveBefore := arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(reservetypes.StrategicReserveName),
		chain.NoahBaseDenom,
	)
	bufferBefore := arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName),
		chain.NoahBaseDenom,
	)
	ctx, failedProposal := executeTreasuryProposal(
		t,
		arkApp,
		ctx,
		voter,
		&reservetypes.MsgFundBuffer{
			Authority:             authority,
			Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 5),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 50),
		},
		&reservetypes.MsgFundBuffer{
			Authority:             authority,
			Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 50),
		},
	)
	require.Equal(t, govv1.StatusFailed, failedProposal.Status)
	require.Equal(t, reserveBefore, arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(reservetypes.StrategicReserveName),
		chain.NoahBaseDenom,
	))
	require.Equal(t, bufferBefore, arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName),
		chain.NoahBaseDenom,
	))
}

func treasuryGovernanceVoter(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context) sdk.AccAddress {
	t.Helper()
	for _, account := range arkApp.AccountKeeper.GetAllAccounts(ctx) {
		if _, isModule := account.(sdk.ModuleAccountI); !isModule {
			return account.GetAddress()
		}
	}
	require.FailNow(t, "missing governance voter account")
	return nil
}

func executeTreasuryProposal(
	t *testing.T,
	arkApp *app.ArkApp,
	ctx sdk.Context,
	voter sdk.AccAddress,
	messages ...sdk.Msg,
) (sdk.Context, govv1.Proposal) {
	t.Helper()
	proposal, err := arkApp.GovKeeper.SubmitProposal(
		ctx,
		messages,
		"",
		"Treasury proposal",
		"Treasury Phase 1 integration test",
		voter,
		false,
	)
	require.NoError(t, err)
	params, err := arkApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	govMsgServer := govkeeper.NewMsgServerImpl(arkApp.GovKeeper)
	_, err = govMsgServer.Deposit(ctx, govv1.NewMsgDeposit(voter, proposal.Id, params.MinDeposit))
	require.NoError(t, err)
	require.NoError(t, arkApp.GovKeeper.AddVote(
		ctx,
		proposal.Id,
		voter,
		govv1.NewNonSplitVoteOption(govv1.OptionYes),
		"",
	))
	proposal, err = arkApp.GovKeeper.Proposals.Get(ctx, proposal.Id)
	require.NoError(t, err)
	require.NotNil(t, proposal.VotingEndTime)
	header := ctx.BlockHeader()
	header.Height++
	header.Time = proposal.VotingEndTime.Add(time.Nanosecond)
	ctx = ctx.WithBlockHeader(header)
	require.NoError(t, gov.EndBlocker(ctx, arkApp.GovKeeper))
	proposal, err = arkApp.GovKeeper.Proposals.Get(ctx, proposal.Id)
	require.NoError(t, err)
	return ctx, proposal
}

// TestTreasuryLaunchesWithExposureModelInert pins the guarantee the whole
// risk-scaling design rests on: a chain standing up from default genesis
// carries a multiplier of one, so every fund target is exactly its policy ratio
// against liability and the model changes nothing until governance votes a
// weight positive.
//
// It is asserted end to end rather than in the types package because that is
// where it can silently break — a default that never reaches state, a genesis
// path that skips the item, a keeper that reads something else — and because
// every other treasury and app test's expectations are only valid while it
// holds.
func TestTreasuryLaunchesWithExposureModelInert(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Now(),
	})

	// The weights are committee-owned policy (D72 as amended); the machinery
	// bounding them is governance Params. Both halves have to be right for the
	// model to be inert, so both are asserted.
	policy, err := arkApp.TreasuryKeeper.MonetaryPolicy.Get(ctx)
	require.NoError(t, err)
	require.True(t, policy.LiabilityRatioWeight.IsZero(),
		"launch policy must not weight the liability ratio")
	require.True(t, policy.VolatilityWeight.IsZero(),
		"launch policy must not weight volatility")
	require.True(t, policy.FlowWeight.IsZero(),
		"launch policy must not weight redemption flow")

	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.True(t, params.MultiplierCap.GTE(math.LegacyOneDec()),
		"launch params must carry a usable multiplier cap")
	require.True(t, params.MultiplierMaxStep.IsPositive(),
		"launch params must carry a positive step limit")

	state, err := arkApp.TreasuryKeeper.ExposureState.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, math.LegacyOneDec(), state.Multiplier,
		"launch genesis must scale every fund target by exactly one")

	// The multiplier reaches consumers through one helper, so the launch state
	// has to survive a real target read rather than only a state read. A
	// requirement sized on a zero liability is zero at any multiplier, which is
	// the correct answer for a chain that has issued nothing.
	required, err := arkApp.TreasuryKeeper.RequiredReserveCapital(ctx)
	require.NoError(t, err)
	require.True(t, required.IsZero())
}
