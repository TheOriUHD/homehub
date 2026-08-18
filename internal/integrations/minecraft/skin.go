package minecraft

import _ "embed"

//go:embed steve.png
var defaultSkin []byte

// DefaultSkin is the classic wide skin, served when a player's real skin
// cannot be fetched (offline-mode uuid or Mojang unreachable).
func DefaultSkin() []byte { return defaultSkin }
