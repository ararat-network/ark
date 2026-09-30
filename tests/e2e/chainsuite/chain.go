// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, tests/interchain/chainsuite/chain.go.
// Modified for Ark: chain setup and node-operation helpers.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package chainsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/errgroup"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	sdkmath "cosmossdk.io/math"

	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// Chain is an ark chain under test: interchaintest's chain plus the
// validator wallets every suite addresses.
type Chain struct {
	*cosmos.CosmosChain
	ValidatorWallets []ValidatorWallet
	RelayerWallet    ibc.Wallet
}

type ValidatorWallet struct {
	Moniker        string
	Address        string
	ValoperAddress string
	ValConsAddress string
}

func chainFromCosmosChain(cosmosChain *cosmos.CosmosChain, relayerWallet ibc.Wallet) (*Chain, error) {
	c := &Chain{CosmosChain: cosmosChain}
	wallets, err := getValidatorWallets(context.Background(), c)
	if err != nil {
		return nil, err
	}
	c.ValidatorWallets = wallets
	c.RelayerWallet = relayerWallet
	return c, nil
}

// chainConfig is spec's config for one chain: its own copies of what a
// running chain writes to, so a spec can start a second chain from the same
// point. UpgradeVersion moves the image in place, and the price-feed hook
// needs the logger and container names that exist only once a context does.
func chainConfig(ctx context.Context, spec *interchaintest.ChainSpec) ibc.ChainConfig {
	cfg := spec.ChainConfig
	cfg.Images = slices.Clone(cfg.Images)
	cfg.SidecarConfigs = slices.Clone(cfg.SidecarConfigs)
	if len(cfg.SidecarConfigs) == 0 {
		return cfg
	}
	inner := cfg.PreGenesis
	log := GetLogger(ctx)
	cfg.PreGenesis = func(c ibc.Chain) error {
		if inner != nil {
			if err := inner(c); err != nil {
				return err
			}
		}
		return WirePriceFeed(ctx, log, c.(*cosmos.CosmosChain))
	}
	return cfg
}

// NewCosmosChain builds the chain straight from the spec's config. The
// builtin factory would refuse an empty GasPrices, and empty is what lets
// arkd price its own transactions.
func NewCosmosChain(ctx context.Context, testName interchaintest.TestName, spec *interchaintest.ChainSpec) *cosmos.CosmosChain {
	validators, fullNodes := 1, 0
	if spec.NumValidators != nil {
		validators = *spec.NumValidators
	}
	if spec.NumFullNodes != nil {
		fullNodes = *spec.NumFullNodes
	}
	return cosmos.NewCosmosChain(testName.Name(), chainConfig(ctx, spec), validators, fullNodes, GetLogger(ctx))
}

// CreateChain starts one chain from spec and funds a relayer wallet on it.
func CreateChain(ctx context.Context, testName interchaintest.TestName, spec *interchaintest.ChainSpec) (*Chain, error) {
	cosmosChain := NewCosmosChain(ctx, testName, spec)
	relayerWallet, err := cosmosChain.BuildRelayerWallet(ctx, "relayer-"+cosmosChain.Config().ChainID)
	if err != nil {
		return nil, err
	}

	ic := interchaintest.NewInterchain().AddChain(cosmosChain, ibc.WalletAmount{
		Address: relayerWallet.FormattedAddress(),
		Denom:   cosmosChain.Config().Denom,
		Amount:  NOAH(ValidatorFunds),
	})
	dockerClient, dockerNetwork, err := GetDockerContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := ic.Build(ctx, GetRelayerExecReporter(ctx), interchaintest.InterchainBuildOptions{
		Client:    dockerClient,
		NetworkID: dockerNetwork,
		TestName:  testName.Name(),
	}); err != nil {
		return nil, err
	}
	return chainFromCosmosChain(cosmosChain, relayerWallet)
}

