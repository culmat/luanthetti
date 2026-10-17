# Command line

Generated from the command definitions by `go run ./tools/gendocs > docs/cli.md`; don't edit by hand.

    luanthetti [global flags] <command> [flags] [arguments]

Every command accepts `--json` and then prints the same report the web UI's API returns.
Exit codes: 0 success, 1 error, 2 usage error (unknown command or flag, wrong arguments).

## Global flags

| flag | meaning |
|---|---|
| `--contentdb-url` | ContentDB base URL (default https://content.luanti.org) |
| `-n`, `--dry-run` | show what would be done without changing anything |
| `--home` | data directory (default: per-OS location; env LUANTHETTI_HOME) |
| `--json` | print results as JSON |
| `--offline` | use only cached data; no network |
| `-v`, `--verbose` | more output on stderr |
| `-y`, `--yes` | answer yes to confirmations |


## adopt

Move installed ContentDB packages into the library and link them back.

Adopt takes folders that carry author and release in their conf (installed by the in-game
client or luma), moves them into the library and puts a link in their place. Folders without
that identity, and git checkouts, are left alone.

    luanthetti adopt [installation] [folder|author/name]...

| flag | meaning |
|---|---|
| `--copy` | leave the folder in place and register it as a copy |
| `--force` | also adopt folders that replaced links, and git checkouts with a release |


## doctor

Check paths, the dependency snapshot, installations and ContentDB.

    luanthetti doctor


## env

Print LUANTI_MOD_PATH and world.mt lines to run a server from the library without links.

    luanthetti env [installation]


## gc

Delete unreferenced old releases, interrupted downloads and old trash.

    luanthetti gc


## info

Show a package: details, releases, compatibility with your games, where it is installed.

    luanthetti info <author/name|name>


## install

Install packages into every registered installation where they work (or --into).

Install downloads each package once into the library and links it into the selected
installations (all by default). Each package is resolved against every game of each
installation; hard dependencies are installed too, after one confirmation.

    luanthetti install <author/name|name|path>...

| flag | meaning |
|---|---|
| `--force` | install even where no game can run it, or over an unmanaged folder (moved to the trash) |
| `--into` | installation name(s); default: all registered |
| `--no-deps` | don't install dependencies |
| `--release` | a specific ContentDB release id |


## installs

Register and list Luanti installations.

    luanthetti installs

Aliases: installations.


## installs add

Register a Luanti installation (user path, server root or RUN_IN_PLACE build).

    luanthetti installs add <path>

| flag | meaning |
|---|---|
| `--create` | create mods/, games/, textures/ and worlds/ if the folder is not an installation yet |
| `--engine-version` | Luanti version, when there is no binary to ask (e.g. 5.17.0) |
| `--kind` | user \| server \| run_in_place \| flatpak \| docker |
| `--link-mode` | auto \| symlink \| junction \| copy \| env |
| `--name` | name to refer to it (default: desktop for a user path, else the folder name) |


## installs detect

Find Luanti installations on this machine.

    luanthetti installs detect

| flag | meaning |
|---|---|
| `--add` | register the installations found |


## installs games

List the games of an installation and how they map to ContentDB.

    luanthetti installs games <name>


## installs list

List registered installations and their games.

    luanthetti installs list


## installs remove

Forget an installation (its folders stay as they are unless --unlink).

    luanthetti installs remove <name>

| flag | meaning |
|---|---|
| `--unlink` | remove every link Luanthetti placed in it first |


## list

List installed packages per installation (or the library).

    luanthetti list

Aliases: ls.

| flag | meaning |
|---|---|
| `--into` | installation name(s) |
| `--library` | list the library's releases instead |
| `--names` | print only package names |
| `--outdated` | only packages with a newer release on ContentDB |
| `--type` | mod \| game \| txp |


## pin

Keep a package at its current release (or --release) in installations.

    luanthetti pin <author/name|name>

| flag | meaning |
|---|---|
| `--into` | installation name(s); default: all where it is installed |
| `--release` | pin to this release id instead of the current one |


## refresh

Download a newer dependency snapshot and refresh the update lists.

    luanthetti refresh


## remove

Remove packages from installations (and the library when nothing links them).

    luanthetti remove <author/name|name>...

Aliases: uninstall, rm.

| flag | meaning |
|---|---|
| `--force` | remove even if other installed packages need it |
| `--into` | installation name(s); default: all registered |
| `--keep-library` | keep the downloaded releases in the library |


## repair

Fix what verify finds: --relink broken links, --readopt replaced folders.

    luanthetti repair

| flag | meaning |
|---|---|
| `--into` | installation name(s) |
| `--readopt` | bring folders that replaced links back into the library |
| `--relink` | recreate missing, broken or wrong links; finish interrupted updates |


## rollback

Switch a package back to the previous release kept in the library (and pin it).

    luanthetti rollback <author/name|name>

| flag | meaning |
|---|---|
| `--into` | installation name(s) |
| `--to` | a specific release id kept in the library |


## scan

Show what is in an installation: managed, adoptable, unmanaged, replaced, broken.

    luanthetti scan [installation]


## search

Find packages, with filters and compatibility marks for your games.

Search the dependency snapshot (works offline). Compatibility marks are shown for the
games of your installations: ok, ~ (risky: uses another mod's API without declaring it),
x (unsatisfiable), ? (unknown).

    luanthetti search [text]

| flag | meaning |
|---|---|
| `--compatible-with` | installation or game name(s): only packages that work there |
| `--dev-state` | wip, beta, actively_developed, maintenance_only, as_is, deprecated, looking_for_maintainer |
| `--limit` | maximum results (default `30`) |
| `--min-downloads` | at least this many downloads |
| `--min-score` | at least this ContentDB score |
| `--sort` | score \| downloads \| updated \| name (default `score`) |
| `--tag` | ContentDB tag(s), all must match (e.g. tools, building) |
| `--type` | mod \| game \| txp |
| `--updated-within` | latest release within this period (e.g. 6m, 1y, 90d) |


## ui

Open the web UI (served from this binary on localhost).

ui serves Luanthetti's browser interface on 127.0.0.1 and opens it. The URL carries a
per-run access token; other local programs and web pages cannot use the API without it.
Stop it with Ctrl+C.

    luanthetti ui

| flag | meaning |
|---|---|
| `--no-open` | don't open a browser |
| `--port` | port on 127.0.0.1 (0 picks a free one) (default `7331`) |
| `--print-url` | print only the URL |


## unpin

Let update move a pinned package again.

    luanthetti unpin <author/name|name>

| flag | meaning |
|---|---|
| `--into` | installation name(s) |


## update

Update installed packages to their latest ContentDB release (--check only lists them).

    luanthetti update [author/name|name]...

| flag | meaning |
|---|---|
| `--check` | only list outdated packages |
| `-x`, `--exclude` | package or folder names to skip |
| `--into` | installation name(s); default: all registered |
| `--no-deps` | don't install new dependencies |


## verify

Check links, library versions, replaced folders and missing dependencies.

    luanthetti verify

| flag | meaning |
|---|---|
| `--into` | installation name(s) |


## version

Print the luanthetti version.

    luanthetti version

