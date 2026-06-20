package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"noah/pkg/metrics"
)

func TestModuleMethodString(t *testing.T) {
	tests := []struct {
		name   string
		method metrics.ModuleMethod
		want   string
	}{
		{
			name:   "end block",
			method: metrics.EndBlock,
			want:   "end_blocker",
		},
		{
			name:   "unknown",
			method: metrics.ModuleMethod(-1),
			want:   "not_implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.method.String())
		})
	}
}