func getValidatorWallets(ctx context.Context, chain *Chain) ([]ValidatorWallet, error) {
	wallets := make([]ValidatorWallet, len(chain.Validators))
	lock := new(sync.Mutex)
	eg := new(errgroup.Group)
	for i := range chain.Validators {
		eg.Go(func() error {
			address, err := chain.Validators[i].KeyBech32(ctx, ValidatorMoniker, "acc")
			if err != nil {
				return err
			}
			valoper, err := chain.Validators[i].KeyBech32(ctx, ValidatorMoniker, "val")
			if err != nil {
				return err
			}
			valCons, _, err := chain.Validators[i].ExecBin(ctx, "comet", "show-address")
			if err != nil {
				return err
			}
			lock.Lock()
			defer lock.Unlock()
			wallets[i] = ValidatorWallet{
				Moniker:        ValidatorMoniker,
				Address:        address,
				ValoperAddress: valoper,
				ValConsAddress: strings.TrimSpace(string(valCons)),
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return wallets, nil
}

// GenerateTx builds an unsigned transaction on validator valIdx's node.
func (c *Chain) GenerateTx(ctx context.Context, valIdx int, command ...string) (string, error) {
	command = append([]string{"tx"}, command...)
	command = append(command, "--generate-only", "--keyring-backend", "test", "--chain-id", c.Config().ChainID)
	command = c.Validators[valIdx].NodeCommand(command...)
	stdout, _, err := c.Validators[valIdx].Exec(ctx, command, nil)
	if err != nil {
		return "", err
	}
	return string(stdout), nil
}

// QueryJSON runs a query on the first node and returns the value at jsonPath.
func (c *Chain) QueryJSON(ctx context.Context, jsonPath string, query ...string) (gjson.Result, error) {
	stdout, _, err := c.GetNode().ExecQuery(ctx, query...)
	if err != nil {
		return gjson.Result{}, err
	}
	result := gjson.GetBytes(stdout, jsonPath)
	if !result.Exists() {
		return gjson.Result{}, fmt.Errorf("json path %s not found in query result %s", jsonPath, stdout)
	}
	return result, nil
}

// TxEvents returns the events a committed transaction emitted.
func (c *Chain) TxEvents(ctx context.Context, txhash string) ([]abcitypes.Event, error) {
	stdout, _, err := c.GetNode().ExecQuery(ctx, "tx", txhash)
	if err != nil {
		return nil, err
	}
	result := struct {
		Events []abcitypes.Event `json:"events"`
	}{}
	if err := json.Unmarshal(stdout, &result); err != nil {
		return nil, err
	}
	return result.Events, nil
}

// GetProposalID reads the proposal a submission created.
func (c *Chain) GetProposalID(ctx context.Context, txhash string) (string, error) {
	events, err := c.TxEvents(ctx, txhash)
	if err != nil {
		return "", err
	}
	for _, event := range events {
		if event.Type != "submit_proposal" {
			continue
		}
		for _, attr := range event.Attributes {
			if attr.Key == "proposal_id" {
				return attr.Value, nil
			}
		}
	}
	return "", fmt.Errorf("proposal ID not found in tx %s", txhash)
}

// SubmitProposal submits a v1 proposal carrying messages and returns its ID.
func (c *Chain) SubmitProposal(ctx context.Context, keyName, title, deposit string, expedited bool, messages ...json.RawMessage) (string, error) {
	prop := cosmos.TxProposalv1{
		Messages:  messages,
		Metadata:  "ipfs://CID",
		Deposit:   deposit,
		Title:     title,
		Summary:   title,
		Expedited: expedited,
	}
	txhash, err := c.GetNode().SubmitProposal(ctx, keyName, prop)
	if err != nil {
		return "", err
	}
	return c.GetProposalID(ctx, txhash)
}

func (c *Chain) WaitForProposalStatus(ctx context.Context, proposalID string, status govv1.ProposalStatus) error {
	propID, err := strconv.ParseInt(proposalID, 10, 64)
	if err != nil {
		return err
	}
	height, err := c.Height(ctx)
	if err != nil {
		return err
	}
	// Two block times past the longest window a proposal can sit in.
	maxHeight := height + int64((GovDepositPeriod+GovVotingPeriod)/BlockTime) + 5
	_, err = cosmos.PollForProposalStatusV1(ctx, c.CosmosChain, height, maxHeight, uint64(propID), status)
	return err
}

// PassProposal has every validator vote yes and waits for the tally.
func (c *Chain) PassProposal(ctx context.Context, proposalID string) error {
	propID, err := strconv.ParseInt(proposalID, 10, 64)
	if err != nil {
		return err
	}
	if err := c.VoteOnProposalAllValidators(ctx, uint64(propID), cosmos.ProposalVoteYes); err != nil {
		return err
	}
	return c.WaitForProposalStatus(ctx, proposalID, govv1.StatusPassed)
}

// SubmitAndPassProposal is the whole governance act.
func (c *Chain) SubmitAndPassProposal(ctx context.Context, keyName, title string, messages ...json.RawMessage) (string, error) {
	id, err := c.SubmitProposal(ctx, keyName, title, NOAHCoin(GovDeposit), false, messages...)
	if err != nil {
		return "", err
	}
	return id, c.PassProposal(ctx, id)
}

// GovAuthority is the governance module account, the authority every
// module's governance message names.
func (c *Chain) GovAuthority(ctx context.Context) (string, error) {
	return c.GetGovernanceAddress(ctx)
}

// ReplaceImagesAndRestart is a coordinated binary swap: every node stops,
// changes image, and starts again.
func (c *Chain) ReplaceImagesAndRestart(ctx context.Context, version string) error {
	if err := c.StopAllNodes(ctx); err != nil {
		return err
	}
	c.UpgradeVersion(ctx, c.GetNode().DockerClient, c.GetNode().Image.Repository, version)
	// UpgradeVersion moves the nodes alone. The sidecars come from the same
	// image and are recreated from their own record when the nodes start.
	for _, node := range c.Nodes() {
		for _, sidecar := range node.Sidecars {
			sidecar.Image.Version = version
		}
	}
	for _, sidecar := range c.Sidecars {
		sidecar.Image.Version = version
	}
	if err := c.StartAllNodes(ctx); err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := testutil.WaitForBlocks(timeoutCtx, 5, c); err != nil {
		return fmt.Errorf("waiting for blocks after the restart: %w", err)
	}
	return nil
}

// Upgrade runs a software-upgrade proposal to its halt, then swaps the
// image and expects blocks to resume.
func (c *Chain) Upgrade(ctx context.Context, upgradeName, version string) error {
	height, err := c.Height(ctx)
	if err != nil {
		return err
	}
	haltHeight := height + UpgradeDelta
	upgradeTx, err := c.UpgradeProposal(ctx, FaucetKeyName, cosmos.SoftwareUpgradeProposal{
		Deposit:     NOAHCoin(GovDeposit),
		Title:       "Upgrade to " + upgradeName,
		Name:        upgradeName,
		Description: "Upgrade to " + upgradeName,
		Height:      haltHeight,
	})
	if err != nil {
		return err
	}
	if err := c.PassProposal(ctx, upgradeTx.ProposalID); err != nil {
		return err
	}
	if err := c.WaitForHalt(ctx, haltHeight); err != nil {
		return err
	}
	return c.ReplaceImagesAndRestart(ctx, version)
}

// UpgradeToImageUnderTest moves the chain from env's old image to the one
// under test: through governance when a plan is named, as a coordinated
// binary swap when not. Without an old image there is nothing to move from
// and the chain stays as it is.
func (c *Chain) UpgradeToImageUnderTest(ctx context.Context, env Environment) error {
	log := GetLogger(ctx).Sugar()
	if env.OldImageVersion == "" {
		log.Info("no TEST_OLD_IMAGE_VERSION; running on the image under test without an upgrade")
		return nil
	}
	if env.UpgradeName == "" {
		log.Infof("Swapping %s for %s without a plan", env.OldImageVersion, env.ImageVersion)
		return c.ReplaceImagesAndRestart(ctx, env.ImageVersion)
	}
	log.Infof("Upgrade %s from %s to %s", env.UpgradeName, env.OldImageVersion, env.ImageVersion)
	if err := c.Upgrade(ctx, env.UpgradeName, env.ImageVersion); err != nil {
		return err
	}
	applied, err := c.UpgradeQueryAppliedPlan(ctx, env.UpgradeName)
	if err != nil {
		return err
	}
	if applied.Height <= 0 {
		return fmt.Errorf("plan %s was not applied", env.UpgradeName)
	}
	return nil
}

// WaitForHalt expects the chain to stop at haltHeight: the node reports
// that height or the one before it, depending on whether the block store
// took the block the application refused, and nothing after.
func (c *Chain) WaitForHalt(ctx context.Context, haltHeight int64) error {
	height, err := c.Height(ctx)
	if err != nil {
		return err
	}
	if height >= haltHeight {
		return fmt.Errorf("height %d is already at halt height %d", height, haltHeight)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(haltHeight-height+10)*BlockTime)
	defer cancel()
	// Asking for more blocks than the halt allows means the timeout is the
	// expected outcome; any other result is a chain that did not halt or a
	// node that stopped answering.
	err = testutil.WaitForBlocks(timeoutCtx, int(haltHeight-height)+3, c)
	if err == nil {
		return fmt.Errorf("chain produced blocks past halt height %d", haltHeight)
	}
	if timeoutCtx.Err() == nil {
		return fmt.Errorf("waiting for the halt at %d: %w", haltHeight, err)
	}
	height, err = c.Height(ctx)
	if err != nil {
		return err
	}
	if height < haltHeight-1 || height > haltHeight {
		return fmt.Errorf("height %d is not halt height %d or the block before it; the chain stalled elsewhere", height, haltHeight)
	}
	return nil
}

func (c *Chain) GetValidatorHex(ctx context.Context, val int) (string, error) {
	privval, err := c.Validators[val].ReadFile(ctx, "config/priv_validator_key.json")
	if err != nil {
		return "", err
	}
	return gjson.GetBytes(privval, "address").String(), nil
}

// GetValidatorPower reads a validator's power from the consensus set.
func (c *Chain) GetValidatorPower(ctx context.Context, hexAddr string) (int64, error) {
	body, err := GetJSON(ctx, c.GetHostRPCAddress()+"/validators")
	if err != nil {
		return 0, err
	}
	power := body.Get(fmt.Sprintf("result.validators.#(address==%q).voting_power", hexAddr)).Int()
	if power == 0 {
		return 0, fmt.Errorf("validator %s not in the consensus set: %s", hexAddr, body.Raw)
	}
	return power, nil
}

// RestartNode stops a node and brings it back on the same volumes. The
// containers are removed in between: stopping a node stops its sidecars, and
// starting it creates fresh sidecar containers under the same names. Docker can
// refuse a reserved host port as already allocated while concurrent restarts
// release and reserve ports, so such a start is retried on fresh ones.
func RestartNode(ctx context.Context, node *cosmos.ChainNode) error {
	if err := node.StopContainer(ctx); err != nil {
		return err
	}
	var err error
	for range 3 {
		if err = recreateNode(ctx, node); err == nil || !strings.Contains(err.Error(), "port is already allocated") {
			return err
		}
	}
	return err
}

// recreateNode replaces a stopped node's containers. Removal is forced and
// skips missing containers, so it also clears what a failed start left.
func recreateNode(ctx context.Context, node *cosmos.ChainNode) error {
	if err := node.RemoveContainer(ctx); err != nil {
		return err
	}
	if err := node.CreateNodeContainer(ctx); err != nil {
		return err
	}
	return node.StartContainer(ctx)
}

// ModifyConfig changes config files on the given validators (all when none
// are named) and restarts them.
func (c *Chain) ModifyConfig(ctx context.Context, testName interchaintest.TestName, changes map[string]testutil.Toml, validators ...int) error {
	if len(validators) == 0 {
		validators = make([]int, len(c.Validators))
		for i := range validators {
			validators[i] = i
		}
	}
	eg := errgroup.Group{}
	for _, i := range validators {
		val := c.Validators[i]
		eg.Go(func() error {
			for file, toml := range changes {
				if err := testutil.ModifyTomlConfigFile(
					ctx, GetLogger(ctx), val.DockerClient, testName.Name(), val.VolumeName, file, toml,
				); err != nil {
					return err
				}
			}
			return RestartNode(ctx, val)
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return testutil.WaitForBlocks(ctx, 2, c)
}

// RestGet fetches a path from the first validator's REST gateway.
func (c *Chain) RestGet(ctx context.Context, path string) (gjson.Result, error) {
	return GetJSON(ctx, c.GetHostAPIAddress()+path)
}

// GasPrice is Treasury's live per-gas price in denom.
func (c *Chain) GasPrice(ctx context.Context, denom string) (sdkmath.LegacyDec, error) {
	price, err := c.QueryJSON(ctx, "gas_price.gas_price", "treasury", "gas-price", denom)
	if err != nil {
		return sdkmath.LegacyDec{}, err
	}
	return sdkmath.LegacyNewDecFromStr(price.String())
}

// VerifyGasPrices fails when the relayer's gas price no longer clears the
// chain's floor, so a drift shows up here rather than as fee refusals.
func (c *Chain) VerifyGasPrices(ctx context.Context) error {
	quoted, err := c.GasPrice(ctx, Denom)
	if err != nil {
		return fmt.Errorf("querying the gas price: %w", err)
	}
	configured, err := sdkmath.LegacyNewDecFromStr(strings.TrimSuffix(RelayerGasPrices, Denom))
	if err != nil {
		return err
	}
	if configured.LT(quoted) {
		return fmt.Errorf("relayer gas price %s is below the chain's %s%s; raise RelayerGasPrices", RelayerGasPrices, quoted, Denom)
	}
	return nil
}

// ExchangeRates is the oracle's current rate per denom.
func (c *Chain) ExchangeRates(ctx context.Context) (map[string]string, error) {
	stdout, _, err := c.GetNode().ExecQuery(ctx, "oracle", "exchange-rates")
	if err != nil {
		return nil, err
	}
	rates := map[string]string{}
	for _, rate := range gjson.GetBytes(stdout, "exchange_rates").Array() {
		rates[rate.Get("denom").String()] = rate.Get("exchange_rate").String()
	}
	return rates, nil
}

// WaitForExchangeRate polls until the oracle holds a rate for denom. The
// sidecar reaches its providers over the internet, so this can take a while
// on a cold start.
func (c *Chain) WaitForExchangeRate(ctx context.Context, denom string, timeout time.Duration) error {
	return testutil.WaitForCondition(timeout, BlockTime, func() (bool, error) {
		rates, err := c.ExchangeRates(ctx)
		if err != nil {
			return false, nil //nolint:nilerr // the query races the node's first blocks
		}
		_, ok := rates[denom]
		return ok, nil
	})
}

// VoteExtension is one validator's oracle report in a committed block.
type VoteExtension struct {
	ValidatorAddress string            `json:"validator_address"`
	ValidatorPower   int64             `json:"validator_power"`
	BlockIDFlag      string            `json:"block_id_flag"`
	Rates            map[string]string `json:"rates"`
}

// VoteExtensions reads the oracle vote extensions block height retains,
// through the operator query that inspects them. Zero is the latest.
func (c *Chain) VoteExtensions(ctx context.Context, height int64) ([]VoteExtension, error) {
	args := []string{"vote-extensions"}
	if height > 0 {
		args = append(args, strconv.FormatInt(height, 10))
	}
	stdout, stderr, err := c.GetNode().ExecQuery(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("query vote-extensions: %w: %s", err, stderr)
	}
	var out struct {
		Votes []VoteExtension `json:"votes"`
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		return nil, fmt.Errorf("decoding vote-extensions output %s: %w", stdout, err)
	}
	return out.Votes, nil
}

// WaitForPricedVoteExtensions polls until the latest block carries rates from
// every validator and returns its vote extensions. A rate lands once a
// threshold of power reports it, so an exchange rate alone does not mean every
// sidecar has reported.
func (c *Chain) WaitForPricedVoteExtensions(ctx context.Context, timeout time.Duration) ([]VoteExtension, error) {
	var votes []VoteExtension
	err := testutil.WaitForCondition(timeout, BlockTime, func() (bool, error) {
		latest, err := c.VoteExtensions(ctx, 0)
		if err != nil {
			return false, err
		}
		votes = latest
		if len(latest) != len(c.Validators) {
			return false, nil
		}
		for _, vote := range latest {
			if len(vote.Rates) == 0 {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w; last vote extensions: %+v", err, votes)
	}
	return votes, nil
}

// WaitForTx polls until txhash is committed and returns its code.
func (c *Chain) WaitForTx(ctx context.Context, txhash string) (uint32, error) {
	var code uint32
	err := testutil.WaitForCondition(30*time.Second, BlockTime, func() (bool, error) {
		stdout, _, err := c.GetNode().ExecQuery(ctx, "tx", txhash)
		if err != nil {
			return false, nil //nolint:nilerr // not yet indexed
		}
		code = uint32(gjson.GetBytes(stdout, "code").Uint())
		return true, nil
	})
	return code, err
}

// HostRPC is the first validator's RPC as reached from the test process.
func (c *Chain) HostRPC() string { return c.GetHostRPCAddress() }

// HostAPI is the first validator's REST gateway as reached from the test process.
func (c *Chain) HostAPI() string { return c.GetHostAPIAddress() }
