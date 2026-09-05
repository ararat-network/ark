package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// expectGenesisFundBalances stubs the balance inspection InitGenesis performs:
// the NOAH-only funds, and the collector, which is read under the weaker
// member-or-NOAH rule.
func (s *KeeperTestSuite) expectGenesisFundBalances(balances map[string]sdk.Coins) {
	accounts := append(types.FundAccountNames(), types.TransferTaxCollectorName)
	for _, moduleName := range accounts {
		account := authtypes.NewEmptyModuleAccount(moduleName)
		s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, moduleName).Return(account)
		s.bankKeeper.EXPECT().GetAllBalances(s.ctx, account.GetAddress()).Return(balances[moduleName])
	}
}

func (s *KeeperTestSuite) TestInitAndExportGenesis() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap = math.ZeroInt()
	// The NOAH cross rides in the table and survives export on the same
	// keep-last-value terms as the member factors, exempt from the registry
	// membership rule the member entry beside it is held to.
	genesis.ConversionFactors = []types.ConversionFactor{
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyMustNewDecFromStr("0.25"), DerivedHeight: 5},
		{Denom: chain.XDRBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 3},
	}
	// An export taken while an exposure update was owed carries the raised
	// flag, and the import must keep the work owed rather than forgive it.
	genesis.ExposureRefreshPending = true
	// A non-default risk state, so the round trip proves the series survive
	// rather than being re-derived from zero: restarting the EWMAs would hand
	// the new chain a calm-market multiplier during whatever prompted the
	// export.
	genesis.ExposureState = types.ExposureState{
		LastReferencePrice: math.LegacyNewDec(3),
		VolatilityVariance: math.LegacyMustNewDecFromStr("0.25"),
		FlowPressure:       math.LegacyNewDec(11),
		LiabilityRatio:     math.LegacyMustNewDecFromStr("0.4"),
		FlowRatio:          math.LegacyMustNewDecFromStr("0.05"),
		Multiplier:         math.LegacyMustNewDecFromStr("1.75"),
		LastRefreshHeight:  7,
	}
	s.setAssets(chain.XDRBaseDenom)
	s.expectGenesisFundBalances(map[string]sdk.Coins{
		types.SubsidyPoolName:             sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 3)),
		types.RedemptionBufferName:        sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
		reservetypes.StrategicReserveName: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 7)),
	})

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, exported.Params)
	s.Require().True(genesis.EconomicPolicy.Equal(exported.EconomicPolicy))
	s.Require().Equal(genesis.ConversionFactors, exported.ConversionFactors)
	s.Require().Equal(genesis.RewardFunding, exported.RewardFunding)
	s.Require().Equal(genesis.EconomicMandate, exported.EconomicMandate)
	s.Require().Equal(genesis.ExposureState, exported.ExposureState)
	s.Require().Equal(genesis.ExposureRefreshPending, exported.ExposureRefreshPending)
}

// TestInitGenesisSeedsFactorsAtOne pins the launch path: a genesis shipping
// no factors seeds every member at one — a fresh chain holds no rates at
// InitChain, so there is no conversion to refuse, and the strict oracle mock
// carries that assertion. The derived cap is then the unconverted reference
// amount until the first block's pass re-derives from real rates, and a zero
// reference derives the uncapped sentinel through the same seeds.
func (s *KeeperTestSuite) TestInitGenesisSeedsFactorsAtOne() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap = math.NewInt(100)
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.GetTaxCap(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), cap)
	}

	// The zero sentinel keeps its meaning through the same seeds.
	genesis.Params.ReferenceTaxCap = math.ZeroInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, genesis.Params))
	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.GetTaxCap(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().True(cap.IsZero())
	}
}

// TestInitGenesisRequiresConfiguredMatchingReferenceDenom pins the launch
// invariant: the reference tax cap and the protocol reference are the same
// unit, permanently, so a genesis without a configured reference — or with a
// cap denominated in anything else — is not launchable.
func (s *KeeperTestSuite) TestInitGenesisRequiresConfiguredMatchingReferenceDenom() {
	tests := []struct {
		name      string
		reference string
		wantErr   string
	}{
		{
			name:      "reference not configured",
			reference: "",
			wantErr:   "treasury reference denom axdr requires a configured protocol reference",
		},
		{
			name:      "reference names another feed",
			reference: chain.USDBaseDenom,
			wantErr:   "treasury reference denom axdr must be the protocol reference ausd",
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.reference = test.reference

			err := s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState())
			s.Require().ErrorContains(err, test.wantErr)
		})
	}
}

// TestInitGenesisAcceptsMembersWithoutFactors pins the import contract loose
// on the member side: a member holding no factor is the gap an arrival opens
// until the next BeginBlocker covers it, and an export taken inside that gap
// must remain importable, arriving with the member untaxed exactly as it was
// on the exporting chain; the next block's pass covers it either way.
func (s *KeeperTestSuite) TestInitGenesisAcceptsMembersWithoutFactors() {
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	genesis := types.DefaultGenesisState()
	genesis.ConversionFactors = []types.ConversionFactor{
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyOneDec()},
		{Denom: chain.XDRBaseDenom, Factor: math.LegacyOneDec()},
	}
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	_, err := s.keeper.ConversionFactors.Get(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	_, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)

	// The chain's own export round-trips: the gap survives import instead of
	// refusing it.
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.ConversionFactors, exported.ConversionFactors)
	s.expectGenesisFundBalances(nil)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	_, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
}

