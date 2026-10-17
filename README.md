# luanthetti

One library of [Luanti](https://www.luanti.org) mods, games and texture packs, linked into every Luanti installation you have, with [luanti-depgraph](https://github.com/culmat/luanti-depgraph) telling you in which games each package really works.

> Luanthetti = Luanti + spaghetti, because dependencies are entangled like spaghetti.

```console
$ luanthetti installs detect --add
desktop   ~/Library/Application Support/minetest   Luanti 5.17.0   games: minetest_game, mineclonia
server    /srv/luanti                               Luanti 5.17.0   games: mineclonia

$ luanthetti install TenPlus1/ethereal
TenPlus1/ethereal 1.30 (release 31245)
  desktop  mods/ethereal  linked   minetest_game: ok   mineclonia: unsatisfiable (needs default)
  server   skipped        mineclonia: unsatisfiable (needs default)
```

**Status: work in progress.** The command line and file layout may still change before 0.1.0.

## Why

- **Luanti installs each package into one `mods/` folder per installation** and lets worlds pick them in `world.mt`. That is fine for one desktop. With a desktop, a server and a Docker root you download, update and track everything three times.
- **ContentDB tells you which games a mod *declares*.** Whether it also *works* there, and what it pulls in, needs a dependency graph. Luanthetti reads luanti-depgraph's nightly snapshot and shows `ok`, `risky` (uses another mod's API without declaring it) or `unsatisfiable` per game before you install.
- **Finding things is hard.** Filters by type, tag, maturity, popularity, maintenance and "works in my games" are built into search and the web UI.

Luanthetti complements [luma](https://github.com/luanti-org/luma), the official terminal tool for one installation. Both tools, and the in-game installer, recognise each other's installs. See [docs/coexistence.md](docs/coexistence.md).

| Question | Command |
|---|---|
| Which Luanti installations are on this machine? | `luanthetti installs detect` |
| Install a package into every installation it works in | `luanthetti install author/name` |
| Only into one installation | `luanthetti install author/name --into server` |
| In which games does it work, and what would it pull in? | `luanthetti info author/name` |
| Find mods that work in my games | `luanthetti search farming --compatible-with desktop` |
| What is outdated, and update it | `luanthetti update --check`, `luanthetti update` |
| Bring existing installs under management | `luanthetti scan`, `luanthetti adopt` |
| Browse with a GUI | `luanthetti ui` |

## Install

Download the archive for your platform from the [releases page](https://github.com/culmat/luanthetti/releases), extract it, and put `luanthetti` somewhere on your `PATH`. Builds exist for Linux, macOS and Windows on amd64 and arm64. Each release ships `SHA256SUMS`.

With Go installed: `go install github.com/culmat/luanthetti/cmd/luanthetti@latest`.

## How it works

- **Library:** every package version is stored once under the Luanthetti data directory. Installations get a symlink (a junction on Windows, a copy where neither works) in their `mods/`, `games/` or `textures/` folder.
- **Compatibility:** the nightly luanti-depgraph snapshot (a few MB, downloaded at most once a day) carries ContentDB metadata plus the resolution of every package against the most-played games. Luanthetti reads that and resolves the rest itself with the same rules.
- **Updates:** compare the `release` recorded in each package's `.conf` with ContentDB's `/api/updates/`, like the in-game updater. Previous versions stay in the library for `rollback`; `pin` holds a package in one installation.
- **Scan and adopt:** anything in an installation with `author` and `release` in its conf can be moved into the library and replaced with a link. Everything else is left alone.

## Being a good citizen

The client identifies itself (`luanthetti/<version> (+https://github.com/culmat/luanthetti)`), never exceeds 2 requests per second, caches by release id, and serves thumbnails through its own cache. Package metadata comes from the luanti-depgraph snapshot, which is built once a day, not from crawling ContentDB.

## Contributing

```sh
go build ./... && go vet ./... && go test ./...
```

See [AGENTS.md](AGENTS.md) for conventions. This project is developed with AI assistance, reviewed by a human maintainer. Part of [avanti-luanti](https://github.com/culmat/avanti-luanti).

## License

MIT. ContentDB data belongs to its respective authors; see [content.luanti.org](https://content.luanti.org).
