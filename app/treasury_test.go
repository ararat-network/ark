package app

import (
	"bytes"
	"slices"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	protocolpooltypes "github.com/cosmos/cosmos-sdk/x/protocolpool/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"

	"ark/pkg/chain"
	marketkeeper "ark/x/market/keeper"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytypes "ark/x/treasury/types"
)

func TestTreasuryAccountAndLifecycleWiring(t *testing.T) {
	permissions := GetMaccPerms()
	fundAddresses := make(map[string]struct{}, len(treasurytypes.FundAccountNames()))
	for _, moduleName := range treasurytypes.FundAccountNames() {
		perms, ok := permissions[moduleName]
		require.True(t, ok, "missing Treasury fund account %s", moduleName)
		require.Empty(t, perms, "Treasury fund account %s must have no permissions", moduleName)
		address := authtypes.NewModuleAddress(moduleName).String()
		_, duplicate := fundAddresses[address]
		require.False(t, duplicate, "Treasury fund accounts must be distinct")
		fundAddresses[address] = struct{}{}
	}
	_, hasTreasuryAccount := permissions[treasurytypes.ModuleName]
	require.False(t, hasTreasuryAccount, "Treasury module identity must not be a custody account")
	collectorPermissions, hasCollector := permissions[treasurytypes.StabilityTaxCollectorName]
	require.True(t, hasCollector, "missing Oracle tax collector account")
	require.Empty(t, collectorPermissions, "Oracle tax collector must have no permissions")
	var minters []string
	for moduleName, perms := range permissions {
		if slices.Contains(perms, authtypes.Minter) {
			minters = append(minters, moduleName)
		}
	}
	require.ElementsMatch(
		t,
		[]string{markettypes.ModuleName, ibctransfertypes.ModuleName},
		minters,
		"Only Market and IBC transfer may mint",
	)
	require.ElementsMatch(
		t,
		[]string{authtypes.Minter, authtypes.Burner},
		permissions[markettypes.ModuleName],
		"Market must retain conversion mint and burn permissions",
	)
	require.ElementsMatch(
		t,
		[]string{authtypes.Minter, authtypes.Burner},
		permissions[ibctransfertypes.ModuleName],
		"IBC transfer must retain voucher mint and burn permissions",
	)

	blocked := BlockedAddresses()
	for _, moduleName := range append([]string{govtypes.ModuleName}, treasurytypes.FundAccountNames()...) {
		require.False(t, blocked[moduleName], "%s must remain reachable", moduleName)
	}
	for _, moduleName := range []string{
		authtypes.FeeCollectorName,
		distrtypes.ModuleName,
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		protocolpooltypes.ModuleName,
		protocolpooltypes.ProtocolPoolEscrowAccount,
		markettypes.ModuleName,
		ibctransfertypes.ModuleName,
		icatypes.ModuleName,
		treasurytypes.StabilityTaxCollectorName,
		oracletypes.ModuleName,
	} {
		require.True(t, blocked[moduleName], "%s must remain blocked", moduleName)
	}

	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		banktypes.ModuleName,
		treasurytypes.ModuleName,
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		treasurytypes.ModuleName,
		distrtypes.ModuleName,
	)
	require.NotContains(t, arkApp.ModuleManager.OrderEndBlockers, treasurytypes.ModuleName)
}

