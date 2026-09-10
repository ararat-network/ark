package chainsuite

import (
	"strconv"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type ChainScope int

const (
	// ChainScopeSuite creates one chain for the whole suite.
	ChainScopeSuite ChainScope = iota
	// ChainScopeTest creates a fresh chain for every test, for suites whose
	// tests change node configuration or break a validator.
	ChainScopeTest
)

// SuiteConfig is what a suite asks for beyond the defaults.
type SuiteConfig struct {
	ChainSpec *interchaintest.ChainSpec
	// UpgradeOnSetup starts the chain on TEST_OLD_IMAGE_VERSION and upgrades
	// to TEST_IMAGE_VERSION before the tests run. Without an old version
	// set it is a no-op, so the same suite runs against one image.
	UpgradeOnSetup bool
	Scope          ChainScope
	// GenesisOverrides are applied after DefaultGenesis.
	GenesisOverrides []cosmos.GenesisKV
}

const (
	Denom        = "anoah"
	Bech32Prefix = "ark"
	CoinType     = "330"
	CoinDecimals = int64(18)

	// GasPrices is empty on purpose: arkd prices every transaction itself,
	// from Treasury's live sheet and the payer's balances, tax included. A
	// --gas-prices flag would make the SDK factory build a zero-amount fee
	// coin for the `--gas auto` simulation, which the ante refuses.
	GasPrices     = ""
	GasAdjustment = 2.0

	// RelayerGasPrices is what Hermes declares per gas; it does not price
	// through arkd. Ark's fee floor is Treasury's live base gas price, not a
	// node setting: at genesis it is MinBaseGasPrice (1e11 reference base
	// units) times the NOAH cross (1.371), so this is about seven times the
	// floor. VerifyGasPrices checks it against the chain's sheet at setup, so
	// a change to either constant fails loudly rather than as fee refusals.
	RelayerGasPrices = "1000000000000" + Denom

	// BlockTime is what interchaintest sets timeout_commit to.
	BlockTime = 2 * time.Second

	GovVotingPeriod          = 40 * time.Second
	GovExpeditedVotingPeriod = 20 * time.Second
	GovDepositPeriod         = 60 * time.Second
	// GovDeposit and GovExpeditedDeposit clear their floors with room: the
	// default minimum deposit is 10 NOAH and the expedited one five times that.
	GovDeposit          = 20
	GovExpeditedDeposit = 60

	SlashingWindow       = 20
	DowntimeJailDuration = 20 * time.Second
	// ShortUnbondingTime lets a suite see an unbonding complete. It is not
	// the default because a relayer's trusting period must stay under it.
	ShortUnbondingTime = 60 * time.Second

	// UpgradeDelta is the number of blocks between an upgrade proposal and
	// its height: enough for the vote to pass under GovVotingPeriod.
	UpgradeDelta = 35

	// ValidatorFunds is each validator account's genesis balance, in NOAH.
	ValidatorFunds = 10_000
	TrustingPeriod = "336h"

	TransferPortID = "transfer"

	HermesRepository = "ghcr.io/informalsystems/hermes"
	HermesVersion    = "1.13.1"
	HermesUIDGID     = "2000:2000"

	// ValidatorMoniker is the key name interchaintest gives every validator.
	ValidatorMoniker = "validator"
	FaucetKeyName    = interchaintest.FaucetAccountKeyName
)

// These have to be vars so their address can be taken.
var (
	OneValidator   = 1
	FourValidators = 4
	NoFullNodes    = 0
)

// NOAH is n whole NOAH in base units.
func NOAH(n int64) sdkmath.Int {
	return sdkmath.NewIntWithDecimal(n, int(CoinDecimals))
}

// NOAHCoin is n whole NOAH as the CLI writes a coin.
func NOAHCoin(n int64) string {
	return NOAH(n).String() + Denom
}

// DefaultGenesisAmounts funds each validator account with ValidatorFunds and
// self-delegates a descending share. On the four-validator chains the suites
// use, the first two hold under two thirds of the power, and the rest hold
// over two thirds without the largest, so any one validator can stop without
// a halt.
func DefaultGenesisAmounts(denom string) func(i int) (sdk.Coin, sdk.Coin) {
	return func(i int) (sdk.Coin, sdk.Coin) {
		stakes := []int64{3000, 2900, 2200, 1000, 700, 400}
		if i >= len(stakes) {
			panic("chain has more validators than DefaultGenesisAmounts funds")
		}
		return sdk.Coin{Denom: denom, Amount: NOAH(ValidatorFunds)},
			sdk.Coin{Denom: denom, Amount: NOAH(stakes[i])}
	}
}

func DefaultConfigToml() testutil.Toml {
	consensus := make(testutil.Toml)
	consensus["timeout_commit"] = BlockTime.String()
	configToml := make(testutil.Toml)
	configToml["consensus"] = consensus
	return configToml
}

// DefaultAppToml restores the node's minimum-gas-prices, which interchaintest
// sets to the chain's GasPrices: the SDK refuses an empty one at start, and
// Ark reads the fee floor from Treasury rather than from this key.
func DefaultAppToml() testutil.Toml {
	appToml := make(testutil.Toml)
	appToml["minimum-gas-prices"] = "0" + Denom
	return appToml
}

// DefaultGenesis shortens governance and slashing windows to what a test can
// wait for and enables vote extensions from the first block, as
// `arkd testnet init-files` does.
func DefaultGenesis() []cosmos.GenesisKV {
	return []cosmos.GenesisKV{
		cosmos.NewGenesisKV("app_state.gov.params.voting_period", GovVotingPeriod.String()),
		cosmos.NewGenesisKV("app_state.gov.params.expedited_voting_period", GovExpeditedVotingPeriod.String()),
		cosmos.NewGenesisKV("app_state.gov.params.max_deposit_period", GovDepositPeriod.String()),
		cosmos.NewGenesisKV("app_state.slashing.params.signed_blocks_window", strconv.Itoa(SlashingWindow)),
		cosmos.NewGenesisKV("app_state.slashing.params.min_signed_per_window", "0.500000000000000000"),
		cosmos.NewGenesisKV("app_state.slashing.params.downtime_jail_duration", DowntimeJailDuration.String()),
		cosmos.NewGenesisKV("consensus.params.abci.vote_extensions_enable_height", "1"),
	}
}

// ShortUnbondingGenesis is for suites that wait for an unbonding to
// complete. Not for chains a relayer tracks.
func ShortUnbondingGenesis() []cosmos.GenesisKV {
	return []cosmos.GenesisKV{
		cosmos.NewGenesisKV("app_state.staking.params.unbonding_time", ShortUnbondingTime.String()),
	}
}

// Image is the arkd image at version.
func Image(env Environment, version string) ibc.DockerImage {
	return ibc.DockerImage{
		Repository: env.Repository(),
		Version:    version,
		// The image's nonroot user; interchaintest chowns volumes to it.
		UIDGID: "1025:1025",
	}
}

// DefaultChainSpec is one ark validator with a price-feed sidecar, booting
// on the version a suite starts from.
func DefaultChainSpec(env Environment) *interchaintest.ChainSpec {
	return ChainSpecAt(env, env.StartVersion())
}

// ChainSpecAt is DefaultChainSpec on a named image version, for a chain
// started after the suite has moved to the image under test.
func ChainSpecAt(env Environment, version string) *interchaintest.ChainSpec {
	decimals := CoinDecimals
	cfg := ibc.ChainConfig{
		Type:           "cosmos",
		Name:           "ark",
		ChainID:        "ark-e2e",
		Bin:            "arkd",
		Bech32Prefix:   Bech32Prefix,
		Denom:          Denom,
		CoinType:       CoinType,
		CoinDecimals:   &decimals,
		GasPrices:      GasPrices,
		GasAdjustment:  GasAdjustment,
		Gas:            "auto",
		TrustingPeriod: TrustingPeriod,
		Images:         []ibc.DockerImage{Image(env, version)},
		ConfigFileOverrides: map[string]any{
			"config/config.toml": DefaultConfigToml(),
			"config/app.toml":    DefaultAppToml(),
		},
		ModifyGenesis:        cosmos.ModifyGenesis(DefaultGenesis()),
		ModifyGenesisAmounts: DefaultGenesisAmounts(Denom),
	}
	if env.PriceFeed {
		cfg.SidecarConfigs = []ibc.SidecarConfig{PriceFeedSidecar(Image(env, version))}
	}
	return &interchaintest.ChainSpec{
		Name:          "ark",
		ChainName:     "ark",
		Version:       version,
		NumValidators: &OneValidator,
		NumFullNodes:  &NoFullNodes,
		ChainConfig:   cfg,
	}
}

func DefaultSuiteConfig(env Environment) SuiteConfig {
	return SuiteConfig{ChainSpec: DefaultChainSpec(env)}
}

func MergeChainSpecs(spec, other *interchaintest.ChainSpec) *interchaintest.ChainSpec {
	if spec == nil {
		return other
	}
	if other == nil {
		return spec
	}
	spec.ChainConfig = spec.MergeChainSpecConfig(other.ChainConfig)
	if other.Name != "" {
		spec.Name = other.Name
	}
	if other.ChainName != "" {
		spec.ChainName = other.ChainName
	}
	if other.Version != "" {
		spec.Version = other.Version
	}
	if other.NoHostMount != nil {
		spec.NoHostMount = other.NoHostMount
	}
	if other.NumValidators != nil {
		spec.NumValidators = other.NumValidators
	}
	if other.NumFullNodes != nil {
		spec.NumFullNodes = other.NumFullNodes
	}
	return spec
}

func (c SuiteConfig) Merge(other SuiteConfig) SuiteConfig {
	c.ChainSpec = MergeChainSpecs(c.ChainSpec, other.ChainSpec)
	c.UpgradeOnSetup = other.UpgradeOnSetup
	c.Scope = other.Scope
	if len(other.GenesisOverrides) > 0 {
		c.ChainSpec.ModifyGenesis = cosmos.ModifyGenesis(
			append(DefaultGenesis(), other.GenesisOverrides...),
		)
	}
	return c
}
