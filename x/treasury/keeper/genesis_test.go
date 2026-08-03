package keeper_test

import (
	"fmt"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/mandate"
	oracletypes "ark/x/oracle/types"
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) expectGenesisFundBalances(balances map[string]sdk.Coins) {
	for _, moduleName := range types.FundAccountNames() {
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

func (s *KeeperTestSuite) TestInitGenesisBuildsUncappedSetWhenTaxIsDisabled() {
	genesis := types.DefaultGenesisState()
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().True(cap.IsZero())
	}
}

func (s *KeeperTestSuite) TestInitGenesisBuildsPositiveCapsWhenTaxIsDisabled() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.NewInt(100)
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	// One capture: sorted oracle-priced members. The reference is already a
	// member, so nothing is appended.
	s.oracleKeeper.EXPECT().GetRateSet(
		s.ctx,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), cap)
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
// the member side: a member holding no cap is the gap an activation opens
// until a rebuild lands, and live state closes it eventually rather than
// continuously — a rebuild skips while any needed rate is stale. An export
// taken inside that gap must therefore remain importable, arriving with the
// member untaxed exactly as it was on the exporting chain; the membership
// trigger re-derives the cap on the next BeginBlocker either way.
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

// TestInitGenesisAcceptsCapsBeyondMembership pins the import contract loose on
// the cap side. A cap no member claims is the expected shape of an export
// taken after a departure, and an amount that disagrees with the reference is
// what a kept cap looks like once policy has moved on; rejecting either would
// refuse the chain its own exported state.
func (s *KeeperTestSuite) TestInitGenesisAcceptsCapsBeyondMembership() {
	s.setAssets(chain.USDBaseDenom)
	genesis := types.DefaultGenesisState()
	// asdr is the reference and is not itself oracle-priced; akrw has left
	// membership altogether. Neither is a member, both carry a cap, and
	// neither amount matches the reference.
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

// TestInitGenesisImportsTaxCapRefreshFlag pins the cadence flag as imported
// state. A fresh genesis carries false, so block 1 does not rebuild what
// genesis established; an export taken while a refresh was owed carries true,
// so the debt survives the migration instead of being forgiven by it.
func (s *KeeperTestSuite) TestInitGenesisImportsTaxCapRefreshFlag() {
	for _, pending := range []bool{false, true} {
		s.Run(fmt.Sprintf("pending %t", pending), func() {
			s.setBlockHeight(9)
			s.setAssets(chain.SDRBaseDenom)
			s.expectGenesisFundBalances(nil)
			genesis := types.DefaultGenesisState()
			genesis.TaxCapRefreshPending = pending

			s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
			s.requireTaxCapRefreshPending(pending)
		})
	}
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
// cannot be the Treasury authority. The equivalent Claims rule moved with the
// mandate and is asserted in x/claims.
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
