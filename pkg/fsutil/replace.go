// Package fsutil holds the file operations both binaries share.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReplaceFile writes data to a fresh file beside path, syncs it, and renames
// it into place with mode: a reader never sees a partial file, a crash never
// leaves an empty one, and a mode the old file had is not kept.
func ReplaceFile(path string, data []byte, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("setting the mode of %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	return nil
}