func TestMarketTreasurySettlementMaintainsBlockLiability(t *testing.T) {
	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Now(),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	for _, denom := range []string{chain.USDBaseDenom, chain.SDRBaseDenom} {
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
	require.True(t, arkApp.BankKeeper.GetBalance(
		ctx,
		bufferAddress,
		chain.NoahBaseDenom,
	).Amount.LT(bufferBefore))
}

func TestTreasuryFundRestrictionsRunThroughBank(t *testing.T) {
	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	goCtx := sdk.WrapSDKContext(ctx)
	bankMsgServer := bankkeeper.NewMsgServerImpl(arkApp.BankKeeper)

	sender := sdk.AccAddress(bytes.Repeat([]byte{0x41}, 20))
	unrelated := sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))
	userFunding := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.SDRBaseDenom, 100),
	)
	protocolFunding := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 20),
		sdk.NewInt64Coin(chain.SDRBaseDenom, 20),
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
	sdrSupply := arkApp.BankKeeper.GetSupply(ctx, chain.SDRBaseDenom)

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
			Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
		})
		require.Error(t, err, moduleName)
		require.Equal(t, balanceBefore, arkApp.BankKeeper.GetAllBalances(ctx, fundAddress))

		_, err = bankMsgServer.Send(goCtx, &banktypes.MsgSend{
			FromAddress: sender.String(),
			ToAddress:   fundAddress.String(),
			Amount: sdk.NewCoins(
				sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
				sdk.NewInt64Coin(chain.SDRBaseDenom, 1),
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

	senderSdrBefore := arkApp.BankKeeper.GetBalance(ctx, sender, chain.SDRBaseDenom)
	unrelatedSdrBefore := arkApp.BankKeeper.GetBalance(ctx, unrelated, chain.SDRBaseDenom)
	reserveAddress := authtypes.NewModuleAddress(treasurytypes.StrategicReserveName)
	reserveSdrBefore := arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.SDRBaseDenom)
	_, err = bankMsgServer.MultiSend(goCtx, &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: sender.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 2)),
		}},
		Outputs: []banktypes.Output{
			{Address: unrelated.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1))},
			{Address: reserveAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1))},
		},
	})
	require.Error(t, err)
	require.Equal(t, senderSdrBefore, arkApp.BankKeeper.GetBalance(ctx, sender, chain.SDRBaseDenom))
	require.Equal(t, unrelatedSdrBefore, arkApp.BankKeeper.GetBalance(ctx, unrelated, chain.SDRBaseDenom))
	require.Equal(t, reserveSdrBefore, arkApp.BankKeeper.GetBalance(ctx, reserveAddress, chain.SDRBaseDenom))

	for _, moduleName := range treasurytypes.FundAccountNames() {
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
		treasurytypes.InsuranceName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		treasurytypes.InsuranceName,
		unrelated,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	))

	require.Equal(t, noahSupply, arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom))
	require.Equal(t, sdrSupply, arkApp.BankKeeper.GetSupply(ctx, chain.SDRBaseDenom))
}

