package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/app"
)

func init() {
	commands = append(commands, newScanCmd, newAdoptCmd, newVerifyCmd, newRepairCmd, newRefreshCmd, newGCCmd, newEnvCmd, newDoctorCmd)
}

func newScanCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "scan [installation]",
		Short: "Show what is in an installation: managed, adoptable, unmanaged, replaced, broken",
		Args:  wrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return g.withApp(func(a *app.App) error {
				names := []string{name}
				if name == "" {
					insts, err := a.Store.Installations()
					if err != nil {
						return err
					}
					names = nil
					for _, i := range insts {
						names = append(names, i.Name)
					}
					if len(names) == 0 {
						return fmt.Errorf("no installations registered")
					}
				}
				var reps []*app.ScanReport
				for _, n := range names {
					rep, err := a.Scan(cmd.Context(), n)
					if err != nil {
						return err
					}
					reps = append(reps, rep)
				}
				return g.emit(reps, func(w io.Writer) {
					for _, rep := range reps {
						fmt.Fprintf(w, "%s:\n", rep.Installation)
						var rows [][]string
						for _, r := range rep.Results {
							pkg := ""
							if r.Author != "" {
								pkg = r.Author + "/" + r.Name
							}
							rel := ""
							if r.ReleaseID > 0 {
								rel = fmt.Sprint(r.ReleaseID)
							}
							rows = append(rows, []string{"  " + r.Status, r.Path, dash(pkg), dash(rel), r.Detail})
						}
						table(w, "", rows)
						var counts []string
						for k, v := range rep.Counts {
							counts = append(counts, fmt.Sprintf("%d %s", v, k))
						}
						sort.Strings(counts)
						fmt.Fprintf(w, "  %s\n", strings.Join(counts, ", "))
						if rep.Counts["adoptable"] > 0 {
							fmt.Fprintf(w, "  Bring adoptable folders into the library with `luanthetti adopt %s`.\n", rep.Installation)
						}
					}
				})
			})
		},
	}
}

