# Coexistence with Luanti's content browser and luma

Luanthetti shares installations with two other tools: the content browser built into Luanti's
main menu, and [luma](https://github.com/luanti-org/luma). This page explains how each sees the
others' work, and what happens when they touch the same folder. Engine facts are from the
Luanti 5.17 source: `builtin/mainmenu/content/` (`pkgmgr.lua`, `contentdb.lua`,
`update_detector.lua`) and `src/filesys.cpp`.

## One identity for a package

Luanti recognises a package installed from ContentDB by two keys in its `.conf`
(`mod.conf`, `modpack.conf`, `game.conf` or `texture_pack.conf`):

- `author`, the ContentDB user name;
- `release`, the ContentDB release id (greater than 0).

The identity is `lower(author)/name` (for games `lower(author)/<game id>`; see
`pkgmgr.get_contentdb_id`). After extracting a release, the content browser writes `title` and
`description` (only when absent), `name` (not for games), `author` and `release`.

Luanthetti writes exactly these keys, in place, keeping every other line of the file. So:

- Luanti's content browser shows a package placed by Luanthetti as installed, with its release,
  and offers updates for it;
- luma's `mods list` and `mods outdated` see it the same way;
- `luanthetti scan` sees packages installed by either tool as **adoptable**.

## Placing a package: symlinks

A package release lives once in Luanthetti's library. An installation gets a symbolic link in
`mods/`, `games/` or `textures/` that points at it. Luanti follows the link and loads the
package as if it were a normal folder. This was checked with a headless Luanti 5.17.0 server,
which loaded mods linked this way.

Where symbolic links are not available, Luanthetti copies the release instead and records the
copy. That happens on some network file systems, and **on Windows always, unless you ask for
links**; see [Windows](#windows).

## When another tool updates a linked package

The content browser updates a package by deleting its folder and copying the new release into
the same place (`pkgmgr.install_dir`: `core.delete_dir(targetpath)`, then `core.copy_dir`).
On Linux and macOS, Luanti 5.17 deletes with `rm -rf <path>`, which removes a symbolic link
without following it (`src/filesys.cpp`, `RecursiveDelete`). The library is untouched, and the
installation ends up with a real folder holding the newer release. luma's update replaces the
folder in the same way; Go's `os.RemoveAll` does not follow links either.

Luanthetti notices this:

- `luanthetti verify` and `scan` report the path as **replaced**: a real folder where a link was
  recorded.
- `luanthetti repair --readopt`, or `adopt --force`, moves that folder into the library as a new
  version and puts a link back.
- `luanthetti update` refuses to touch a replaced path until it has been re-adopted, so it never
  overwrites another tool's newer release.

If you mostly update in-game, you can also leave such folders alone; they keep working.

## Removing

`luanthetti remove` removes only the links it placed (and copies it recorded). It refuses to
delete a real folder recorded as a link, and never deletes anything outside the installation.
Folders that other tools installed are not Luanthetti's to remove; adopt them first if you want
Luanthetti to manage them.

On Linux and macOS, the content browser's own "Uninstall" on a linked package deletes the link
only. Luanthetti then reports the link as **missing**. `repair --relink` puts it back, or
`remove` forgets it.

## Worlds

Which mods a world loads is set in its `world.mt` (`load_mod_<name> = true`). Luanthetti does not
edit `world.mt`. The main menu's "Configure" dialog does, and luma plans to. When you remove a
package that a world still enables, Luanthetti names the world, because Luanti will report
the mod as missing.

## Windows

Luanti 5.17.0 and older delete a folder on Windows by listing it and deleting every file inside,
following symbolic links and junctions (`src/filesys.cpp`, Windows `RecursiveDelete`: `IsDir`,
`GetDirListing`, `DeleteFile`). An in-game update or uninstall of a linked package would
therefore empty the release in Luanthetti's library, not just remove the link. The engine
replaced this with `std::filesystem::remove_all` on 2026-09-13
([luanti-org/luanti#17537](https://github.com/luanti-org/luanti/pull/17537)), which is not in
5.17.0.

So on Windows, Luanthetti copies releases into installations by default. Copies still come from
the one library, update together and can be rolled back; they only use more disk space. Choose
`installs add --link-mode symlink` (needs developer mode or administrator rights) or
`--link-mode junction` only for installations whose packages no other tool updates or
uninstalls, such as a server managed by Luanthetti alone, or once you run a Luanti release with
the fix. `luanthetti verify` reports a library release that went missing; reinstalling the
package restores it.

## Servers without links

For a server started from a script, `luanthetti env <installation>` prints a `LUANTI_MOD_PATH`
value and the `load_mod_<name> = /absolute/path` lines for `world.mt`. Luanti accepts absolute
`load_mod_` paths only inside folders listed in `LUANTI_MOD_PATH` (`doc/world_format.md`).
Together they run the server straight from the library, with nothing linked into the
installation.
