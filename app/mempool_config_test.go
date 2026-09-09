package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	"github.com/ararat-network/ark/app/mempool"
)

func TestMempoolByteConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		options   simtestutil.AppOptionsMap
		poolBytes int64
		txBytes   int
		wantErr   string
	}{
		{"omitted", simtestutil.AppOptionsMap{}, 64 << 20, 1 << 20, ""},
		{"configured", simtestutil.AppOptionsMap{mempool.MaxPoolBytesKey: int64(128 << 20), mempool.MaxTransactionBytesKey: 2 << 20}, 128 << 20, 2 << 20, ""},
		{"environment strings", simtestutil.AppOptionsMap{mempool.MaxPoolBytesKey: "134217728", mempool.MaxTransactionBytesKey: "2097152"}, 128 << 20, 2 << 20, ""},
		{"zero pool", simtestutil.AppOptionsMap{mempool.MaxPoolBytesKey: 0}, 0, 0, "max_txs_bytes"},
		{"zero transaction", simtestutil.AppOptionsMap{mempool.MaxTransactionBytesKey: 0}, 0, 0, "max_tx_bytes"},
		{"negative pool", simtestutil.AppOptionsMap{mempool.MaxPoolBytesKey: -1}, 0, 0, "max_txs_bytes"},
		{"fractional transaction", simtestutil.AppOptionsMap{mempool.MaxTransactionBytesKey: 1.5}, 0, 0, "must be an integer"},
		{"pool overflow", simtestutil.AppOptionsMap{mempool.MaxPoolBytesKey: "9223372036854775808"}, 0, 0, "must be an integer"},
		{"malformed transaction", simtestutil.AppOptionsMap{mempool.MaxTransactionBytesKey: "bad"}, 0, 0, "must be an integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := mempoolConfig(tc.options)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.poolBytes, cfg.MaxTxsBytes)
			require.Equal(t, tc.txBytes, cfg.MaxTxBytes)
		})
	}
}
