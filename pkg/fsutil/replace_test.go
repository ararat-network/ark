package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceFile(t *testing.T) {
	tests := []struct {
		name     string
		existing *os.FileMode // nil: no file beforehand
		mode     os.FileMode
	}{
		{name: "creates a missing file", mode: 0o600},
		{name: "replaces a file and its mode", existing: modePtr(0o644), mode: 0o600},
		{name: "widens the mode when asked", existing: modePtr(0o600), mode: 0o644},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "target.toml")
			if tt.existing != nil {
				require.NoError(t, os.WriteFile(path, []byte("old"), *tt.existing))
			}

			err := ReplaceFile(path, []byte("new"), tt.mode)

			require.NoError(t, err)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "new", string(data))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, tt.mode, info.Mode().Perm())
			// The temporary file the replacement went through is gone.
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

// A failure before the rename leaves the old file untouched and no temporary
// file behind.
func TestReplaceFileLeavesTheOldFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent", "target.toml")

	err := ReplaceFile(path, []byte("new"), 0o600)

	require.ErrorContains(t, err, "creating")
	_, err = os.Stat(filepath.Dir(path))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func modePtr(mode os.FileMode) *os.FileMode {
	return &mode
}
