package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/app"
)

func init() { commands = append(commands, newInstallCmd, newRemoveCmd, newListCmd) }

func printInstall(w io.Writer, rep *app.InstallReport) {
	for _, r := range rep.Results {
		fmt.Fprintf(w, "%s:\n", r.Installation)
		if len(r.Games) > 0 {
			var parts []string
			for _, gs := range r.Games {
				s := gs.Game + " " + gs.Status
				if len(gs.Missing) > 0 {
					s += " (missing " + strings.Join(gs.Missing, ", ") + ")"
				}
				if len(gs.Risky) > 0 {
					s += " (undeclared: " + strings.Join(gs.Risky, ", ") + ")"
				}
				parts = append(parts, s)
			}
			fmt.Fprintf(w, "  games: %s\n", strings.Join(parts, "; "))
		}
		if r.Skipped != "" {
			fmt.Fprintf(w, "  skipped: %s\n", r.Skipped)
			continue
		}
		for _, it := range r.Items {
			line := fmt.Sprintf("  %-9s %s", it.Status, it.Package)
			if it.Release > 0 {
				line += fmt.Sprintf(" (release %d)", it.Release)
			}
			if it.Reason == "dependency" {
				line += " for " + strings.Join(it.RequiredBy, ", ")
			}
			if it.LinkKind != "" && it.Status == "installed" {
				line += " → " + it.Path + " [" + it.LinkKind + "]"
			}
			if it.Note != "" {
				line += ": " + it.Note
			}
			fmt.Fprintln(w, line)
		}
		for _, warn := range r.Warnings {
			fmt.Fprintln(w, "  note:", warn)
		}
	}
}

func newInstallCmd(g *globals) *cobra.Command {
	var req app.InstallRequest
	cmd := &cobra.Command{
		Use:   "install <author/name|name|path>...",
		Short: "Install packages into every registered installation where they work (or --into)",
		Long: `Install downloads each package once into the library and links it into the selected
installations (all by default). Each package is resolved against every game of each
installation; hard dependencies are installed too, after one confirmation.`,
		Args: wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Specs, req.DryRun = args, g.dryRun
			req.Confirm = func(rep *app.InstallReport) (bool, error) {
				if g.yes {
					return true, nil
				}
				if !g.json {
					printInstall(g.err, rep)
				}
				return g.confirm("Install these packages, including the new dependencies?")
			}
			return g.withApp(func(a *app.App) error {
				rep, err := a.Install(cmd.Context(), req)
				if rep != nil && (err == nil || !errors.Is(err, app.ErrDeclined)) {
					if perr := g.emit(rep, func(w io.Writer) { printInstall(w, rep) }); perr != nil {
						return perr
					}
				}
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&req.Into, "into", nil, "installation name(s); default: all registered")
	f.BoolVar(&req.NoDeps, "no-deps", false, "don't install dependencies")
	f.BoolVar(&req.Force, "force", false, "install even where no game can run it, or over an unmanaged folder (moved to the trash)")
	f.Int64Var(&req.Release, "release", 0, "a specific ContentDB release id")
	return cmd
}

func newRemoveCmd(g *globals) *cobra.Command {
	var req app.RemoveRequest
	cmd := &cobra.Command{
		Use:     "remove <author/name|name>...",
		Aliases: []string{"uninstall", "rm"},
		Short:   "Remove packages from installations (and the library when nothing links them)",
		Args:    wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Specs, req.DryRun = args, g.dryRun
			return g.withApp(func(a *app.App) error {
				rep, err := a.Remove(cmd.Context(), req)
				if rep != nil {
					if perr := g.emit(rep, func(w io.Writer) {
						for _, r := range rep.Removed {
							fmt.Fprintf(w, "%s: removed %s (%s)\n", r.Installation, r.Package, r.Reason)
						}
						for _, p := range rep.Library {
							fmt.Fprintf(w, "library: deleted %s\n", p)
						}
						for _, warn := range rep.Warnings {
							fmt.Fprintln(w, "note:", warn)
						}
					}); perr != nil {
						return perr
					}
				}
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&req.Into, "into", nil, "installation name(s); default: all registered")
	f.BoolVar(&req.KeepLibrary, "keep-library", false, "keep the downloaded releases in the library")
	f.BoolVar(&req.Force, "force", false, "remove even if other installed packages need it")
	return cmd
}

func newListCmd(g *globals) *cobra.Command {
	var req app.ListRequest
	var names bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed packages per installation (or the library)",
		Args:    wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.List(cmd.Context(), req)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					if names {
						for _, r := range rep.Rows {
							fmt.Fprintln(w, r.Package)
						}
						return
					}
					if !req.Library {
						if len(rep.Rows) == 0 {
							fmt.Fprintln(w, "Nothing installed through luanthetti yet.")
							return
						}
						var rows [][]string
						for _, r := range rep.Rows {
							rel := fmt.Sprint(r.Release)
							if r.Outdated {
								rel += fmt.Sprintf(" → %d", r.Latest)
							}
							flags := r.Reason
							if r.Pinned {
								flags += ",pinned"
							}
							rows = append(rows, []string{r.Installation, r.Package, r.Folder, rel, r.Kind, flags, r.State})
						}
						table(w, "INSTALLATION\tPACKAGE\tFOLDER\tRELEASE\tLINK\tREASON\tSTATE", rows)
						return
					}
					var rows [][]string
					for _, l := range rep.Library {
						for _, v := range l.Versions {
							rows = append(rows, []string{l.Package, l.Type, fmt.Sprint(v.Release), dash(v.Title), fmt.Sprint(v.Links), v.Origin})
						}
					}
					table(w, "PACKAGE\tTYPE\tRELEASE\tTITLE\tLINKS\tORIGIN", rows)
				})
			})
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&req.Into, "into", nil, "installation name(s)")
	f.StringVar(&req.Type, "type", "", "mod | game | txp")
	f.BoolVar(&req.Outdated, "outdated", false, "only packages with a newer release on ContentDB")
	f.BoolVar(&req.Library, "library", false, "list the library's releases instead")
	f.BoolVar(&names, "names", false, "print only package names")
	return cmd
}
