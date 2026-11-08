// Command agents manages AI harness configurations, parity, model intelligence, and live usage.
package main

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/giocaizzi/agents/internal/cli"
)

func main() {
	code := cli.Run(context.Background(), os.Args[1:], version())
	os.Exit(code)
}

// buildVersion is the release version, set by the release build (-ldflags -X); a
// package variable is the only way to stamp a subdirectory module's tag into the binary.
var buildVersion string //nolint:gochecknoglobals // set at link time

func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, kv := range bi.Settings {
			if kv.Key == "vcs.revision" && len(kv.Value) >= 7 {
				return "devel-" + kv.Value[:7]
			}
		}
	}
	return "devel"
}
