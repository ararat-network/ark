package validator_test

import (
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// EvidenceSuite makes validator 0 double-sign by running its key on a second
// node, and reads the equivocation, the jailing, and the tombstone back.
type EvidenceSuite struct {
	*chainsuite.Suite
}

func (s *EvidenceSuite) TestDoubleSigning() {
	ctx := s.GetContext()
	from, to := 0, 3
	privkey, err := s.Chain.Validators[from].PrivValFileContent(ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.Validators[to].OverwritePrivValFile(ctx, privkey))

	s.Require().NoError(s.Chain.StopAllNodes(ctx))
	s.Require().NoError(s.Chain.Validators[to].CreateNodeContainer(ctx))
	s.Require().NoError(s.Chain.Validators[to].StartContainer(ctx))
	time.Sleep(10 * time.Second)
	s.Require().NoError(s.Chain.Validators[from].CreateNodeContainer(ctx))
	s.Require().NoError(s.Chain.Validators[from].StartContainer(ctx))
	time.Sleep(10 * time.Second)
	for i := range s.Chain.Validators {
		if i == from || i == to {
			continue
		}
		s.Require().NoError(s.Chain.Validators[i].CreateNodeContainer(ctx))
		s.Require().NoError(s.Chain.Validators[i].StartContainer(ctx))
	}
	s.Require().NoError(testutil.WaitForBlocks(ctx, 5, s.Chain))

	valcons := s.Chain.ValidatorWallets[from].ValConsAddress
	s.Require().NoError(testutil.WaitForCondition(2*time.Minute, chainsuite.BlockTime, func() (bool, error) {
		evidence, err := s.Chain.QueryJSON(ctx, "evidence", "evidence", "list")
		if err != nil {
			return false, nil //nolint:nilerr // none recorded yet
		}
		for _, e := range evidence.Array() {
			if e.Get("consensus_address").String() == valcons || e.Get("value.consensus_address").String() == valcons {
				return true, nil
			}
		}
		return false, nil
	}), "no equivocation recorded against %s", valcons)

	validator, err := s.Chain.StakingQueryValidator(ctx, s.Chain.ValidatorWallets[from].ValoperAddress)
	s.Require().NoError(err)
	s.Require().True(validator.Jailed)
	info, err := s.Chain.SlashingQuerySigningInfo(ctx, valcons)
	s.Require().NoError(err)
	s.Require().True(info.Tombstoned)
}

func TestEvidence(t *testing.T) {
	// A node restarted alone block-syncs until a peer is ahead. CometBFT v0.39
	// cannot turn block sync off, but adaptive sync starts consensus at once,
	// which the staggered restart needs.
	configToml := chainsuite.DefaultConfigToml()
	configToml["blocksync"] = testutil.Toml{"adaptive_sync": true}
	s := &EvidenceSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
			UpgradeOnSetup: true,
			ChainSpec: &interchaintest.ChainSpec{
				NumValidators: &chainsuite.FourValidators,
				ChainConfig: ibc.ChainConfig{ConfigFileOverrides: map[string]any{
					"config/config.toml": configToml,
					"config/app.toml":    chainsuite.DefaultAppToml(),
				}},
			},
		}),
	}
	suite.Run(t, s)
}
