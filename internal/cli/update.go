package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/app"
)

func init() { commands = append(commands, newUpdateCmd, newPinCmd, newUnpinCmd, newRollbackCmd) }

func printUpdates(w io.Writer, rep *app.UpdateReport) {
	if len(rep.Items) == 0 {
		fmt.Fprintln(w, "Everything is up to date.")
	}
	var rows [][]string
	for _, it := range rep.Items {
		note := it.Note
		if len(it.NewDeps) > 0 {
			note = strings.TrimSpace(note + " new dependencies: " + strings.Join(it.NewDeps, ", "))
		}
		rows = append(rows, []string{it.Installation, it.Package, fmt.Sprintf("%d → %d", it.Current, it.Latest), it.Status, note})
	}
	if len(rows) > 0 {
		table(w, "INSTALLATION\tPACKAGE\tRELEASE\tSTATUS\tNOTE", rows)
	}
	for _, p := range rep.Pruned {
		fmt.Fprintln(w, "library: pruned", p)
	}
	for _, warn := range rep.Warnings {
		fmt.Fprintln(w, "note:", warn)
	}
}

func newUpdateCmd(g *globals) *cobra.Command {
	var req app.UpdateRequest
	cmd := &cobra.Command{
		Use:   "update [author/name|name]...",
		Short: "Update installed packages to their latest ContentDB release (--check only lists them)",
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Specs, req.DryRun = args, g.dryRun
			req.Confirm = func(rep *app.UpdateReport) (bool, error) {
				if g.yes {
					return true, nil
				}
				if !g.json {
					printUpdates(g.err, rep)
				}
				return g.confirm("Update, including the new dependencies?")
			}
			return g.withApp(func(a *app.App) error {
				rep, err := a.Update(cmd.Context(), req)
				if rep != nil && !errors.Is(err, app.ErrDeclined) {
					if perr := g.emit(rep, func(w io.Writer) { printUpdates(w, rep) }); perr != nil {
						return perr
					}
				}
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&req.Into, "into", nil, "installation name(s); default: all registered")
	f.StringSliceVarP(&req.Exclude, "exclude", "x", nil, "package or folder names to skip")
	f.BoolVar(&req.Check, "check", false, "only list outdated packages")
	f.BoolVar(&req.NoDeps, "no-deps", false, "don't install new dependencies")
	return cmd
}

func newPinCmd(g *globals) *cobra.Command {
	var into []string
	var release int64
	cmd := &cobra.Command{
		Use:   "pin <author/name|name>",
		Short: "Keep a package at its current release (or --release) in installations",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				done, err := a.Pin(cmd.Context(), args[0], into, release, false)
				if err != nil {
					return err
				}
				return g.emit(map[string]any{"package": args[0], "pinned_in": done}, func(w io.Writer) {
					fmt.Fprintf(w, "Pinned %s in %s.\n", args[0], strings.Join(done, ", "))
				})
			})
		},
	}
	cmd.Flags().StringSliceVar(&into, "into", nil, "installation name(s); default: all where it is installed")
	cmd.Flags().Int64Var(&release, "release", 0, "pin to this release id instead of the current one")
	return cmd
}

func newUnpinCmd(g *globals) *cobra.Command {
	var into []string
	cmd := &cobra.Command{
		Use:   "unpin <author/name|name>",
		Short: "Let update move a pinned package again",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				done, err := a.Pin(cmd.Context(), args[0], into, 0, true)
				if err != nil {
					return err
				}
				return g.emit(map[string]any{"package": args[0], "unpinned_in": done}, func(w io.Writer) {
					fmt.Fprintf(w, "Unpinned %s in %s.\n", args[0], strings.Join(done, ", "))
				})
			})
		},
	}
	cmd.Flags().StringSliceVar(&into, "into", nil, "installation name(s)")
	return cmd
}

func newRollbackCmd(g *globals) *cobra.Command {
	var into []string
	var release int64
	cmd := &cobra.Command{
		Use:   "rollback <author/name|name>",
		Short: "Switch a package back to the previous release kept in the library (and pin it)",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				items, err := a.Rollback(cmd.Context(), args[0], into, release)
				if err != nil {
					return err
				}
				return g.emit(items, func(w io.Writer) { printUpdates(w, &app.UpdateReport{Items: items}) })
			})
		},
	}
	cmd.Flags().StringSliceVar(&into, "into", nil, "installation name(s)")
	cmd.Flags().Int64Var(&release, "to", 0, "a specific release id kept in the library")
	return cmd
}
