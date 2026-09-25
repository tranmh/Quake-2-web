package gametest

import (
	"quake2web/server/internal/game"
	"quake2web/server/internal/qcommon/crand"
)

// defaultNewGame creates the real game module.
func defaultNewGame(gi game.Import, rng *crand.Rand) game.Export { return game.New(gi, rng) }
