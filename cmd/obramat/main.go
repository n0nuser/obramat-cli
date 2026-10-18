// Command obramat is an unofficial, read-only terminal client for obramat.es.
// See docs/cli-spec.md.
package main

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/n0nuser/obramat-cli/internal/cli"
)

// version is stamped by goreleaser with -ldflags "-X main.version=...".
var version string

func main() {
	os.Exit(cli.Execute(resolveVersion(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// resolveVersion falls back to the module version Go records for
// `go install ...@vX.Y.Z`, then to "dev".
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}
