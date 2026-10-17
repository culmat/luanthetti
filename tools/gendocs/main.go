// Command gendocs writes docs/cli.md from the cobra command tree:
//
//	go run ./tools/gendocs > docs/cli.md
package main

import (
	"os"

	"github.com/culmat/luanthetti/internal/cli"
)

func main() {
	cli.WriteMarkdown(os.Stdout)
}
