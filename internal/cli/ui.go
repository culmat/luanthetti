package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/culmat/luanthetti/internal/app"
	"github.com/culmat/luanthetti/internal/web"
)

func init() { commands = append(commands, newUICmd) }

func newUICmd(g *globals) *cobra.Command {
	var port int
	var noOpen, printURL bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the web UI (served from this binary on localhost)",
		Long: `ui serves Luanthetti's browser interface on 127.0.0.1 and opens it. The URL carries a
per-run access token; other local programs and web pages cannot use the API without it.
Stop it with Ctrl+C.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withApp(func(a *app.App) error {
				if !cmd.Flags().Changed("port") {
					port = a.Config.UI.Port
				}
				s := web.New(cmd.Context(), a)
				ln, url, err := s.Listen(port)
				if err != nil {
					return err
				}
				if printURL || g.json {
					fmt.Fprintln(g.out, url)
				} else {
					fmt.Fprintf(g.out, "Luanthetti UI: %s\nPress Ctrl+C to stop.\n", url)
				}
				if !noOpen && a.Config.UI.OpenBrowser {
					if err := openBrowser(url); err != nil {
						fmt.Fprintf(g.err, "could not open a browser (%v); open the URL yourself\n", err)
					}
				}
				return s.Serve(cmd.Context(), ln)
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", 7331, "port on 127.0.0.1 (0 picks a free one)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open a browser")
	cmd.Flags().BoolVar(&printURL, "print-url", false, "print only the URL")
	return cmd
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
