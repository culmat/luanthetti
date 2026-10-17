# luanthetti

One library of [Luanti](https://www.luanti.org) mods, games and texture packs, linked into every Luanti installation you have, with [luanti-depgraph](https://github.com/culmat/luanti-depgraph) telling you in which games each package really works.

> Luanthetti = Luanti + spaghetti, because dependencies are entangled like spaghetti.

```console
$ luanthetti installs detect --add
REGISTERED  KIND  PATH                                       LUANTI  MODS  GAMES
desktop     user  ~/Library/Application Support/minetest     5.17.0  25    asuna, mineclone2, mineclonia

$ luanthetti install mt-mods/pipeworks --yes
desktop:
  games: EmptyStar/asuna ok; Wuzzy/mineclone2 ok; ryvnf/mineclonia ok
  installed mt-mods/pipeworks (release 39410) → mods/pipeworks [symlink]
  installed OgelGames/fakelib (release 37365) for mt-mods/pipeworks → mods/fakelib [symlink]
  installed mt-mods/basic_materials (release 38908) for mt-mods/pipeworks → mods/basic_materials [symlink]
  installed mt-mods/xcompat (release 38678) for mt-mods/basic_materials → mods/xcompat [symlink]

$ luanthetti search --tag technology --updated-within 6m --compatible-with desktop
```

**Status: work in progress.** Nothing is released yet; the command line and file layout may still change before 0.1.0. Build it with Go 1.26 or newer (see Install).

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

With Go 1.26 or newer: `go install github.com/culmat/luanthetti/cmd/luanthetti@latest`.

See [docs/cli.md](docs/cli.md) for every command and flag, and [docs/concept.md](docs/concept.md) for the design.

## Web UI

`luanthetti ui` opens a page served by the binary itself on `127.0.0.1`. It offers search with filters and per-game compatibility badges, package details, an install plan to confirm before anything changes, the installed packages, updates, installations, and live job progress. It needs no internet connection for what is already cached, loads nothing from other sites, and only accepts requests that carry the access token from the URL it prints.

## How it works

- **Library:** every package release is stored once under the Luanthetti data directory. Installations get a symbolic link (a copy where links are not possible) in their `mods/`, `games/` or `textures/` folder. Luanti loads linked packages like normal folders.
- **Compatibility:** the nightly luanti-depgraph snapshot (a few MB, downloaded at most once a day) carries ContentDB metadata plus the resolution of every package against the most-played games. Luanthetti reads that and resolves the rest itself with the same rules.
- **Updates:** compare the `release` recorded in each package's `.conf` with ContentDB's `/api/updates/`, like the in-game updater. Previous versions stay in the library for `rollback`; `pin` holds a package in one installation.
- **Scan and adopt:** anything in an installation with `author` and `release` in its conf can be moved into the library and replaced with a link. Everything else is left alone.
- **Living with other tools:** Luanti's content browser and luma see Luanthetti's packages as normal installs and can update them; Luanthetti notices and re-adopts the result. See [docs/coexistence.md](docs/coexistence.md).

## Being a good citizen

The client identifies itself (`luanthetti/<version> (+https://github.com/culmat/luanthetti)`), never exceeds 2 requests per second, caches by release id, and serves thumbnails through its own cache. Package metadata comes from the luanti-depgraph snapshot, which is built once a day, not from crawling ContentDB.

## Contributing

```sh
go build ./... && go vet ./... && go test ./...
```

See [AGENTS.md](AGENTS.md) for conventions. This project is developed with AI assistance, reviewed by a human maintainer. Part of [avanti-luanti](https://github.com/culmat/avanti-luanti).

## License

MIT. ContentDB data belongs to its respective authors; see [content.luanti.org](https://content.luanti.org).
