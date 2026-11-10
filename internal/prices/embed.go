package prices

import _ "embed"

//go:generate go run gen.go

// api.json is a trimmed models.dev snapshot (see Trim), refreshed at runtime.
//
//go:embed api.json
var APIJSON []byte

//go:embed cursor.json
var CursorJSON []byte

//go:embed opencode.json
var OpenCodeJSON []byte
