// Package skill embeds the Agent-facing instructions and installer for CLI and HTTP reuse.
package skill

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/reorx/tinyhost/internal/version"
)

//go:embed SKILL.md
var Text string

//go:embed install.sh
var installer string

//go:embed quickstart.txt
var quickstart string

// QuickstartNamePlaceholder stands in for the Agent name in a Quickstart
// rendered for client-side substitution (see the admin Agents page).
const QuickstartNamePlaceholder = "__AGENT_NAME__"

func Render(server string) string {
	if server == "" {
		server = "https://<your-tinyhost-server>"
	}
	return strings.NewReplacer("{{SERVER}}", strings.TrimRight(server, "/"), "{{VERSION}}", version.Version).Replace(Text)
}

// InstallCommand is the documented way to install the CLI from server. curl
// retries CDN 52x and dropped connections; install.sh only runs once complete.
func InstallCommand(server string) string {
	return "curl -fsSL --retry 3 --retry-all-errors " + strings.TrimRight(server, "/") + "/install.sh | sh"
}

// Quickstart is the text an Owner pastes to an Agent in a fresh environment. An
// empty name asks the Agent to choose its own. name is placed inside double
// quotes in a shell command, so callers must only pass [A-Za-z0-9._@-].
func Quickstart(server, name string) string {
	server = strings.TrimRight(server, "/")
	pick := ""
	if name == "" {
		name = "<your-name>"
		pick = "First choose a short name that says who you are (for example your tool plus the machine, like codex-laptop). "
	}
	return strings.NewReplacer("{{SERVER}}", server, "{{INSTALL}}", InstallCommand(server), "{{PICK}}", pick, "{{NAME}}", name).Replace(quickstart)
}

// InstallAvailable reports whether install.sh can work: a dev build has no
// published packages unless the server was given a local package directory.
func InstallAvailable(build string, local bool) bool { return build != "dev" || local }

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func RenderInstall(server, build string, local bool) string {
	if !InstallAvailable(build, local) {
		return "#!/bin/sh\nprintf '%s\\n' '此服务器是开发版本，不提供安装'\nexit 1\n"
	}
	config, _ := json.Marshal(struct {
		Server string `json:"server"`
	}{strings.TrimRight(server, "/")})
	return strings.NewReplacer("{{SERVER}}", shellQuote(strings.TrimRight(server, "/")), "{{VERSION}}", shellQuote(strings.TrimPrefix(build, "v")), "{{CONFIG}}", shellQuote(string(config))).Replace(installer)
}
