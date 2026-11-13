package cli

import (
	"context"
	"fmt"
)

// runModelsTiers prints the tier x harness model matrix and the agent assignments.
func (d Deps) runModelsTiers(_ context.Context, args []string) int {
	fs := d.newFlags(commandModels + " " + commandTiers)
	addConfigFlags(fs)
	fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	fmt.Fprintln(d.Stderr, "models tiers: not implemented yet")
	return exitFindings
}
