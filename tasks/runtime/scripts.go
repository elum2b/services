package runtime

import _ "embed"

//go:embed scripts/tgrass.lua
var TgrassScript string

//go:embed scripts/get_bonus.lua
var GetBonusScript string

//go:embed scripts/flyer.lua
var FlyerScript string

//go:embed scripts/subgram.lua
var SubGramScript string