// TestInitGenesisAcceptsFactorsBeyondOraclePricing pins the import contract
// loose on the oracle-priced side. A factor kept after its member leaves the
// oracle-priced set is the expected shape of an export taken after a
// departure; rejecting it would refuse the chain its own exported state.
// Departure is a lifecycle status, never a missing registry row, so both
// denoms are still members here.
func (s *KeeperTestSuite) TestInitGenesisAcceptsFactorsBeyondOraclePricing() {
	s.setAssets(chain.USDBaseDenom)
	// akrw has departed the oracle-priced set — written off, its row permanent —
	// and its kept factor travels in the export.
	s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap = math.NewInt(3)
	genesis.ConversionFactors = []types.ConversionFactor{
		{Denom: chain.KRWBaseDenom, Factor: math.LegacyNewDec(11)},
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyOneDec()},
		{Denom: chain.USDBaseDenom, Factor: math.LegacyNewDec(7)},
	}
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(21), usdCap)
	krwCap, err := s.keeper.GetTaxCap(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(33), krwCap)
	_, err = s.keeper.GetTaxCap(s.ctx, chain.XDRBaseDenom)
	s.Require().Error(err)
}

// TestInitGenesisRefusesFactorsForNonMembers pins where the loose import
// stops: the factor set is the tax base, so one naming a denomination the
// protocol never issued would have the chain collect tax that settlement can
// never price and never move — it would defer in the collector permanently.
// No real export carries such a factor, because runtime derivation walks the
// registry and rows are permanent, so refusing it costs no round trip.
func (s *KeeperTestSuite) TestInitGenesisRefusesFactorsForNonMembers() {
	s.setAssets(chain.USDBaseDenom)
	genesis := types.DefaultGenesisState()
	genesis.ConversionFactors = []types.ConversionFactor{
		{Denom: chain.KRWBaseDenom, Factor: math.LegacyNewDec(11)},
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyOneDec()},
	}

	s.Require().ErrorContains(
		s.keeper.InitGenesis(s.ctx, genesis),
		"not an Ark-issued asset",
	)
}

// TestInitGenesisCollectorBalanceAdmission pins the rule on seeded collector
// custody. The collector is not NOAH-only — an export taken mid-window carries
// that window's member tax — but it can only ever legitimately hold what tax
// is collected in, and tax is collected only in capped denominations, which
// are members. Bank writes genesis balances directly and every runtime door is
// closed (blocked address; the one inbound path is the ante routing), so this
// is the only place the rule can be stated.
func (s *KeeperTestSuite) TestInitGenesisCollectorBalanceAdmission() {
	s.Run("admits NOAH and member tax", func() {
		s.SetupTest()
		s.setAssets(chain.USDBaseDenom)
		genesis := types.DefaultGenesisState()
		s.expectGenesisFundBalances(map[string]sdk.Coins{
			types.TransferTaxCollectorName: sdk.NewCoins(
				sdk.NewInt64Coin(chain.NoahBaseDenom, 3),
				sdk.NewInt64Coin(chain.USDBaseDenom, 7),
			),
		})

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	})

	// A departed member's tax is still importable custody: rows are permanent,
	// so the coins a suspension or write-off stranded mid-window round-trip.
	s.Run("admits a written-off member's tax", func() {
		s.SetupTest()
		s.setAssets(chain.USDBaseDenom)
		s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
		genesis := types.DefaultGenesisState()
		s.expectGenesisFundBalances(map[string]sdk.Coins{
			types.TransferTaxCollectorName: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 5)),
		})

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	})

	s.Run("refuses a non-member balance", func() {
		s.SetupTest()
		s.setAssets(chain.USDBaseDenom)
		genesis := types.DefaultGenesisState()
		s.expectGenesisFundBalances(map[string]sdk.Coins{
			types.TransferTaxCollectorName: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 5)),
		})

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, genesis),
			"unsupported genesis denom",
		)
	})
}

func (s *KeeperTestSuite) TestInitGenesisRejectsNonNoahFundBalance() {
	genesis := types.DefaultGenesisState()
	s.setAssets(chain.XDRBaseDenom)
	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.SubsidyPoolName).
		Return(authtypes.NewEmptyModuleAccount(types.SubsidyPoolName))
	s.bankKeeper.EXPECT().GetAllBalances(s.ctx, gomock.Any()).Return(
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1)),
	)

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "unsupported genesis denom")
}

// TestInitGenesisRejectsAuthorityCommittee pins that the economic committee
// cannot be the Treasury authority; the equivalent Claims rule is asserted in
// x/claims.
func (s *KeeperTestSuite) TestInitGenesisRejectsAuthorityCommittee() {
	minimum, maximum := economicPolicyBounds()
	genesis := types.DefaultGenesisState()
	genesis.EconomicMandate = types.EconomicMandate{
		Envelope: mandate.Envelope{
			Term:             1,
			Committee:        s.authority,
			ActivationHeight: 1,
			ExpiryHeight:     100,
		},
		MinimumPolicy: minimum,
		MaximumPolicy: maximum,
	}
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "distinct from Treasury authority")
}
