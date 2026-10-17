package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// WriteMarkdown renders the command reference (docs/cli.md).
func WriteMarkdown(w io.Writer) {
	g := &globals{out: io.Discard, err: io.Discard}
	root := newRoot(g)
	fmt.Fprintln(w, "# Command line")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Generated from the command definitions by `go run ./tools/gendocs > docs/cli.md`; don't edit by hand.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "    luanthetti [global flags] <command> [flags] [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Every command accepts `--json` and then prints the same report the web UI's API returns.")
	fmt.Fprintln(w, "Exit codes: 0 success, 1 error, 2 usage error (unknown command or flag, wrong arguments).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Global flags")
	fmt.Fprintln(w)
	writeFlags(w, root.PersistentFlags())
	for _, c := range root.Commands() {
		writeCommand(w, c, "luanthetti")
	}
}

func writeCommand(w io.Writer, c *cobra.Command, parent string) {
	if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
		return
	}
	full := parent + " " + c.Name()
	fmt.Fprintf(w, "\n## %s\n\n", strings.TrimPrefix(full, "luanthetti "))
	fmt.Fprintf(w, "%s.\n\n", strings.TrimSuffix(c.Short, "."))
	if c.Long != "" {
		fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(c.Long))
	}
	fmt.Fprintf(w, "    %s %s\n\n", parent, c.Use)
	if len(c.Aliases) > 0 {
		fmt.Fprintf(w, "Aliases: %s.\n\n", strings.Join(c.Aliases, ", "))
	}
	writeFlags(w, c.LocalNonPersistentFlags())
	for _, sub := range c.Commands() {
		writeCommand(w, sub, full)
	}
}

func writeFlags(w io.Writer, fs *pflag.FlagSet) {
	var rows []string
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "`--" + f.Name + "`"
		if f.Shorthand != "" {
			name = "`-" + f.Shorthand + "`, " + name
		}
		def := ""
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" && f.DefValue != "0" {
			def = " (default `" + f.DefValue + "`)"
		}
		rows = append(rows, fmt.Sprintf("| %s | %s%s |", name, strings.ReplaceAll(f.Usage, "|", "\\|"), def))
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "| flag | meaning |")
	fmt.Fprintln(w, "|---|---|")
	for _, r := range rows {
		fmt.Fprintln(w, r)
	}
	fmt.Fprintln(w)
}
