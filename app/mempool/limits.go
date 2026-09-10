package mempool

import "fmt"

// Config keys retain the SDK's app.toml count key and CometBFT's config.toml
// byte keys. All consumers use this shared vocabulary.
const (
	MaxTxsKey              = "mempool.max-txs"
	MaxPoolBytesKey        = "mempool.max_txs_bytes"
	MaxTransactionBytesKey = "mempool.max_tx_bytes"
)

// Admission defaults bound pending storage by count and encoded bytes.
const (
	// Zero max-txs in app.toml selects this default, not an unbounded pool.
	DefaultMaxTx = 5000

	// Byte defaults match config.toml; CometBFT and Ark enforce both limits.
	DefaultMaxPoolBytes        int64 = 64 << 20
	DefaultMaxTransactionBytes       = 1 << 20

	// MaxTxLimit caps the operator-configured pending transaction count.
	MaxTxLimit = 50_000
)

// Admission shares reserve pending storage. Count and bytes are independent
// budgets; normal storage receives the remainder, including integer rounding.
// All shares use basis points (500 = 5%). These are initial local defaults.
const (
	CommitteeAdmissionCountShare  = 500
	GovernanceAdmissionCountShare = 500
	CommitteeAdmissionByteShare   = 500
	GovernanceAdmissionByteShare  = 500
)

// Block shares cap service for transactions that fit their lane allowance.
// An individual transaction larger than the entire allowance competes as normal
// for this proposal; smaller transactions above the remaining allowance wait.
const (
	CommitteeBlockGasShare   = 500
	GovernanceBlockGasShare  = 500
	CommitteeBlockByteShare  = 500
	GovernanceBlockByteShare = 500
)

// Config contains node-local admission limits, never consensus validity rules.
type Config struct {
	MaxTxs      int   `mapstructure:"max-txs"`
	MaxTxsBytes int64 `mapstructure:"max_txs_bytes"`
	MaxTxBytes  int   `mapstructure:"max_tx_bytes"`
}

func DefaultConfig() Config {
	return Config{
		MaxTxs:      DefaultMaxTx,
		MaxTxsBytes: DefaultMaxPoolBytes,
		MaxTxBytes:  DefaultMaxTransactionBytes,
	}
}

// Count is the pool size; zero max-txs selects the default. CometBFT's list
// must hold at least this many, which the start command enforces.
func (c Config) Count() int {
	if c.MaxTxs == 0 {
		return DefaultMaxTx
	}
	return c.MaxTxs
}

func (c Config) Validate() error {
	// Disabling the pool would also disable SDK proposal checks.
	if c.MaxTxs < 0 || c.MaxTxs > MaxTxLimit {
		return fmt.Errorf("[mempool] max-txs must be between 0 and %d: zero uses %d; disabling the app pool is unsupported", MaxTxLimit, DefaultMaxTx)
	}
	if c.MaxTxsBytes <= 0 {
		return fmt.Errorf("config.toml [mempool] max_txs_bytes must be positive")
	}
	if c.MaxTxBytes <= 0 {
		return fmt.Errorf("config.toml [mempool] max_tx_bytes must be positive")
	}
	return nil
}

//nolint:unparam // Count, byte and gas shares remain independently tunable constants.
func fraction(n, bps uint64) uint64 { return n/10000*bps + n%10000*bps/10000 }
