package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
