package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportStatusString(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "absent", got: Absent.String(), want: "absent"},
		{name: "missing price", got: MissingPrice.String(), want: "missing_price"},
		{name: "with price", got: WithPrice.String(), want: "with_price"},
		{name: "unknown", got: ReportStatus(-1).String(), want: "not_implemented"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}
