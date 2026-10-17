package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || !strings.HasPrefix(out, "luanthetti ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "--json", "version")
	var v map[string]any
	if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v["version"] == "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{"nope"}, {"--bogus"}, {"version", "extra"}} {
		code, _, errOut := run(t, args...)
		if code != ExitUsage {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
	}
}

func TestParseWithin(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{"6m": 180 * day, "90d": 90 * day, "2w": 14 * day, "1y": 365 * day, "48h": 48 * time.Hour} {
		got, err := parseWithin(in)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"m", "xd", "-3d", "5q"} {
		if _, err := parseWithin(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestCLIDocsUpToDate(t *testing.T) {
	var buf bytes.Buffer
	WriteMarkdown(&buf)
	want, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Error("docs/cli.md is out of date; run `go run ./tools/gendocs > docs/cli.md`")
	}
}
