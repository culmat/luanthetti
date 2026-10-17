// Package cli defines the luanthetti command line. Commands are thin: they parse flags,
// call internal/app and render the returned report as text or JSON.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/version"
)

// globals are the persistent flags every command sees.
type globals struct {
	json     bool
	home     string
	cdbURL   string
	verbose  bool
	yes      bool
	dryRun   bool
	offline  bool
	out, err io.Writer
	in       io.Reader
}

// Main runs the command line and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	g := &globals{out: stdout, err: stderr, in: os.Stdin}
	root := newRoot(g)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(stderr, "luanthetti: %s\nRun 'luanthetti --help' for usage.\n", usage.Msg)
		return ExitUsage
	}
	fmt.Fprintf(stderr, "luanthetti: %v\n", err)
	return ExitError
}

func newRoot(g *globals) *cobra.Command {
	root := &cobra.Command{
		Use:   "luanthetti",
		Short: "One library of Luanti mods, games and texture packs, linked into every installation",
		Long: `Luanthetti keeps Luanti content in one library and links it into every Luanti
installation you register. luanti-depgraph tells it in which games a package works.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Unknown flags and bad argument counts are usage errors (exit 2), like luma.
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &UsageError{Msg: err.Error()} })
	pf := root.PersistentFlags()
	pf.BoolVar(&g.json, "json", false, "print results as JSON")
	pf.StringVar(&g.home, "home", "", "data directory (default: per-OS location; env LUANTHETTI_HOME)")
	pf.StringVar(&g.cdbURL, "contentdb-url", "", "ContentDB base URL (default https://content.luanti.org)")
	pf.BoolVarP(&g.verbose, "verbose", "v", false, "more output on stderr")
	pf.BoolVarP(&g.yes, "yes", "y", false, "answer yes to confirmations")
	pf.BoolVarP(&g.dryRun, "dry-run", "n", false, "show what would be done without changing anything")
	pf.BoolVar(&g.offline, "offline", os.Getenv("LUANTHETTI_OFFLINE") == "1", "use only cached data; no network")

	root.AddCommand(newVersionCmd(g))
	for _, add := range commands {
		root.AddCommand(add(g))
	}
	return root
}

// commands registers the subcommands defined in the other files of this package.
var commands []func(*globals) *cobra.Command

// wrapArgs turns cobra's argument-count errors into usage errors.
func wrapArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return &UsageError{Msg: err.Error()}
		}
		return nil
	}
}

// emit prints v as indented JSON when --json is set, otherwise calls text.
func (g *globals) emit(v any, text func(w io.Writer)) error {
	if g.json {
		enc := json.NewEncoder(g.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text(g.out)
	return nil
}

func newVersionCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the luanthetti version",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			info := struct {
				Version string `json:"version"`
				Commit  string `json:"commit,omitempty"`
				Date    string `json:"date,omitempty"`
			}{version.Version(), version.Commit(), version.Date()}
			return g.emit(info, func(w io.Writer) {
				fmt.Fprintf(w, "luanthetti %s", info.Version)
				if info.Commit != "" {
					fmt.Fprintf(w, " (%s", info.Commit)
					if info.Date != "" {
						fmt.Fprintf(w, ", %s", info.Date)
					}
					fmt.Fprint(w, ")")
				}
				fmt.Fprintln(w)
			})
		},
	}
}
