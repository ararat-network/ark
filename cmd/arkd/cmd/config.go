package cmd

import (
	stdmath "math"
	"math/big"
	"strings"
	"time"

	"github.com/spf13/viper"

	cmtcfg "github.com/cometbft/cometbft/config"

	"cosmossdk.io/math"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdktelemetry "github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/app/mempool"
	"github.com/ararat-network/ark/pkg/decimal"
	"github.com/ararat-network/ark/pkg/telemetry"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
)

// arkAppConfig is app.toml. The SDK's sections squash onto the file's
// top-level keys; the tags name Ark's. initAppConfig renders it and
// readAppConfig decodes it, so there is one shape of the file.
type arkAppConfig struct {
	serverconfig.Config `mapstructure:",squash"`

	// Mempool decodes the [mempool] table a second time, beside the squashed
	// SDK copy: max-txs from app.toml, and the byte keys start copies in from
	// config.toml.
	Mempool    mempool.Config             `mapstructure:"mempool"`
	PriceFeed  pricefeedclient.Config     `mapstructure:"pricefeed"`
	Prometheus telemetry.PrometheusConfig `mapstructure:"prometheus"`
}

// appConfigTemplate is app.toml: the SDK's sections plus Ark's own.
var appConfigTemplate = strings.ReplaceAll(serverconfig.DefaultConfigTemplate,
	"# Setting max_txs to negative 1 (-1) will disable transactions from being inserted into the mempool (no-op mempool).",
	"# Negative max-txs values are rejected: Ark requires the app pool for proposal verification.",
) +
	pricefeedclient.DefaultConfigTemplate +
	telemetry.PrometheusConfigTemplate

// arkSections are the tables that are Ark's rather than the SDK's. The SDK
// writes app.toml only when it is missing and confix knows only the SDK's
// tables, so a file older than a section never gains it.
var arkSections = []string{"pricefeed", "prometheus"}

// otelSink is the legacy [telemetry] bridge's OpenTelemetry sink, the one
// installGoMetricsSink replaces.
const otelSink = sdktelemetry.MetricSinkOtel //nolint:staticcheck // the legacy bridge is what the sink replaces

var (
	maximumGasLimit   = math.LegacyNewDecFromInt(math.NewIntFromUint64(stdmath.MaxUint64))
	maximumCoinAmount = math.LegacyNewDecFromBigInt(
		new(big.Int).Sub(
			new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen),
			big.NewInt(1),
		),
	)
)

// defaultCommitTimeout is the timeout_commit config.toml and the testnet
// command write. CometBFT's 1s yields ~2s blocks; chain.BlocksPerMinute
// assumes ~6s. Node-local, so a default rather than a rule.
const defaultCommitTimeout = 5 * time.Second

// initCometBFTConfig is the config.toml arkd writes.
func initCometBFTConfig() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()
	// CometBFT's flood list mirrors the app pool, so its caps follow the pool's.
	cfg.Mempool.Size = mempool.DefaultMaxTx
	cfg.Mempool.MaxTxBytes = mempool.DefaultMaxTransactionBytes
	cfg.Mempool.MaxTxsBytes = mempool.DefaultMaxPoolBytes
	cfg.Consensus.TimeoutCommit = defaultCommitTimeout

	return cfg
}

// initAppConfig is the template and defaults the SDK writes app.toml from.
func initAppConfig() (string, any) {
	return appConfigTemplate, defaultAppConfig()
}

// defaultAppConfig is the app.toml arkd writes.
func defaultAppConfig() arkAppConfig {
	srvCfg := serverconfig.DefaultConfig()
	srvCfg.MinGasPrices = "0anoah"
	// The SDK default is -1, which disables the app-side pool and with it
	// SDK proposal verification. The pool decides admission behind CometBFT's
	// list; zero selects this bounded default too.
	srvCfg.Mempool.MaxTxs = mempool.DefaultMaxTx

	return arkAppConfig{
		Config:     *srvCfg,
		Mempool:    mempool.DefaultConfig(),
		PriceFeed:  pricefeedclient.NewDefaultConfig(),
		Prometheus: telemetry.DefaultPrometheusConfig(),
	}
}

