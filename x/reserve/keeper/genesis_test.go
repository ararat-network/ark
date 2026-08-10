package keeper_test

import (
	"ark/pkg/chain"
	"ark/x/reserve/types"
)

// TestInitGenesisCustodyAdmission pins the rule on seeded custody. Bank writes
// genesis balances directly, so this is the one door into the account that the
// send restriction never sees, and an import is held to the restriction's own
// rule here instead: NOAH, or a registry member.
func (s *KeeperTestSuite) TestInitGenesisCustodyAdmission() {
	s.Run("admits NOAH", func() {
		s.SetupTest()
		s.fundReserve(1_000)

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()))
	})

	// Settlement routes derecognized stability tax here and a deployment's
	// acquired leg can be member paper, so an export taken after either carries
	// member custody and must reimport.
	s.Run("admits a registered member", func() {
		s.SetupTest()
		s.registeredAssets[chain.USDBaseDenom] = true
		s.fundReserveAsset(chain.USDBaseDenom, 40)

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()))
	})

	s.Run("refuses a bare denomination no asset carries", func() {
		s.SetupTest()
		s.fundReserveAsset(chain.USDBaseDenom, 40)

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()),
			"unsupported genesis denom",
		)
	})

	// An external symbol names custody the chain cannot see, so a bank balance
	// under one is refused here exactly as the send restriction refuses it: the
	// Reserve's external universe is committee-attested, never on-chain.
	s.Run("refuses an external symbol", func() {
		s.SetupTest()
		s.fundReserveAsset(testAsset, 40)

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()),
			"unsupported genesis denom",
		)
	})

	// The rule cannot refuse a chain its own export, because membership is
	// permanent: a registry row is never deleted and a lifecycle status is not
	// membership, so custody the restriction admitted stays admissible however
	// far its asset has since fallen.
	s.Run("round-trips member custody", func() {
		s.SetupTest()
		s.registeredAssets[chain.USDBaseDenom] = true
		s.fundReserve(1_000)
		s.fundReserveAsset(chain.USDBaseDenom, 40)

		exported, err := s.keeper.ExportGenesis(s.ctx)
		s.Require().NoError(err)

		s.SetupTest()
		s.registeredAssets[chain.USDBaseDenom] = true
		s.fundReserve(1_000)
		s.fundReserveAsset(chain.USDBaseDenom, 40)
		s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	})
}
