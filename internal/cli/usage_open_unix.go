//go:build !windows

package cli

import (
	"fmt"
	"os"
	"syscall"
)

// openNoBlock opens a file read-only without waiting for a writer, so a FIFO
// swapped in cannot block; the caller checks the file type.
func openNoBlock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // a fixed profile file name; the caller checks the type
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return f, nil
}
