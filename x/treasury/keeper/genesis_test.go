package keeper_test

import (
	"fmt"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/mandate"
	assettypes "ark/x/asset/types"
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

// expectGenesisFundBalances stubs the balance inspection InitGenesis performs:
// the NOAH-only funds, and the collector, which is read under the weaker
// member-or-NOAH rule.
func (s *KeeperTestSuite) expectGenesisFundBalances(balances map[string]sdk.Coins) {
	accounts := append(types.FundAccountNames(), types.StabilityTaxCollectorName)
	for _, moduleName := range accounts {
		account := authtypes.NewEmptyModuleAccount(moduleName)
		s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, moduleName).Return(account)
		s.bankKeeper.EXPECT().GetAllBalances(s.ctx, account.GetAddress()).Return(balances[moduleName])
	}
}

func (s *KeeperTestSuite) TestInitAndExportGenesis() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()},
	}
	// An export taken while a cadence refresh was owed carries the raised flag,
	// and the import must keep the work owed rather than forgive it.
	genesis.TaxCapRefreshPending = true
	s.setAssets(chain.SDRBaseDenom)
	s.expectGenesisFundBalances(map[string]sdk.Coins{
		types.SubsidyPoolName:             sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 3)),
		types.RedemptionBufferName:        sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
		reservetypes.StrategicReserveName: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 7)),
	})

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, exported.Params)
	s.Require().True(genesis.MonetaryPolicy.Equal(exported.MonetaryPolicy))
	s.Require().Equal(genesis.TaxCaps, exported.TaxCaps)
	s.Require().Equal(genesis.RewardFunding, exported.RewardFunding)
	s.Require().Equal(genesis.MonetaryMandate, exported.MonetaryMandate)
	s.Require().Equal(genesis.TaxCapRefreshPending, exported.TaxCapRefreshPending)
}

// TestInitGenesisSeedsCapsAtReferenceAmount pins the launch path: a genesis
// shipping no caps seeds every member at the unconverted reference amount — a
// fresh chain holds no rates at InitChain, so there is no conversion to
// refuse, and the strict oracle mock carries that assertion. The seeds raise
// the cadence flag so the first successful rebuild re-expresses them in
// member units.
func (s *KeeperTestSuite) TestInitGenesisSeedsCapsAtReferenceAmount() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.NewInt(100)
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), cap)
	}
	s.requireTaxCapRefreshPending(true)
}

// TestInitGenesisSeedsUncappedSetWhenTaxIsUncapped keeps the zero sentinel's
// meaning through seeding: an explicitly uncapped launch copies zero into
// every member, so the whole set is taxed-uncapped from block one.
func (s *KeeperTestSuite) TestInitGenesisSeedsUncappedSetWhenTaxIsUncapped() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().True(cap.IsZero())
	}
	s.requireTaxCapRefreshPending(true)
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
			wantErr:   "reference tax cap denom asdr requires a configured protocol reference",
		},
		{
			name:      "reference names another feed",
			reference: chain.USDBaseDenom,
			wantErr:   "reference tax cap denom asdr must be the protocol reference ausd",
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

// TestInitGenesisAcceptsMembersWithoutCaps pins the import contract loose on
// the member side: a member holding no cap is the gap an arrival opens until
// the next BeginBlocker covers it, and an export taken inside that gap must
// remain importable, arriving with the member untaxed exactly as it was on
// the exporting chain; the membership trigger covers it — derived or seeded —
// on the next BeginBlocker either way.
func (s *KeeperTestSuite) TestInitGenesisAcceptsMembersWithoutCaps() {
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	genesis := types.DefaultGenesisState()
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()},
	}
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(sdrCap.IsZero())
	_, err = s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)

	// The chain's own export round-trips: the gap survives import instead of
	// refusing it.
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.TaxCaps, exported.TaxCaps)
	s.expectGenesisFundBalances(nil)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	_, err = s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
}

