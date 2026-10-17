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
copy. That happens on Windows without developer mode or administrator rights, and on some
network file systems.

`luanthetti installs add --link-mode junction` uses NTFS directory junctions on Windows. Automatic
mode never picks them; see [the Windows note](#windows-junctions).

## When another tool updates a linked package

The content browser updates a package by deleting its folder and copying the new release into
the same place (`pkgmgr.install_dir`: `core.delete_dir(targetpath)`, then `core.copy_dir`).
Since 5.10 or so, `core.delete_dir` uses `std::filesystem::remove_all`, which removes a symbolic
link without following it. The library is untouched, and the installation ends up with a real
folder holding the newer release. luma's update replaces the folder in the same way.

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

The content browser's own "Uninstall" on a linked package deletes the link only. Luanthetti then
reports the link as **missing**. `repair --relink` puts it back, or `remove` forgets it.

## Worlds

Which mods a world loads is set in its `world.mt` (`load_mod_<name> = true`). Luanthetti does not
edit `world.mt`. The main menu's "Configure" dialog does, and luma plans to. When you remove a
package that a world still enables, Luanthetti names the world, because Luanti will report
the mod as missing.

## Windows junctions

On Windows, a directory junction is a link that needs no special rights. Whether a recursive
delete follows a junction depends on the C++ standard library the engine was built with. If one
does, the content browser updating a junction-linked package would empty that release's folder
in the library. Automatic mode therefore uses a symbolic link, or else a copy.

Choose `--link-mode junction` only for installations that no other tool updates, such as a
server managed only by Luanthetti. `luanthetti verify` reports a library folder that went
missing; reinstalling the package restores it.

## Servers without links

For a server started from a script, `luanthetti env <installation>` prints a `LUANTI_MOD_PATH`
value and the `load_mod_<name> = /absolute/path` lines for `world.mt`. Luanti accepts absolute
`load_mod_` paths only inside folders listed in `LUANTI_MOD_PATH` (`doc/world_format.md`).
Together they run the server straight from the library, with nothing linked into the
installation.
