# Luanthetti: one library of Luanti content, many installations

> **Luanthetti** = Luanti + spaghetti, because dependencies are entangled like spaghetti.
> Pronounced loo-ahn-TET-tee. CLI command: `luanthetti`.

*Status: concept, revised 2026-10-17 after research. The first draft assumed Luanti installs mods per game; it does not (see [What Luanti actually does](#what-luanti-actually-does)). This revision reflects the corrected scope and the existence of [luma](https://github.com/luanti-org/luma).*

## Summary

Luanthetti is a standalone, optional content manager for [Luanti](https://www.luanti.org). It keeps every mod, game and texture pack **once**, in a library, and links each package into **every Luanti installation** the user registers: the desktop client, a headless server, a Docker root, a development build. It uses [luanti-depgraph](https://github.com/culmat/luanti-depgraph) to tell, before installing, **in which games a package works** (`ok`, `risky`, `unsatisfiable`) and what it pulls in. It ships as a single Go binary with a command line and an embedded web UI, and it coexists with the in-game installer and with luma.

## What Luanti actually does

Verified against the engine source (`builtin/mainmenu/content/dlg_install.lua`, `pkgmgr.lua`, `doc/lua_api.md`, `doc/world_format.md`):

- Mods load from three places per **installation** (user path): `games/<gameid>/mods/` (bundled with the game), `mods/` (user mods, shared by all games) and `worlds/<world>/worldmods/`.
- The in-game ContentDB installer puts every mod into that single `mods/` folder (`core.get_modpath()`). Games go to `games/`, texture packs to `textures/`.
- Each **world** decides which user mods to load through `load_mod_<name> = true|false|mods/<modpack>/<dir>` in `world.mt`.
- The "base game" dropdown on ContentDB and in the client is a **compatibility filter**. It never changes where a package is installed.
- Luanti recognises a ContentDB install by `author` and `release` in the package's `.conf`, and checks `/api/updates/` for newer release ids.

So inside one installation there is no duplication across games. The duplication, and the update tedium, appear when a user has **several installations**. That is the problem Luanthetti solves.

## Motivation

1. **Several installations, one set of content.** A desktop client, a LAN server, a public server in Docker and a modding checkout each have their own `mods/`. Today each is downloaded, updated and tracked separately.
2. **Declared support is not the same as working.** ContentDB shows which games a mod declares or is detected to support. Whether its dependencies can be met in a game, and whether it calls another mod's API without declaring it, needs a dependency graph. luanti-depgraph has that graph.
3. **Discovery.** ContentDB has thousands of packages. Filtering by type, tag, maturity, popularity, maintenance and "works in my games" at once is not available in one place.
4. **Servers need a command line.** The in-game installer does not help on a headless server; luma covers one installation at a time.

## Architecture

```
<data>/library/
├── mod/TenPlus1/ethereal/31245/ethereal/     one copy per package release, Luanti-ready folder
├── mod/mt-mods/xcompat/30980/xcompat/
└── txp/.../

installation "desktop"  ~/Library/Application Support/minetest/
└── mods/ethereal  → <library>/mod/TenPlus1/ethereal/31245/ethereal     (symlink)
installation "server"   /srv/luanti/
└── mods/ethereal  → <library>/mod/TenPlus1/ethereal/31245/ethereal
└── mods/xcompat   → ...                                                   (dependency)
```

Principles:

1. **Install once, link everywhere.** A package release lives once in the library. Each registered installation gets a symbolic link (a copy where links are not possible, and on Windows by default) in `mods/`, `games/` or `textures/`. See [coexistence.md](coexistence.md#windows) for why.
2. **Installations, not games, are the targets.** `install author/name` links into every registered installation by default, `--into` restricts it. Which games a package works in is a per-game status that Luanthetti shows and uses to pick dependencies, not an install location.
3. **Per-world control stays in `world.mt`.** Luanthetti does not reinvent it; luma's roadmap covers editing it.
4. **Interoperable by construction.** Luanthetti writes `author`, `name`, `release` (and `title`, `description` when absent) into the `.conf` exactly as the in-game installer does. Luanti, luma and Luanthetti recognise each other's installs. `scan` reports when another tool replaced a link with a real folder.
5. **Versions and pins.** Old releases stay in the library (configurable count) for `rollback`; a package can be pinned per installation.

## Compatibility engine

luanti-depgraph (`ldg`) publishes a nightly SQLite snapshot: every ContentDB package with the mod names it provides and depends on, declared game support, static-analysis references for the most-used packages, and, as part of this project, the precomputed resolution of every package against the most-played games. Luanthetti downloads that snapshot (a few MB, at most once a day) and:

- reads the precomputed per-game status (`ok` / `risky` / `unsatisfiable`) and install plan when present;
- resolves the rest itself with a Go port of ldg's rules (game-bundled names satisfy; hard dependencies come from ContentDB preferring the package named like the mod; a provider counts only if its own dependencies can be met; declared `supported_games` are respected; a few games' non-standard Minetest Game copies are ignored);
- is tested against every precomputed row, so a rule change in ldg shows up as a failing test, not as a silent difference.

`risky` means the package uses another mod's API (a Lua global) without declaring the dependency, and that mod is not available in the game. It is the class of bug that works in Minetest Game and crashes in Mineclonia.

## Discovery

| Dimension | Values | Source |
|---|---|---|
| Type | mod, game, texture pack | snapshot |
| Category | ContentDB tags | snapshot (added by this project) |
| Maturity | `dev_state`: wip, beta, actively developed, maintenance only, as-is, deprecated, looking for maintainer | snapshot |
| Popularity | downloads, score | snapshot |
| Maintenance | latest release date, creation date | snapshot (added by this project) |
| Compatibility | ok / risky / unsatisfiable per game of the user's installations | snapshot + resolver |
| Text | title, short description, thumbnail | ContentDB `/api/packages/` |

Theme tags (realistic, fantasy, sci-fi) would be an enrichment on ContentDB's side and are out of scope here.

## User interface

**Command line** (exit codes 0 ok, 1 error, 2 usage; every command has `--json`):

```sh
luanthetti installs detect --add          # find Luanti installations on this machine
luanthetti install Wuzza/digilines        # into every installation it works in
luanthetti install Wuzza/digilines --into server
luanthetti info Wuzza/digilines           # details, releases, per-game compatibility
luanthetti search "farming" --type mod --tag farming --compatible-with desktop
luanthetti list --into desktop
luanthetti update --check && luanthetti update
luanthetti pin Wuzza/digilines --into server
luanthetti scan && luanthetti adopt       # bring existing installs under management
luanthetti verify && luanthetti repair
luanthetti ui                             # web UI on localhost
```

**Web UI**: `luanthetti ui` serves a local page from the binary (no browser dependencies, no CDN): search with live results, cards with thumbnail, release, downloads and per-game compatibility marks, a filter sidebar, an Installed view per installation, an Updates view and job progress. Mutations need a per-run token; the server binds to localhost only.

## Updates

1. One `/api/updates/` call per engine version in use (cached 24 h, as the client does) maps `author/name` to the latest release id.
2. Compare with the `release` recorded in each linked package's `.conf`.
3. Resolve the new release against each installation's games; report newly needed dependencies and games that would stop working.
4. Download the new release into the library, switch links atomically, keep the previous version for `rollback`.

Pinned packages and excluded names are skipped.

## Backwards compatibility and migration

- Installations keep their normal layout; Luanti sees linked folders as regular folders.
- The in-game ContentDB tab shows a linked package as installed, with its release, and can update it. Luanthetti's `scan` then shows the folder as `replaced`, and `repair --readopt` brings it back into the library.
- `scan` lists what is in an installation: `managed`, `adoptable` (has `author` and `release`), `git` (a checkout, left alone), `unmanaged` (no ContentDB identity, left alone), `broken_link`, `replaced`.
- Where symlinks are not possible, links degrade to copies and are recorded as such. [coexistence.md](coexistence.md) covers what happens when another tool updates a linked package.
- For servers started from a script, `luanthetti env <inst>` prints `LUANTI_MOD_PATH` lines that expose the library without any links.

## Relation to other tools

| | in-game installer | luma | luctl | Luanthetti |
|---|---|---|---|---|
| Scope | one installation | one installation | one server, declarative manifest | several installations |
| Interface | GUI | TUI + CLI | CLI | CLI + web UI |
| Dependencies | ContentDB API | ContentDB API | – | luanti-depgraph snapshot + resolver |
| Compatibility per game | declared | declared | – | resolved (ok / risky / unsatisfiable) |
| Shared library, pins, rollback | – | – | – | yes |
| Discovery filters | ContentDB web | – | – | built in |
| World enable/disable | yes | planned | yes | no (by design) |

See [research.md](research.md) for details on each.

## Implementation

- **Go**, single static binary (`CGO_ENABLED=0`), pure-Go SQLite, releases for Linux, macOS and Windows on amd64 and arm64.
- **luanti-depgraph** stays in Python as the data producer. Luanthetti consumes its snapshot and ports only the resolver, guarded by a conformance test over the snapshot's precomputed rows and by versioned `schema_version` / `rules_version` fields in the snapshot's `meta` table.
- **Data directory** per OS (`LUANTHETTI_HOME` overrides): `config.toml`, `state.sqlite` (installations, links, pins, caches), `library/`, and a cache for the snapshot and thumbnails.
- **ContentDB etiquette**: descriptive User-Agent, at most 2 requests per second, cache by release id, bulk endpoints only.

## Roadmap

1. **0.1**: installs detect/add, install, remove, list, info, update, pin, rollback, scan, adopt, verify, repair, search with filters, web UI.
2. **0.2**: `env` mode polish, brew/scoop/deb/rpm packaging, non-ContentDB sources (git, local), update notifications.
3. **Later**: collections and shared installation profiles, backup of the library.

## References

- [research.md](research.md), this project's research notes.
- Luanti engine docs: `doc/lua_api.md` (mod load paths), `doc/world_format.md` (`world.mt`), `builtin/mainmenu/content/` (installer, update detector).
- [ContentDB API](https://content.luanti.org/help/api/).
- [luanti-depgraph](https://github.com/culmat/luanti-depgraph).
- [luma](https://github.com/luanti-org/luma), [luctl](https://github.com/brylie/luctl), [minetools](https://github.com/ronoaldo/minetools).
