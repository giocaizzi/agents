package cli

import (
	"context"
	"fmt"
)

// runApply writes every selected harness's native files.
func (d Deps) runApply(_ context.Context, args []string) int {
	fs := d.newFlags(commandApply)
	addConfigFlags(fs)
	fs.Bool("n", false, "dry run: print the diff and write nothing")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	fmt.Fprintln(d.Stderr, "apply: not implemented yet")
	return exitFindings
}
