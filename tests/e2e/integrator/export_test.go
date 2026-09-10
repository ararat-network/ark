package integrator_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// relauncherMnemonic funds a wallet before the export so the relaunched
// chain can be asked to move it. The same words recover it on the new chain.
const relauncherMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon " +
	"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

// ExportSuite relaunches exported state under a new chain ID with the same keys and next absolute
// height, checking validators, balances, transactions, and resumed oracle reporting.
type ExportSuite struct {
	*chainsuite.Suite
}

func (s *ExportSuite) TestExportAndRelaunch() {
	ctx := s.GetContext()
	relauncher, err := s.Chain.BuildWallet(ctx, "relauncher", relauncherMnemonic)
	s.Require().NoError(err)
	funds := chainsuite.NOAH(100)
	s.Require().NoError(s.Chain.SendFunds(ctx, chainsuite.FaucetKeyName, ibc.WalletAmount{
		Address: relauncher.FormattedAddress(), Denom: chainsuite.Denom, Amount: funds,
	}))

	consAddrs := make([]string, len(s.Chain.Validators))
	for i := range s.Chain.Validators {
		hexAddr, err := s.Chain.GetValidatorHex(ctx, i)
		s.Require().NoError(err)
		consAddrs[i] = hexAddr
	}

	height, err := s.Chain.Height(ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.StopAllNodes(ctx))
	exported, err := s.Chain.ExportState(ctx, height)
	s.Require().NoError(err)
	s.Require().Equal(fmt.Sprint(height+1), gjson.Get(exported, "initial_height").String(),
		"a continuation export starts at the next height")

	// The export carries the ABCI consensus params the SDK's own export
	// drops: without the vote extension enable height a relaunch runs with
	// extensions off and the oracle never prices again.
	s.Require().Equal("1", gjson.Get(exported, "consensus.params.abci.vote_extensions_enable_height").String(),
		"arkd export dropped consensus.params.abci.vote_extensions_enable_height")
	newChainID := s.Chain.Config().ChainID + "-relaunch"
	genesis, err := sjson.Set(exported, "chain_id", newChainID)
	s.Require().NoError(err)
	// The export keeps the original genesis time. The state it carries is
	// later than that: the oracle refuses a genesis rate stamped after the
	// genesis block, so a relaunch dates its genesis at the relaunch.
	genesis, err = sjson.Set(genesis, "genesis_time", time.Now().UTC().Format(time.RFC3339Nano))
	s.Require().NoError(err)

	// The relaunch runs the image under test: a nightly upgrade has moved
	// the chain past the old one, and the export carries that state.
	spec := chainsuite.ChainSpecAt(s.Env, s.Env.ImageVersion)
	spec.NumValidators = &chainsuite.FourValidators
	spec.ChainID = newChainID
	spec.ModifyGenesis = func(ibc.ChainConfig, []byte) ([]byte, error) {
		return []byte(genesis), nil
	}
	old := s.Chain
	spec.PreGenesis = func(c ibc.Chain) error {
		for i, val := range old.Validators {
			key, err := val.PrivValFileContent(ctx)
			if err != nil {
				return fmt.Errorf("reading validator %d's key: %w", i, err)
			}
			if err := c.(*cosmos.CosmosChain).Validators[i].OverwritePrivValFile(ctx, key); err != nil {
				return fmt.Errorf("writing validator %d's key: %w", i, err)
			}
		}
		return nil
	}
	relaunched, err := chainsuite.CreateChain(ctx, s.DockerTestName(), spec)
	s.Require().NoError(err)

	// The heights are absolute: the first block of the relaunch follows the
	// exported one, and the exported one is not the relaunch's to serve.
	current, err := relaunched.Height(ctx)
	s.Require().NoError(err)
	s.Require().Greater(current, height)
	body, err := chainsuite.GetJSON(ctx, relaunched.HostRPC()+fmt.Sprintf("/block?height=%d", height+1))
	s.Require().NoError(err)
	s.Require().Equal(newChainID, body.Get("result.block.header.chain_id").String())
	_, status, err := chainsuite.Get(ctx, relaunched.HostRPC()+fmt.Sprintf("/block?height=%d", height))
	s.Require().NoError(err)
	s.Require().NotEqual(200, status, "the relaunch served a block from before its genesis")

	for _, wallet := range old.ValidatorWallets {
		validator, err := relaunched.StakingQueryValidator(ctx, wallet.ValoperAddress)
		s.Require().NoError(err)
		s.Require().Equal(stakingtypes.Bonded, validator.Status)
		s.Require().False(validator.Jailed)
	}
	set, err := chainsuite.GetJSON(ctx, relaunched.HostRPC()+"/validators")
	s.Require().NoError(err)
	s.Require().Len(set.Get("result.validators").Array(), len(old.Validators), set.Raw)
	for _, consAddr := range consAddrs {
		s.Require().True(set.Get(fmt.Sprintf(`result.validators.#(address==%q)`, consAddr)).Exists(),
			"validator %s left the set across the relaunch", consAddr)
	}

	s.Require().NoError(relaunched.RecoverKey(ctx, "relauncher", relauncherMnemonic))
	balance, err := relaunched.GetBalance(ctx, relauncher.FormattedAddress(), chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(funds.String(), balance.String())
	recipient := old.ValidatorWallets[0].Address
	before, err := relaunched.GetBalance(ctx, recipient, chainsuite.Denom)
	s.Require().NoError(err)
	_, err = relaunched.GetNode().ExecTx(ctx, "relauncher",
		"bank", "send", relauncher.FormattedAddress(), recipient, chainsuite.NOAHCoin(1),
	)
	s.Require().NoError(err)
	after, err := relaunched.GetBalance(ctx, recipient, chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(before.Add(chainsuite.NOAH(1)).String(), after.String())

	// Vote extensions stay enabled across the relaunch, and the extended
	// commit rides in every block after the first.
	enableHeight, err := relaunched.QueryJSON(ctx, "params.abci.vote_extensions_enable_height", "consensus", "params")
	s.Require().NoError(err)
	s.Require().Equal("1", enableHeight.String())
	s.Require().NoError(testutil.WaitForBlocks(ctx, 3, relaunched))
	latest, err := relaunched.Height(ctx)
	s.Require().NoError(err)
	block, err := chainsuite.GetJSON(ctx, relaunched.HostRPC()+fmt.Sprintf("/block?height=%d", latest))
	s.Require().NoError(err)
	s.Require().NotEmpty(block.Get("result.block.data.txs").Array(),
		"block %d of the relaunch carries no extended commit", latest)

	if s.Env.PriceFeed {
		// The rates come across in the export; what has to be seen is the
		// pipeline reporting again from the resumed height.
		s.Require().NoError(relaunched.WaitForExchangeRate(ctx, "ausd", 4*time.Minute))
		votes, err := relaunched.VoteExtensions(ctx, 0)
		s.Require().NoError(err)
		s.Require().Len(votes, len(old.Validators))
	}
	s.Require().NoError(testutil.WaitForBlocks(ctx, 2, relaunched))
}

func TestExport(t *testing.T) {
	s := &ExportSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
			UpgradeOnSetup: true,
			ChainSpec: &interchaintest.ChainSpec{
				NumValidators: &chainsuite.FourValidators,
			},
		}),
	}
	suite.Run(t, s)
}
