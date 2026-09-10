package validator_test

import (
	"strconv"
	"testing"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// UnbondingSuite has the smallest validator withdraw its whole self-bond,
// which drops it below its minimum self-delegation and jails it.
type UnbondingSuite struct {
	*chainsuite.Suite
}

func (s *UnbondingSuite) TestUnbondValidator() {
	ctx := s.GetContext()
	const idx = 3
	_, stake := chainsuite.DefaultGenesisAmounts(chainsuite.Denom)(idx)
	wallet := s.Chain.ValidatorWallets[idx]

	txhash, err := s.Chain.Validators[idx].ExecTx(ctx, wallet.Moniker,
		"staking", "unbond", wallet.ValoperAddress, stake.String(),
	)
	s.Require().NoError(err)
	validator, err := s.Chain.StakingQueryValidator(ctx, wallet.ValoperAddress)
	s.Require().NoError(err)
	s.Require().Equal(stakingtypes.Unbonding, validator.Status)
	s.Require().True(validator.Jailed, "a validator with no self-bond stays in the set")

	tx, err := s.Chain.GetTransaction(txhash)
	s.Require().NoError(err)
	_, err = s.Chain.Validators[idx].ExecTx(ctx, wallet.Moniker,
		"staking", "cancel-unbond", wallet.ValoperAddress, stake.String(), strconv.FormatInt(tx.Height, 10),
	)
	s.Require().ErrorContains(err, "jailed")

	// The rest of the set carries on without it.
	s.Require().NoError(testutil.WaitForBlocks(ctx, 3, s.Chain))
}

func TestUnbonding(t *testing.T) {
	s := &UnbondingSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
			UpgradeOnSetup: true,
			ChainSpec: &interchaintest.ChainSpec{
				NumValidators: &chainsuite.FourValidators,
			},
		}),
	}
	suite.Run(t, s)
}
