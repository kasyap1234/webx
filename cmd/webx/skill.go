package main

import _ "embed"

// skillMD is the SKILL.md emitted by `webx skill` — a real file in the repo
// (edit it like docs), embedded so the binary stays self-contained.
//
//go:embed SKILL.md
var skillMD string