func TestTreasuryGovernanceFundAndPolicyConfiguration(t *testing.T) {
	arkApp := Setup(t, false)
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
		treasurytypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 100)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		treasurytypes.InsuranceName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)),
	))

	transfer := &treasurytypes.MsgTransferReserveToBuffer{
		Authority:             authority,
		Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 40),
		MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 60),
	}
	claimsMandateUpdate := &treasurytypes.MsgSetClaimsMandate{
		Authority:           authority,
		Committee:           claimsCommittee.String(),
		ActivationHeight:    1,
		ExpiryHeight:        1_000_000,
		CommitteeClaimLimit: math.NewInt(50),
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
	require.Equal(t, "/ark.treasury.v1.MsgTransferReserveToBuffer", proposal.Messages[0].TypeUrl)
	require.Equal(t, "/ark.treasury.v1.MsgSetClaimsMandate", proposal.Messages[1].TypeUrl)
	require.Equal(t, "/ark.treasury.v1.MsgSetMonetaryMandate", proposal.Messages[2].TypeUrl)
	require.Equal(
		t,
		math.NewInt(60),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(treasurytypes.StrategicReserveName), chain.NoahBaseDenom).Amount,
	)
	require.Equal(
		t,
		math.NewInt(40),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName), chain.NoahBaseDenom).Amount,
	)
	require.Equal(
		t,
		math.NewInt(50),
		arkApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(treasurytypes.InsuranceName), chain.NoahBaseDenom).Amount,
	)
	storedClaimsMandate, err := arkApp.TreasuryKeeper.ClaimsMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, claimsCommittee.String(), storedClaimsMandate.Committee)
	require.Equal(t, uint64(1), storedClaimsMandate.Term)
	require.Equal(t, math.NewInt(50), storedClaimsMandate.CommitteeClaimLimit)
	storedMonetaryMandate, err := arkApp.TreasuryKeeper.MonetaryMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), storedMonetaryMandate.Term)
	require.Equal(t, monetaryCommittee.String(), storedMonetaryMandate.Committee)

	submission := &treasurytypes.MsgCommitteeSubmitClaim{
		Committee:         claimsCommittee.String(),
		ExpectedTerm:      storedClaimsMandate.Term,
		IncidentReference: "incident",
		Recipient:         sdk.AccAddress(bytes.Repeat([]byte{0x45}, 20)).String(),
		Amount:            sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
		EvidenceReference: "evidence",
	}
	submissionResponse, err := treasurykeeper.NewMsgServerImpl(arkApp.TreasuryKeeper).CommitteeSubmitClaim(ctx, submission)
	require.NoError(t, err)
	pendingClaim, err := arkApp.TreasuryKeeper.Claims.Get(ctx, submissionResponse.ClaimId)
	require.NoError(t, err)
	governancePolicy := treasurytypes.DefaultMonetaryPolicy()
	governancePolicy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
	ctx, cancelProposal := executeTreasuryProposal(t, arkApp, ctx, voter,
		&treasurytypes.MsgUpdateMonetaryPolicy{
			Authority: authority,
			Policy:    governancePolicy,
		},
		&treasurytypes.MsgCancelClaim{
			Authority: authority,
			ClaimId:   pendingClaim.ClaimId,
			Reason:    "governance veto",
			Reference: "proposal",
		})
	require.Equal(t, govv1.StatusPassed, cancelProposal.Status)
	require.Equal(t, "/ark.treasury.v1.MsgUpdateMonetaryPolicy", cancelProposal.Messages[0].TypeUrl)
	require.Equal(t, "/ark.treasury.v1.MsgCancelClaim", cancelProposal.Messages[1].TypeUrl)
	storedMonetaryPolicy, err := arkApp.TreasuryKeeper.MonetaryPolicy.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, governancePolicy.InsuranceTargetRatio, storedMonetaryPolicy.InsuranceTargetRatio)
	cancelledClaim, err := arkApp.TreasuryKeeper.Claims.Get(ctx, pendingClaim.ClaimId)
	require.NoError(t, err)
	require.Equal(t, treasurytypes.ClaimStatus_CLAIM_STATUS_CANCELLED, cancelledClaim.Status)
	require.Equal(t, authority, cancelledClaim.FinalizedBy)
	insuranceReserved, err := arkApp.TreasuryKeeper.InsuranceReserved.Get(ctx)
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
	require.Len(t, treasuryGenesis.Claims, 1)

	// The successful EndBlock commitment is immediately visible to the next
	// complete-valuation redemption; Treasury reads the live Buffer balance.
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 10)),
	))
	require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, chain.SDRBaseDenom, oracletypes.ExchangeRate{
		Denom:          chain.SDRBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: ctx.BlockTime(),
	}))
	draw, err := arkApp.TreasuryKeeper.DrawRedemptionBuffer(
		ctx,
		sdk.NewInt64Coin(chain.SDRBaseDenom, 1),
		math.NewInt(1),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.SDRBaseDenom:  math.LegacyOneDec(),
		},
	)
	require.NoError(t, err)
	require.True(t, draw.ValuationComplete)
	require.Equal(t, math.NewInt(1), draw.BufferPaid)

	// Governance executes proposal messages in one cache. The first transfer
	// would succeed alone, but the second violates its floor, so both roll back.
	reserveBefore := arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.StrategicReserveName),
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
		&treasurytypes.MsgTransferReserveToBuffer{
			Authority:             authority,
			Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 5),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 50),
		},
		&treasurytypes.MsgTransferReserveToBuffer{
			Authority:             authority,
			Amount:                sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
			MinimumReserveBalance: sdk.NewInt64Coin(chain.NoahBaseDenom, 50),
		},
	)
	require.Equal(t, govv1.StatusFailed, failedProposal.Status)
	require.Equal(t, reserveBefore, arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.StrategicReserveName),
		chain.NoahBaseDenom,
	))
	require.Equal(t, bufferBefore, arkApp.BankKeeper.GetBalance(
		ctx,
		authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName),
		chain.NoahBaseDenom,
	))
}

func treasuryGovernanceVoter(t *testing.T, arkApp *ArkApp, ctx sdk.Context) sdk.AccAddress {
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
	arkApp *ArkApp,
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

func requireOrderBefore(t *testing.T, order []string, first, second string) {
	t.Helper()
	firstIndex := slices.Index(order, first)
	secondIndex := slices.Index(order, second)
	require.NotEqual(t, -1, firstIndex, "%s missing from lifecycle order", first)
	require.NotEqual(t, -1, secondIndex, "%s missing from lifecycle order", second)
	require.Less(t, firstIndex, secondIndex, "%s must run before %s", first, second)
}
