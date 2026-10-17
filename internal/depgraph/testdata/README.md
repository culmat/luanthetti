# depgraph test data

- `mini.sqlite`: built by luanti-depgraph from its own test fixtures, then patched.
- `mini-patch.sql`: adds what an offline build leaves empty (declared game support,
  supports_all_games, dev_state, an undeclared reference that makes pipeworks risky).
- `golden/*.json`: `ldg ... --json` output for a fixed list of cases; `golden/VERSION` records the
  ldg version and commit that produced them.

Regenerate (needs `uv` and a luanti-depgraph checkout):

```sh
W=$(mktemp -d); LDG=~/git/luanti-depgraph
mkdir -p "$W/home/cache/raw" "$W/home/corpus/mt-mods"
cp "$LDG"/tests/fixtures/contentdb/*.json "$W/home/cache/raw/"
cp -R "$LDG/tests/fixtures/mods/my_modpack" "$W/home/corpus/mt-mods/pipeworks"
printf '201 test\n' > "$W/home/corpus/mt-mods/pipeworks/.ldg-release"
cd "$LDG" && LDG_HOME="$W/home" uv run ldg build --offline
sqlite3 "$W/home/luanti-deps.sqlite" < internal/depgraph/testdata/mini-patch.sql   # from the luanthetti checkout
# Recompute the precomputed tables with the patch in place:
LDG_HOME="$W/home" uv run python -c 'import sqlite3,json; from luanti_depgraph import db; c=sqlite3.connect("'"$W"'/home/luanti-deps.sqlite"); c.execute("DELETE FROM compat_matrix"); c.execute("DELETE FROM resolution"); c.execute("DELETE FROM meta WHERE key IN (\"precomputed_games\",\"compat_rows\",\"resolutions\")"); s=db._precompute(c, print); c.executemany("INSERT INTO meta VALUES (?, ?)", [(k, json.dumps(v)) for k, v in s.items()]); c.commit(); c.execute("VACUUM")'
cp "$W/home/luanti-deps.sqlite" internal/depgraph/testdata/mini.sqlite
```

Then rerun each case in `goldenCases` (depgraph_test.go) with `LDG_HOME="$W/home" uv run ldg <args> --json > golden/<case>.json`.
