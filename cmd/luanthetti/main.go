// Command luanthetti manages Luanti content in one library linked into several installations.
package main

import (
	"os"

	"github.com/culmat/luanthetti/internal/cli"
	"github.com/culmat/luanthetti/internal/version"
)

// Set at release time by goreleaser:
// -ldflags "-X main.buildVersion=... -X main.buildCommit=... -X main.buildDate=...".
// Empty in a plain `go build`, which reports "dev" and the git commit.
var (
	buildVersion = ""
	buildCommit  = ""
	buildDate    = ""
)

func main() {
	version.Set(buildVersion, buildCommit, buildDate)
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
