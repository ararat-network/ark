package telemetry

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildVersionIsNeverEmpty(t *testing.T) {
	require.NotEmpty(t, BuildVersion())
}

func TestRevisionFromBuildInfo(t *testing.T) {
	tests := []struct {
		name     string
		revision string
		modified string
		want     string
	}{
		{name: "no revision recorded", want: "unknown"},
		{name: "short revision kept whole", revision: "abc123", want: "abc123"},
		{name: "long revision abbreviated", revision: "0123456789abcdef0123", want: "0123456789ab"},
		{name: "modified worktree marked", revision: "0123456789abcdef", modified: "true", want: "0123456789ab-dirty"},
		{name: "unmodified worktree unmarked", revision: "0123456789abcdef", modified: "false", want: "0123456789ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &debug.BuildInfo{}
			if tt.revision != "" {
				info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.revision", Value: tt.revision})
			}
			if tt.modified != "" {
				info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.modified", Value: tt.modified})
			}
			require.Equal(t, tt.want, revisionFromBuildInfo(info))
		})
	}
}
