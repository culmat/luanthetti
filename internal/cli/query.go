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

func init() { commands = append(commands, newInfoCmd, newSearchCmd) }

func newInfoCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "info <author/name|name>",
		Short: "Show a package: details, releases, compatibility with your games, where it is installed",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.Info(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					fmt.Fprintf(w, "%s (%s): %s\n", rep.Package, rep.Type, rep.Title)
					if rep.ShortDescription != "" {
						fmt.Fprintf(w, "  %s\n", rep.ShortDescription)
					}
					fmt.Fprintf(w, "  %s\n", rep.URL)
					d := rep.Details
					fmt.Fprintf(w, "  downloads %d, score %.0f, %s, license %s\n", rep.Downloads, rep.Score, dash(strings.ReplaceAll(d.DevState, "_", " ")), dash(d.License))
					if len(d.Tags) > 0 {
						fmt.Fprintf(w, "  tags: %s\n", strings.Join(d.Tags, ", "))
					}
					if len(rep.Provides) > 1 || (len(rep.Provides) == 1 && !strings.HasSuffix(rep.Package, "/"+rep.Provides[0])) {
						fmt.Fprintf(w, "  provides: %s\n", strings.Join(rep.Provides, ", "))
					}
					fmt.Fprintf(w, "  depends: %s\n", dash(strings.Join(rep.Depends, ", ")))
					if len(rep.OptionalDepends) > 0 {
						fmt.Fprintf(w, "  optional: %s\n", strings.Join(rep.OptionalDepends, ", "))
					}
					if len(rep.Releases) > 0 {
						r := rep.Releases[0]
						fmt.Fprintf(w, "  latest release: %s (id %d, %s)", r.Title, r.ID, dash(firstN(r.Date, 10)))
						switch {
						case r.MinEngine != "" && r.MaxEngine != "":
							fmt.Fprintf(w, ", Luanti %s to %s", r.MinEngine, r.MaxEngine)
						case r.MinEngine != "":
							fmt.Fprintf(w, ", Luanti %s or newer", r.MinEngine)
						case r.MaxEngine != "":
							fmt.Fprintf(w, ", Luanti up to %s", r.MaxEngine)
						}
						fmt.Fprintln(w)
					}
					if len(rep.Compat) > 0 {
						fmt.Fprintln(w, "\nCompatibility:")
						var rows [][]string
						for _, c := range rep.Compat {
							detail := ""
							switch {
							case len(c.Missing) > 0:
								detail = "missing " + strings.Join(c.Missing, ", ")
							case len(c.UndeclaredMissing) > 0:
								detail = "uses undeclared " + strings.Join(c.UndeclaredMissing, ", ")
							case len(c.Install) > 0:
								detail = "installs " + strings.Join(c.Install, ", ")
							}
							claim := ""
							if c.ContentDBSays != nil {
								claim = "ContentDB: no"
								if c.ContentDBSays.Supports {
									claim = "ContentDB: yes"
								}
							}
							rows = append(rows, []string{"  " + c.Game, c.Status, detail, claim})
						}
						table(w, "", rows)
					}
					if len(rep.Installed) > 0 {
						fmt.Fprintln(w, "\nInstalled:")
						for _, r := range rep.Installed {
							fmt.Fprintf(w, "  %s: release %d at %s (%s)\n", r.Installation, r.Release, r.Path, r.State)
						}
					}
					for _, n := range rep.Notes {
						fmt.Fprintln(w, "note:", n)
					}
				})
			})
		},
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func newSearchCmd(g *globals) *cobra.Command {
	var q app.SearchQuery
	var within string
	cmd := &cobra.Command{
		Use:   "search [text]",
		Short: "Find packages, with filters and compatibility marks for your games",
		Long: `Search the dependency snapshot (works offline). Compatibility marks are shown for the
games of your installations: ok, ~ (risky: uses another mod's API without declaring it),
x (unsatisfiable), ? (unknown).`,
		Args: wrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				q.Q = args[0]
			}
			if within != "" {
				d, err := parseWithin(within)
				if err != nil {
					return &UsageError{Msg: err.Error()}
				}
				q.UpdatedWithin = d
			}
			return g.withApp(func(a *app.App) error {
				rep, err := a.Search(cmd.Context(), q)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					if len(rep.Hits) == 0 {
						fmt.Fprintln(w, "No packages match.")
					}
					var gameKeys []string
					for _, gm := range rep.Games {
						gameKeys = append(gameKeys, gm.Key)
					}
					header := "PACKAGE\tTYPE\tDOWNLOADS\tSTATE\tUPDATED"
					var short []string
					for _, k := range gameKeys {
						short = append(short, k[strings.Index(k, "/")+1:])
					}
					if len(short) > 0 {
						header += "\t" + strings.Join(short, " ")
					}
					header += "\tTITLE"
					var rows [][]string
					for _, h := range rep.Hits {
						row := []string{h.Package, h.Type, fmt.Sprint(h.Downloads), dash(strings.ReplaceAll(h.DevState, "_", " ")), dash(firstN(h.LatestReleaseAt, 10))}
						if len(gameKeys) > 0 {
							var marks []string
							for _, k := range gameKeys {
								m := "-"
								if s, ok := h.Compat[k]; ok {
									m = statusMark(s)
								}
								marks = append(marks, fmt.Sprintf("%-*s", len(k[strings.Index(k, "/")+1:]), m))
							}
							row = append(row, strings.Join(marks, " "))
						}
						title := h.Title
						if len(h.InstalledIn) > 0 {
							sort.Strings(h.InstalledIn)
							title += " [installed: " + strings.Join(h.InstalledIn, ", ") + "]"
						}
						rows = append(rows, append(row, title))
					}
					table(w, header, rows)
					if rep.Total > len(rep.Hits) {
						fmt.Fprintf(w, "\n%d of %d shown; use --limit to see more.\n", len(rep.Hits), rep.Total)
					}
					for _, n := range rep.Notes {
						fmt.Fprintln(w, "note:", n)
					}
				})
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&q.Type, "type", "", "mod | game | txp")
	f.StringSliceVar(&q.Tags, "tag", nil, "ContentDB tag(s), all must match (e.g. tools, building)")
	f.StringSliceVar(&q.DevStates, "dev-state", nil, "wip, beta, actively_developed, maintenance_only, as_is, deprecated, looking_for_maintainer")
	f.Int64Var(&q.MinDownloads, "min-downloads", 0, "at least this many downloads")
	f.Float64Var(&q.MinScore, "min-score", 0, "at least this ContentDB score")
	f.StringVar(&within, "updated-within", "", "latest release within this period (e.g. 6m, 1y, 90d)")
	f.StringSliceVar(&q.CompatibleWith, "compatible-with", nil, "installation or game name(s): only packages that work there")
	f.StringVar(&q.Sort, "sort", "score", "score | downloads | updated | name")
	f.IntVar(&q.Limit, "limit", 30, "maximum results")
	return cmd
}

// parseWithin accepts d (days), w (weeks), m (months), y (years), and Go durations such as
// 48h. "m" means months here, not minutes.
func parseWithin(s string) (time.Duration, error) {
	if len(s) >= 2 && strings.ContainsRune("dwmy", rune(s[len(s)-1])) && !strings.HasSuffix(s, "ms") {
		// handled below
	} else if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid period %q", s)
	}
	var n int
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid period %q (use e.g. 90d, 6m, 1y)", s)
	}
	day := 24 * time.Hour
	switch s[len(s)-1] {
	case 'd':
		return time.Duration(n) * day, nil
	case 'w':
		return time.Duration(n) * 7 * day, nil
	case 'm':
		return time.Duration(n) * 30 * day, nil
	case 'y':
		return time.Duration(n) * 365 * day, nil
	}
	return 0, fmt.Errorf("invalid period %q (use e.g. 90d, 6m, 1y)", s)
}
