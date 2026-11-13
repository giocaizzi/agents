package apply

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// writeAtomic writes data to a temp file in the same directory, syncs it and
// renames it into place, then syncs the directory so the rename survives a
// crash. check, when set, runs just before the rename: a non-nil error aborts
// the write and leaves the file as it is.
func writeAtomic(path string, data []byte, mode fs.FileMode, check func() error) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err = tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if check != nil {
		if err = check(); err != nil {
			return fmt.Errorf("replace %s: %w", path, err)
		}
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return syncDir(dir)
}

// syncDir flushes a directory entry to disk. Windows cannot open a directory
// for it, so it does nothing there.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // the directory of a file agents just wrote
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	syncErr := d.Sync()
	if closeErr := d.Close(); syncErr == nil {
		syncErr = closeErr
	}
	if syncErr != nil {
		return fmt.Errorf("sync %s: %w", dir, syncErr)
	}
	return nil
}
