package mempool

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"defaults", func(*Config) {}, ""},
		{"zero count defaults", func(c *Config) { c.MaxTxs = 0 }, ""},
		{"maximum count", func(c *Config) { c.MaxTxs = MaxTxLimit }, ""},
		{"negative count", func(c *Config) { c.MaxTxs = -1 }, "max-txs"},
		{"excess count", func(c *Config) { c.MaxTxs = MaxTxLimit + 1 }, "max-txs"},
		{"zero pool bytes", func(c *Config) { c.MaxTxsBytes = 0 }, "max_txs_bytes"},
		{"negative pool bytes", func(c *Config) { c.MaxTxsBytes = -1 }, "max_txs_bytes"},
		{"minimum pool bytes", func(c *Config) { c.MaxTxsBytes = 1 }, ""},
		{"maximum pool bytes", func(c *Config) { c.MaxTxsBytes = math.MaxInt64 }, ""},
		{"zero transaction bytes", func(c *Config) { c.MaxTxBytes = 0 }, "max_tx_bytes"},
		{"negative transaction bytes", func(c *Config) { c.MaxTxBytes = -1 }, "max_tx_bytes"},
		{"minimum transaction bytes", func(c *Config) { c.MaxTxBytes = 1 }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigCount(t *testing.T) {
	for _, tc := range []struct{ maxTxs, want int }{{0, DefaultMaxTx}, {7, 7}, {MaxTxLimit, MaxTxLimit}} {
		t.Run(fmt.Sprint(tc.maxTxs), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxTxs = tc.maxTxs
			require.Equal(t, tc.want, cfg.Count())
		})
	}
}
