package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/culmat/luanthetti/internal/app"
)

// open builds the app from the global flags. Logs go to stderr (and nowhere with --json
// unless -v), so stdout stays parseable.
func (g *globals) open() (*app.App, error) {
	return app.New(app.Options{
		Home:         g.home,
		ContentDBURL: g.cdbURL,
		Offline:      g.offline,
		Log: func(format string, args ...any) {
			if g.json && !g.verbose {
				return
			}
			fmt.Fprintf(g.err, format+"\n", args...)
		},
	})
}

// withApp opens the app, runs fn and closes it.
func (g *globals) withApp(fn func(a *app.App) error) error {
	a, err := g.open()
	if err != nil {
		return err
	}
	defer a.Close()
	return fn(a)
}

func table(w io.Writer, header string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	if header != "" {
		fmt.Fprintln(tw, header)
	}
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// isTerminal reports whether r is an interactive terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// confirm asks a yes/no question, luma style: only "y" or "yes" continue. Without a terminal
// it refuses unless --yes was given.
func (g *globals) confirm(question string) (bool, error) {
	if g.yes {
		return true, nil
	}
	if !isTerminal(g.in) {
		return false, errors.New("new dependencies need confirmation; rerun with --yes (or --no-deps)")
	}
	fmt.Fprintf(g.err, "%s [y/N] ", question)
	line, _ := bufio.NewReader(g.in).ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// statusMark is the compact compat mark used in tables.
func statusMark(s string) string {
	switch s {
	case "ok":
		return "ok"
	case "risky":
		return "~"
	case "unsatisfiable":
		return "x"
	}
	return "?"
}
