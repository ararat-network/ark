package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"noah/abci/oracle/metrics"
)

func TestReportStatusString(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "absent", got: metrics.Absent.String(), want: "absent"},
		{name: "missing price", got: metrics.MissingPrice.String(), want: "missing_price"},
		{name: "with price", got: metrics.WithPrice.String(), want: "with_price"},
		{name: "unknown", got: metrics.ReportStatus(-1).String(), want: "not_implemented"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}
