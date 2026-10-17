-- Applied to the database `ldg build --offline` makes from luanti-depgraph's test fixtures,
-- so the fixture covers what an offline build leaves empty: declared game support,
-- supports_all_games, dev_state, and an undeclared reference that makes a package "risky".
-- other/xcompat is declared NOT to support Mineclonia (website claim).
INSERT INTO game_support (package_id, game_id, supports, confidence)
  SELECT p.id, g.id, 0, 11 FROM package p, package g
  WHERE p.author='other' AND p.name='xcompat' AND g.name='mineclonia';
-- mt-mods/xcompat declares minetest_game and mineclonia in its .conf. It has no hard
-- dependencies (game-independent), so the declaration is the only signal and it does NOT
-- count in VoxeLibre: pipeworks becomes unsatisfiable there.
INSERT INTO game_support (package_id, game_id, supports, confidence)
  SELECT p.id, g.id, 1, 10 FROM package p, package g
  WHERE p.author='mt-mods' AND p.name='xcompat' AND g.name IN ('minetest_game', 'mineclonia');
-- ethereal: ContentDB detected support for minetest_game (confidence 1, not declared).
INSERT INTO game_support (package_id, game_id, supports, confidence)
  SELECT p.id, g.id, 1, 1 FROM package p, package g
  WHERE p.author='TenPlus1' AND p.name='ethereal' AND g.name='minetest_game';
UPDATE package SET supports_all_games = 1 WHERE author='mt-mods' AND name='pipeworks';
UPDATE package SET dev_state = 'ACTIVELY_DEVELOPED' WHERE author='mt-mods' AND name='pipeworks';
UPDATE package SET dev_state = 'MAINTENANCE_ONLY' WHERE author='TenPlus1' AND name='ethereal';
-- pipeworks' extra_part uses mcl_core's API without declaring it: risky in Minetest Game,
-- fine in Mineclonia and VoxeLibre.
INSERT INTO ref (mod_id, modname, kind, symbol, count, file, line, declared, transitive, guarded, requires)
  SELECT m.id, 'mcl_core', 'global', 'mcl_core.register_thing', 1, 'extra_part/init.lua', 3, 0, 0, 0, ''
  FROM mod m JOIN package p ON p.id = m.package_id WHERE p.name='pipeworks' AND m.name='extra_part';
-- An item-string reference never counts as risky.
INSERT INTO ref (mod_id, modname, kind, symbol, count, file, line, declared, transitive, guarded, requires)
  SELECT m.id, 'farming', 'itemstring', 'farming:wheat', 2, 'extra_part/init.lua', 9, 0, 0, 0, ''
  FROM mod m JOIN package p ON p.id = m.package_id WHERE p.name='pipeworks' AND m.name='extra_part';
