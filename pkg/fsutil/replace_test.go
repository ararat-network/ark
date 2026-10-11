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

// A failed replacement leaves what was at the path untouched and no temporary
// file behind, whether it fails before or after the temporary file exists.
func TestReplaceFileLeavesTheOldFileOnFailure(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir, path string)
		check   func(t *testing.T, path string)
		wantErr string
	}{
		{
			// Rename cannot replace a directory, so the temporary file is
			// written in full and then has to be cleaned up.
			name: "rename refused",
			setup: func(t *testing.T, _, path string) {
				t.Helper()
				require.NoError(t, os.Mkdir(path, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(path, "kept"), []byte("old"), 0o600))
			},
			check: func(t *testing.T, path string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(path, "kept"))
				require.NoError(t, err)
				require.Equal(t, "old", string(data))
			},
			wantErr: "replacing ",
		},
		{
			name: "temporary file refused",
			setup: func(t *testing.T, dir, path string) {
				t.Helper()
				if os.Geteuid() == 0 {
					t.Skip("root ignores directory permissions")
				}
				require.NoError(t, os.WriteFile(path, []byte("old"), 0o400))
				require.NoError(t, os.Chmod(dir, 0o500))
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			},
			check: func(t *testing.T, path string) {
				t.Helper()
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, "old", string(data))
				info, err := os.Stat(path)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o400), info.Mode().Perm())
			},
			wantErr: "creating ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.Mkdir(dir, 0o700))
			path := filepath.Join(dir, "target.toml")
			tt.setup(t, dir, path)

			err := ReplaceFile(path, []byte("new"), 0o600)

			require.ErrorContains(t, err, tt.wantErr+path)
			tt.check(t, path)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "a temporary file was left behind: %v", entries)
			require.Equal(t, "target.toml", entries[0].Name())
		})
	}
}

func modePtr(mode os.FileMode) *os.FileMode {
	return &mode
}
