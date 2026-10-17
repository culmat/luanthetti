# luanthetti: notes for coding agents working on this repo

Read by any coding agent (Claude Code reads it through CLAUDE.md).

Luanthetti is a Go CLI plus embedded web UI that keeps Luanti content (mods, games, texture packs) in one library and links it into several Luanti installations. It uses the nightly SQLite snapshot of [culmat/luanti-depgraph](https://github.com/culmat/luanti-depgraph) (`ldg`) as its compatibility engine. It complements [luanti-org/luma](https://github.com/luanti-org/luma); see `docs/coexistence.md`.

## Layout

| Path | What |
|---|---|
| `cmd/luanthetti/` | `main` only: version variables and `os.Exit(cli.Main(os.Args))`. |
| `internal/cli/` | cobra commands. Thin: parse flags, call `internal/app`, render. One file per verb. |
| `internal/app/` | The use-case layer shared by CLI and web UI. No printing here; return report structs. |
| `internal/depgraph/` | Go port of ldg's resolver (`resolve.py`) and the snapshot reader. Precomputed rows win over the port. |
| `internal/luanticonf/` | `.conf` parsing/editing, modpack/game/txp scanning, `world.mt` (port of ldg's `luanticonf.py`). |
| `internal/library/`, `installs/`, `linker/`, `store/`, `snapshot/`, `contentdb/`, `thumbs/`, `jobs/` | One concern each; see the package doc comments. |
| `internal/web/` | HTTP server, JSON API, SSE, embedded static UI (`static/`). |
| `docs/` | `concept.md`, `research.md`, `coexistence.md`, `cli.md` (generated). |

## Conventions

- **Go 1.26+, `CGO_ENABLED=0`.** Pure-Go SQLite (`modernc.org/sqlite`). The binary must build for linux/darwin/windows × amd64/arm64 with `go build` alone.
- **`gofmt`, `go vet`, `staticcheck` clean.** Tests: `go test ./...` on all three OSes (CI).
- **Every command has `--json`** and prints the same struct the web API serves (`internal/app/report.go`). Exit codes: 0 ok, 1 error, 2 usage.
- **Facts about engine behaviour must be verified** against the engine source or docs (`~/.avanti/corpus/engine/doc/`, `builtin/mainmenu/content/`) before they go into code comments or docs. Cite the file.
- **Interoperability is a requirement.** Write `author`, `name`, `release` (and `title`/`description` when absent) into the package `.conf` exactly as the in-game installer does, so luma and Luanti recognise our installs. Never touch a directory without `author` + `release` in its conf unless `--force`.
- **ContentDB etiquette:** descriptive User-Agent, at most 2 requests/s, cache by release id, bulk endpoints. Normal use reads the ldg snapshot, not ContentDB.
- **Depgraph fidelity:** `internal/depgraph` must agree with `ldg --json`. The conformance test compares the port against every precomputed row in the snapshot. Rule changes happen in ldg first (with `rules_version` bump), then here.
- **Paths:** tests always set `LUANTHETTI_HOME` to a temp dir. Never write outside it in tests.
- **Wording:** say "agent", not a vendor name, in docs and skills.

## Testing changes

```sh
go build ./... && go vet ./... && go test ./...
go run ./cmd/luanthetti --home "$(mktemp -d)" doctor
LDG_GOLDEN=1 go test ./internal/depgraph   # needs `ldg` on PATH and a downloaded snapshot
```

## Rules

Follow the avanti AI policy ([avanti-luanti/docs/ai-policy.md](https://github.com/culmat/avanti-luanti/blob/main/docs/ai-policy.md)):
- Never publish without explicit approval from the user in the session: no git tag push, GitHub release, brew/scoop update, forum post, upstream issue or PR.
- Work directly on `main` in culmat repos (no PRs there). Never open PRs or issues on third-party repos; draft them for a human instead.
- AI assistance is disclosed in the README.
