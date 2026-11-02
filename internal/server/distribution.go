package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/reorx/tinyhost/internal/distribution"
	"github.com/reorx/tinyhost/internal/version"
	"github.com/reorx/tinyhost/skill"
)

type RootView struct{ AppsURL, Version, InstallCommand string }

// trustedAppsPageCSP applies to server-generated pages on the apps origin.
const trustedAppsPageCSP = "default-src 'self'; frame-ancestors 'none'; form-action 'none'"

func (s *Server) distribution(w http.ResponseWriter, r *http.Request, downloads *distribution.Proxy) bool {
	if strings.HasPrefix(r.URL.Path, "/_/static/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return true
		}
		static, _ := fs.Sub(viewFiles, "views/static")
		http.StripPrefix("/_/static/", http.FileServer(http.FS(static))).ServeHTTP(w, r)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/_/dl/") {
		downloads.ServeHTTP(w, r)
		return true
	}
	if r.URL.Path != "/" && r.URL.Path != "/skill.md" && r.URL.Path != "/install.sh" {
		return false
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(405)
		return true
	}
	w.Header().Set("Cache-Control", "no-cache")
	switch r.URL.Path {
	case "/":
		w.Header().Set("Content-Security-Policy", trustedAppsPageCSP)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			s.render(w, "root.html", RootView{AppsURL: strings.TrimRight(s.Config.AppsURL, "/"), Version: version.Version, InstallCommand: "curl -fsSL " + strings.TrimRight(s.Config.AppsURL, "/") + "/install.sh | sh"})
		}
	case "/skill.md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte(skill.Render(s.Config.AppsURL)))
		}
	case "/install.sh":
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte(skill.RenderInstall(s.Config.AppsURL, version.Version, s.Config.DLDir != "")))
		}
	}
	return true
}