// TestInitGenesisAcceptsCapsBeyondOraclePricing pins the import contract loose
// on the oracle-priced side. A cap kept after its member leaves the
// oracle-priced set is the expected shape of an export taken after a
// departure, and an amount that disagrees with the reference is what a kept
// cap looks like once policy has moved on; rejecting either would refuse the
// chain its own exported state. Departure is a lifecycle status, never a
// missing registry row, so both capped denoms are still members here.
func (s *KeeperTestSuite) TestInitGenesisAcceptsCapsBeyondOraclePricing() {
	s.setAssets(chain.USDBaseDenom)
	// akrw has departed the oracle-priced set — written off, its row permanent —
	// and its kept cap travels in the export. Neither amount matches the
	// reference.
	s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.NewInt(3)
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.KRWBaseDenom, TaxCap: math.NewInt(11)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(7)},
	}
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), usdCap)
	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(11), krwCap)
	_, err = s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().Error(err)
}

// TestInitGenesisRefusesCapsForNonMembers pins where the loose import stops:
// the cap set is the tax base, so a cap naming a denomination the protocol
// never issued would have the chain collect tax that settlement can never
// price and never move — it would defer in the collector permanently. No real
// export carries such a cap, because runtime derivation walks the registry and
// rows are permanent, so refusing it costs no round trip.
func (s *KeeperTestSuite) TestInitGenesisRefusesCapsForNonMembers() {
	s.setAssets(chain.USDBaseDenom)
	genesis := types.DefaultGenesisState()
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.KRWBaseDenom, TaxCap: math.NewInt(11)},
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
			types.StabilityTaxCollectorName: sdk.NewCoins(
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
			types.StabilityTaxCollectorName: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 5)),
		})

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	})

	s.Run("refuses a non-member balance", func() {
		s.SetupTest()
		s.setAssets(chain.USDBaseDenom)
		genesis := types.DefaultGenesisState()
		s.expectGenesisFundBalances(map[string]sdk.Coins{
			types.StabilityTaxCollectorName: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 5)),
		})

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, genesis),
			"unsupported genesis denom",
		)
	})
}

// TestInitGenesisImportsTaxCapRefreshFlag pins the cadence flag as imported
// state whenever the caps are: an export taken while a refresh was owed
// carries true, so the debt survives the migration instead of being forgiven
// by it. A genesis shipping no caps forces the flag instead, because seeds
// are placeholders that owe the first successful rebuild.
func (s *KeeperTestSuite) TestInitGenesisImportsTaxCapRefreshFlag() {
	for _, pending := range []bool{false, true} {
		s.Run(fmt.Sprintf("pending %t", pending), func() {
			s.setBlockHeight(9)
			s.setAssets(chain.SDRBaseDenom)
			s.expectGenesisFundBalances(nil)
			genesis := types.DefaultGenesisState()
			genesis.TaxCaps = []types.TaxCap{
				{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()},
			}
			genesis.TaxCapRefreshPending = pending

			s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
			s.requireTaxCapRefreshPending(pending)
		})
	}

	s.Run("seeded caps force the flag", func() {
		s.setBlockHeight(9)
		s.setAssets(chain.SDRBaseDenom)
		s.expectGenesisFundBalances(nil)
		genesis := types.DefaultGenesisState()
		genesis.TaxCapRefreshPending = false

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
		s.requireTaxCapRefreshPending(true)
	})
}

func (s *KeeperTestSuite) TestInitGenesisRejectsNonNoahFundBalance() {
	genesis := types.DefaultGenesisState()
	s.setAssets(chain.SDRBaseDenom)
	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.SubsidyPoolName).
		Return(authtypes.NewEmptyModuleAccount(types.SubsidyPoolName))
	s.bankKeeper.EXPECT().GetAllBalances(s.ctx, gomock.Any()).Return(
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1)),
	)

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "unsupported genesis denom")
}

// TestInitGenesisRejectsAuthorityCommittee pins that the monetary committee
// cannot be the Treasury authority; the equivalent Claims rule is asserted in
// x/claims.
func (s *KeeperTestSuite) TestInitGenesisRejectsAuthorityCommittee() {
	minimum, maximum := monetaryPolicyBounds()
	genesis := types.DefaultGenesisState()
	genesis.MonetaryMandate = types.MonetaryMandate{
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
