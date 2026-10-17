package depgraph

// MTGMods are Minetest Game's mod names. From ContentDB app/domain/game_support.py, as in ldg.
var MTGMods = NewSet(
	"beds", "boats", "bucket", "carts", "default", "dungeon_loot", "env_sounds", "fire", "flowers",
	"give_initial_stuff", "map", "player_api", "sethome", "spawn", "tnt", "walls", "wool",
	"binoculars", "bones", "butterflies", "creative", "doors", "dye", "farming", "fireflies",
	"game_commands", "keys", "mtg_craftguide", "screwdriver", "sfinv", "stairs", "vessels",
	"weather", "xpanes",
)

// GamesWithNonstandardMTGCopies bundle copies of Minetest Game mods that don't implement the MTG API.
var GamesWithNonstandardMTGCopies = NewSet(
	"pacman", "tutorial", "runorfall", "realtest_mt5", "mevo", "xaenvironment",
	"survivethedays", "holidayhorrors",
)

// ReferenceGames come first in DefaultGames.
var ReferenceGames = []string{"minetest_game", "mineclonia", "mineclone2"}
