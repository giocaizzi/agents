package cli

import (
	"context"
	"fmt"
)

// runConfig dispatches the config subcommands.
func (d Deps) runConfig(_ context.Context, args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == helpFlag || args[0] == helpCmd) {
		d.printHelp(d.Stdout, commandConfig, nil)
		return exitOK
	}
	if len(args) == 0 || args[0] != commandShow {
		d.printHelp(d.Stderr, commandConfig, nil)
		return exitUsage
	}
	fs := d.newFlags(commandConfig + " " + commandShow)
	addConfigFlags(fs)
	fs.Bool("json", false, "print JSON")
	fs.Bool("origin", false, "name the layer that set each key")
	if err := fs.Parse(args[1:]); err != nil {
		return d.parseFailure(fs, err)
	}
	fmt.Fprintln(d.Stderr, "config show: not implemented yet")
	return exitFindings
}

// runSchema prints the JSON Schema of the configuration.
func (d Deps) runSchema(_ context.Context, args []string) int {
	fs := d.newFlags(commandSchema)
	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}
	fmt.Fprintln(d.Stderr, "schema: not implemented yet")
	return exitFindings
}
