//go:build windows

package cli

import (
	"fmt"
	"os"
)

// openNoBlock opens a file read-only; Windows has no blocking FIFO to guard
// against, and the caller checks the file type.
func openNoBlock(path string) (*os.File, error) {
	f, err := os.Open(path) //nolint:gosec // a fixed profile file name; the caller checks the type
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return f, nil
}
