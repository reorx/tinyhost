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
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func RenderInstall(server, build string, local bool) string {
	if build == "dev" && !local {
		return "#!/bin/sh\nprintf '%s\\n' '此服务器是开发版本，不提供安装'\nexit 1\n"
	}
	config, _ := json.Marshal(struct {
		Server string `json:"server"`
	}{strings.TrimRight(server, "/")})
	return strings.NewReplacer("{{SERVER}}", shellQuote(strings.TrimRight(server, "/")), "{{VERSION}}", shellQuote(strings.TrimPrefix(build, "v")), "{{CONFIG}}", shellQuote(string(config))).Replace(installer)
}
