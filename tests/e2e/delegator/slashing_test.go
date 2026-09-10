package delegator_test

import (
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// DowntimeSuite takes the smallest validator offline past the signing
// window, sees it jailed, and unjails it through its own node.
type DowntimeSuite struct {
	*delegator.Suite
}

func (s *DowntimeSuite) TestDowntimeJailAndUnjail() {
	ctx := s.GetContext()
	const idx = 3
	val := s.Chain.Validators[idx]
	wallet := s.Chain.ValidatorWallets[idx]

	s.Require().NoError(val.StopContainer(ctx))
	s.Require().NoError(testutil.WaitForBlocks(ctx, chainsuite.SlashingWindow+5, s.Chain))

	validator, err := s.Chain.StakingQueryValidator(ctx, wallet.ValoperAddress)
	s.Require().NoError(err)
	s.Require().True(validator.Jailed, "validator was not jailed for downtime")
	info, err := s.Chain.SlashingQuerySigningInfo(ctx, wallet.ValConsAddress)
	s.Require().NoError(err)
	s.Require().True(info.JailedUntil.After(time.Now().Add(-time.Minute)))

	s.Require().NoError(val.RemoveContainer(ctx))
	s.Require().NoError(val.CreateNodeContainer(ctx))
	s.Require().NoError(val.StartContainer(ctx))
	s.Require().NoError(testutil.WaitForBlocks(ctx, 2, s.Chain))
	time.Sleep(time.Until(info.JailedUntil) + 2*chainsuite.BlockTime)

	s.Require().NoError(val.SlashingUnJail(ctx, chainsuite.ValidatorMoniker))
	s.Require().NoError(testutil.WaitForBlocks(ctx, 2, s.Chain))
	validator, err = s.Chain.StakingQueryValidator(ctx, wallet.ValoperAddress)
	s.Require().NoError(err)
	s.Require().False(validator.Jailed)
	s.Require().Equal(stakingtypes.Bonded, validator.Status)
}

func TestDowntime(t *testing.T) {
	s := &DowntimeSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
		ChainSpec: &interchaintest.ChainSpec{
			NumValidators: &chainsuite.FourValidators,
		},
	})}}
	suite.Run(t, s)
}
