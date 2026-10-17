// Command luanthetti manages Luanti content in one library linked into several installations.
package main

import (
	"os"

	"github.com/culmat/luanthetti/internal/cli"
	"github.com/culmat/luanthetti/internal/version"
)

// Set by goreleaser through -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version_ = "dev"
	commit   = ""
	date     = ""
)

func main() {
	version.Set(version_, commit, date)
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
