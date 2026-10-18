// Package domainpackassets embeds the domain packs: read-only skills with
// reference files that an agent opts into by name
// (docs/domain-pack-standard.md §3). Each pack is one directory here.
package domainpackassets

import "embed"

// FS holds every pack directory.
//
// The pattern is "*", not "*/SKILL.md */references/* …": go:embed refuses a
// pattern that matches nothing, there is no pack yet, and "*" always matches
// this file. It also means the whole pack tree ships, so the registry
// validates every file go:embed can see — a stray file fails startup instead
// of slipping in unseen. ATTRIBUTION.md and evals/ ship too but are never
// served to a model. Files go:embed skips (names starting with "." or "_")
// are caught by the validator test, which reads this directory from disk.
// Root-level files (this one) are not packs and are ignored.
//
//go:embed *
var FS embed.FS
