package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/app"
)

func init() { commands = append(commands, newInstallsCmd) }

func newInstallsCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "installs",
		Aliases: []string{"installations"},
		Short:   "Register and list Luanti installations",
		Args:    wrapArgs(cobra.NoArgs),
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	var add bool
	detect := &cobra.Command{
		Use:   "detect",
		Short: "Find Luanti installations on this machine",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.DetectInstalls(cmd.Context(), add)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					if len(rep.Candidates) == 0 {
						fmt.Fprintln(w, "No Luanti installation found. Register one with `luanthetti installs add <path>`.")
						return
					}
					var rows [][]string
					for _, c := range rep.Candidates {
						var games []string
						for _, gm := range c.Games {
							games = append(games, gm.ID)
						}
						rows = append(rows, []string{dash(c.Registered), c.Kind, c.Root, dash(c.EngineVersion), fmt.Sprint(c.Mods), dash(strings.Join(games, ", "))})
					}
					table(w, "REGISTERED\tKIND\tPATH\tLUANTI\tMODS\tGAMES", rows)
					if !add {
						fmt.Fprintln(w, "\nRegister them with `luanthetti installs detect --add` or `luanthetti installs add <path>`.")
					}
				})
			})
		},
	}
	detect.Flags().BoolVar(&add, "add", false, "register the installations found")

	var req app.AddInstallRequest
	addCmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a Luanti installation (user path, server root or RUN_IN_PLACE build)",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Path = args[0]
			return g.withApp(func(a *app.App) error {
				inst, err := a.AddInstall(cmd.Context(), req)
				if err != nil {
					return err
				}
				return g.emit(inst, func(w io.Writer) {
					fmt.Fprintf(w, "Registered %q: %s (Luanti %s, links: %s)\n", inst.Name, inst.Root, dash(inst.EngineVersion), inst.LinkMode)
				})
			})
		},
	}
	f := addCmd.Flags()
	f.StringVar(&req.Name, "name", "", "name to refer to it (default: desktop for a user path, else the folder name)")
	f.StringVar(&req.Kind, "kind", "", "user | server | run_in_place | flatpak | docker")
	f.StringVar(&req.EngineVersion, "engine-version", "", "Luanti version, when there is no binary to ask (e.g. 5.17.0)")
	f.StringVar(&req.LinkMode, "link-mode", "", "auto | symlink | junction | copy | env")
	f.BoolVar(&req.Create, "create", false, "create mods/, games/, textures/ and worlds/ if the folder is not an installation yet")

	list := &cobra.Command{
		Use:   "list",
		Short: "List registered installations and their games",
		Args:  wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				views, err := a.ListInstalls(cmd.Context())
				if err != nil {
					return err
				}
				return g.emit(views, func(w io.Writer) {
					if len(views) == 0 {
						fmt.Fprintln(w, "No installations registered. Try `luanthetti installs detect --add`.")
						return
					}
					var rows [][]string
					for _, v := range views {
						var games []string
						for _, gm := range v.Games {
							games = append(games, gm.ID)
						}
						for _, gm := range v.UnknownGames {
							games = append(games, gm.ID+"?")
						}
						root := v.Root
						if !v.Exists {
							root += " (missing)"
						}
						rows = append(rows, []string{v.Name, v.Kind, root, dash(v.EngineVersion), v.LinkMode, fmt.Sprint(v.Links), dash(strings.Join(games, ", "))})
					}
					table(w, "NAME\tKIND\tPATH\tLUANTI\tLINKS\tPACKAGES\tGAMES", rows)
				})
			})
		},
	}
	var unlink bool
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Forget an installation (its folders stay as they are unless --unlink)",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				rep, err := a.RemoveInstall(cmd.Context(), args[0], unlink)
				if err != nil {
					return err
				}
				return g.emit(rep, func(w io.Writer) {
					for _, r := range rep.Removed {
						fmt.Fprintf(w, "unlinked %s (%s)\n", r.Package, r.Path)
					}
					for _, warn := range rep.Warnings {
						fmt.Fprintln(w, "note:", warn)
					}
					fmt.Fprintf(w, "Removed installation %q.\n", args[0])
				})
			})
		},
	}
	remove.Flags().BoolVar(&unlink, "unlink", false, "remove every link Luanthetti placed in it first")
	games := &cobra.Command{
		Use:   "games <name>",
		Short: "List the games of an installation and how they map to ContentDB",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				views, err := a.ListInstalls(cmd.Context())
				if err != nil {
					return err
				}
				for _, v := range views {
					if v.Name != args[0] {
						continue
					}
					out := struct {
						Games   []app.GameRef `json:"games"`
						Unknown []app.GameRef `json:"unknown_games"`
					}{v.Games, v.UnknownGames}
					return g.emit(out, func(w io.Writer) {
						var rows [][]string
						for _, gm := range v.Games {
							rows = append(rows, []string{gm.ID, gm.Key, dash(gm.Title)})
						}
						for _, gm := range v.UnknownGames {
							rows = append(rows, []string{gm.ID, "(not on ContentDB)", dash(gm.Title)})
						}
						table(w, "GAME ID\tCONTENTDB\tTITLE", rows)
					})
				}
				return fmt.Errorf("no installation %q", args[0])
			})
		},
	}
	cmd.AddCommand(detect, addCmd, list, remove, games)
	return cmd
}
