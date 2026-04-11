package stacks

import "embed"

// BuiltinFS contains the built-in Forge stack packs shipped with the CLI.
//
//go:embed */stack.json
var BuiltinFS embed.FS