// readAppConfig decodes and validates app.toml with full Unmarshal so nested environment overrides
// survive. SDK GetConfig handles its sections, including custom block-range parsing.
func readAppConfig(v *viper.Viper) (arkAppConfig, error) {
	// Older files omit this key. Override the SDK's disabled default while
	// preserving an explicit value (including a negative value to reject).
	v.SetDefault(mempool.MaxTxsKey, mempool.DefaultMaxTx)
	cfg := defaultAppConfig()
	if err := v.Unmarshal(&cfg); err != nil {
		return arkAppConfig{}, errortypes.ErrAppConfig.Wrapf("decode: %v", err)
	}
	srvCfg, err := serverconfig.GetConfig(v)
	if err != nil {
		return arkAppConfig{}, errortypes.ErrAppConfig.Wrapf("%v", err)
	}
	cfg.Config = srvCfg
	return cfg, cfg.Validate()
}

// Validate checks app.toml and the complete mempool config. Standalone app.toml
// validation uses default byte limits; startup supplies the resolved CometBFT values.
func (c arkAppConfig) Validate() error {
	if err := c.Mempool.Validate(); err != nil {
		return errortypes.ErrAppConfig.Wrap(err.Error())
	}
	if err := c.ValidateBasic(); err != nil {
		return err
	}
	if err := validateMinGasPrices(c.MinGasPrices); err != nil {
		return err
	}
	if err := c.PriceFeed.Validate(); err != nil {
		return errortypes.ErrAppConfig.Wrapf("[pricefeed]: %v", err)
	}
	if err := c.Prometheus.Validate(); err != nil {
		return errortypes.ErrAppConfig.Wrapf("[prometheus]: %v", err)
	}
	return nil
}

func validateMinGasPrices(value string) error {
	minGasPrices, err := sdk.ParseDecCoins(value)
	if err != nil {
		return errortypes.ErrAppConfig.Wrapf("invalid minimum gas prices: %v", err)
	}

	for _, gasPrice := range minGasPrices {
		// A transaction gas limit is a uint64. Checking the largest possible
		// value makes the ante calculation safe for every transaction.
		requiredFee, err := decimal.Mul(gasPrice.Amount, maximumGasLimit)
		if err != nil || requiredFee.GT(maximumCoinAmount) {
			return errortypes.ErrAppConfig.Wrapf(
				"minimum gas price %s is too large to multiply by the maximum gas limit",
				gasPrice,
			)
		}
	}

	return nil
}

// absentSections lists the Ark tables app.toml does not carry.
func absentSections(v *viper.Viper) []string {
	var absent []string
	for _, section := range arkSections {
		if !v.InConfig(section) {
			absent = append(absent, section)
		}
	}
	return absent
}

// unroutedGoMetrics is the pairing no section checks alone: the legacy
// bridge selects the OTel sink while the node's own endpoint is off, so its
// series reach only what otel.yaml exports.
func (c arkAppConfig) unroutedGoMetrics() bool {
	tel := c.Telemetry //nolint:staticcheck // the legacy bridge is what the sink replaces
	return tel.Enabled && tel.MetricsSink == otelSink && !c.Prometheus.Enabled
}

// deadPrometheusRetention is the other pairing no section checks alone: the
// legacy fan-out that prometheus-retention-time sizes is what
// installGoMetricsSink replaces, so the setting does nothing.
func (c arkAppConfig) deadPrometheusRetention() bool {
	tel := c.Telemetry //nolint:staticcheck // the legacy bridge is what the sink replaces
	return tel.Enabled && tel.MetricsSink == otelSink && tel.PrometheusRetentionTime > 0
}
