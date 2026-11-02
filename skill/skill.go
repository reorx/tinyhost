// Package skill embeds the Agent-facing instructions for CLI and HTTP reuse.
package skill

import _ "embed"

//go:embed SKILL.md
var Text string