func newAdoptCmd(g *globals) *cobra.Command {
	var req app.AdoptRequest
	cmd := &cobra.Command{
		Use:   "adopt [installation] [folder|author/name]...",
		Short: "Move installed ContentDB packages into the library and link them back",
		Long: `Adopt takes folders that carry author and release in their conf (installed by the in-game
client or luma), moves them into the library and puts a link in their place. Folders without
that identity, and git checkouts, are left alone.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				req.Installation, req.Specs = args[0], args[1:]
			}
			req.DryRun = g.dryRun
			return g.withApp(func(a *app.App) error {
				if req.Installation != "" {
					if _, err := a.Store.Installation(req.Installation); err != nil {
						// The first argument is a package, not an installation.
						req.Specs, req.Installation = args, ""
					}
				}
				rep, err := a.Adopt(cmd.Context(), req)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					for _, it := range rep.Adopted {
						fmt.Fprintf(w, "%s: %s %s (release %d) %s\n", it.Installation, it.Action, it.Package, it.Release, it.Path)
					}
					for _, it := range rep.Skipped {
						fmt.Fprintf(w, "%s: skipped %s: %s\n", it.Installation, it.Path, it.Note)
					}
					if len(rep.Adopted) == 0 {
						fmt.Fprintln(w, "Nothing to adopt.")
					}
				})
			})
		},
	}
	cmd.Flags().BoolVar(&req.Copy, "copy", false, "leave the folder in place and register it as a copy")
	cmd.Flags().BoolVar(&req.Force, "force", false, "also adopt folders that replaced links, and git checkouts with a release")
	return cmd
}

func printVerify(w io.Writer, rep *app.VerifyReport) {
	for _, p := range rep.Repaired {
		fmt.Fprintf(w, "repaired %s: %s %s\n", p.Installation, p.Kind, p.Path)
	}
	for _, p := range rep.Problems {
		line := fmt.Sprintf("%s: %s %s: %s", p.Installation, p.Kind, p.Path, p.Detail)
		if p.Fix != "" {
			line += " (fix: " + p.Fix + ")"
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "%s ok, %s.\n", plural(rep.OK, "link"), plural(len(rep.Problems), "problem"))
}

func newVerifyCmd(g *globals) *cobra.Command {
	var into []string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check links, library versions, replaced folders and missing dependencies",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.Verify(cmd.Context(), into)
				if err != nil {
					return err
				}
				if err := g.emit(rep, func(w io.Writer) { printVerify(w, rep) }); err != nil {
					return err
				}
				if len(rep.Problems) > 0 {
					return fmt.Errorf("%s found", plural(len(rep.Problems), "problem"))
				}
				return nil
			})
		},
	}
	cmd.Flags().StringSliceVar(&into, "into", nil, "installation name(s)")
	return cmd
}

func newRepairCmd(g *globals) *cobra.Command {
	var req app.RepairRequest
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Fix what verify finds: --relink broken links, --readopt replaced folders",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !req.Relink && !req.Readopt {
				req.Relink, req.Readopt = true, true
			}
			req.DryRun = g.dryRun
			return g.withApp(func(a *app.App) error {
				rep, err := a.Repair(cmd.Context(), req)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) { printVerify(w, rep) })
			})
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&req.Into, "into", nil, "installation name(s)")
	f.BoolVar(&req.Relink, "relink", false, "recreate missing, broken or wrong links; finish interrupted updates")
	f.BoolVar(&req.Readopt, "readopt", false, "bring folders that replaced links back into the library")
	return cmd
}

func newRefreshCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Download a newer dependency snapshot and refresh the update lists",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.Refresh(cmd.Context())
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					state := "already current"
					if rep.Snapshot.Downloaded {
						state = "downloaded"
					}
					fmt.Fprintf(w, "dependency snapshot: built %s by luanti-depgraph %s (%s), %d packages\n",
						rep.Meta.BuiltAt.Format("2006-01-02 15:04"), rep.Meta.LdgVersion, state, rep.Meta.Packages)
					for inst, n := range rep.Updates {
						fmt.Fprintf(w, "%s: update list with %d packages\n", inst, n)
					}
					for _, n := range rep.Notes {
						fmt.Fprintln(w, "note:", n)
					}
				})
			})
		},
	}
}

func newGCCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Delete unreferenced old releases, interrupted downloads and old trash",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.GC(cmd.Context(), g.dryRun)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					for _, v := range rep.Versions {
						fmt.Fprintln(w, "pruned", v)
					}
					for _, p := range rep.Partial {
						fmt.Fprintln(w, "removed interrupted download", p)
					}
					for _, p := range rep.Trash {
						fmt.Fprintln(w, "emptied trash", p)
					}
					if len(rep.Versions)+len(rep.Partial)+len(rep.Trash) == 0 {
						fmt.Fprintln(w, "Nothing to clean up.")
					}
				})
			})
		},
	}
}

func newEnvCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "env [installation]",
		Short: "Print LUANTI_MOD_PATH and world.mt lines to run a server from the library without links",
		Args:  wrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return g.withApp(func(a *app.App) error {
				rep, err := a.Env(cmd.Context(), name)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					fmt.Fprintf(w, "export %s\n\n# world.mt\n%s\n", rep.Env, strings.Join(rep.WorldLines, "\n"))
				})
			})
		},
	}
}

func newDoctorCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check paths, the dependency snapshot, installations and ContentDB",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep := a.Doctor(cmd.Context())
				return g.emit(rep, func(w io.Writer) {
					fmt.Fprintf(w, "luanthetti %s\n", rep.Version)
					fmt.Fprintf(w, "data      %s\nconfig    %s\ncache     %s\nlibrary   %s\n", rep.Data, rep.Config, rep.Cache, rep.Library)
					if rep.Snapshot != nil {
						fmt.Fprintf(w, "snapshot  built %s (%s ago) by luanti-depgraph %s; schema %d, rules %d (this build knows %d); %d packages; precomputed answers: %v\n",
							rep.Snapshot.BuiltAt.Format("2006-01-02 15:04"), rep.SnapshotAge, rep.Snapshot.LdgVersion,
							rep.Snapshot.SchemaVersion, rep.Snapshot.RulesVersion, rep.RulesKnown, rep.Snapshot.Packages, rep.Precomputed)
					} else {
						fmt.Fprintf(w, "snapshot  %s\n", rep.SnapshotError)
					}
					fmt.Fprintf(w, "links     %s works in the data directory\n", rep.LinkProbe)
					fmt.Fprintf(w, "contentdb %s\n", rep.ContentDB)
					for _, b := range rep.Binaries {
						fmt.Fprintf(w, "luanti    %s %s (RUN_IN_PLACE=%v)\n", b.Path, b.Version, b.RunInPlace)
					}
					if len(rep.Installations) == 0 {
						fmt.Fprintln(w, "installs  none registered (luanthetti installs detect --add)")
					}
					for _, i := range rep.Installations {
						state := "ok"
						if !i.Exists {
							state = "MISSING"
						}
						fmt.Fprintf(w, "install   %s %s (%s, %d packages, %s)\n", i.Name, i.Root, dash(i.EngineVersion), i.Links, state)
					}
					_ = time.Now
				})
			})
		},
	}
}
