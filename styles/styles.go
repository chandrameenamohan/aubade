// Package styles carries aubade's built-in writing voice as data.
//
// default-voice.md is a product asset, not documentation: it is the base layer
// of the two-layer voice contract (SPEC §5) — the product's default drafting
// voice, overridden by the user's own profile.md tone rules wherever those
// speak. It is embedded rather than read from disk so a digest composed on a
// machine with no repo checked out still drafts in the same voice, and so there
// is exactly one copy of the file: the one a reader edits.
package styles

import _ "embed"

// DefaultVoice is styles/default-voice.md verbatim.
//
// Consumers parse it; nothing here interprets it, because the interpretation
// belongs next to the drafting code that has to answer for it
// (internal/digest/voice.go).
//
//go:embed default-voice.md
var DefaultVoice string

// DefaultVoicePath is how the file is cited in a digest, so a reader can go and
// read the rule that shaped a draft.
const DefaultVoicePath = "styles/default-voice.md"
