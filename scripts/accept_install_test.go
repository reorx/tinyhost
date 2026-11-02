package scripts

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeInstall installs a stand-in CLI that reports version 1.2.3, prints the
// same skill as the server and leaves a pending login, like the real CLI does.
const fakeInstall = `#!/bin/sh
set -eu
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/tinyhost" <<'EOF'
#!/bin/sh
case "$1" in
  version) echo 1.2.3 ;;
  skill) echo 'skill text' ;;
  login) printf '{\n  "status": "pending"\n}\n'; exit 4 ;;
esac
EOF
chmod +x "$HOME/.local/bin/tinyhost"
`

func acceptanceServer(serverVersion string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/install.sh":
			_, _ = w.Write([]byte(fakeInstall))
		case "/skill.md":
			_, _ = w.Write([]byte("skill text\n"))
		case "/_/api/v1/whoami":
			if serverVersion != "" {
				w.Header().Set("X-Tinyhost-Version", serverVersion)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func runAcceptance(t *testing.T, server string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "accept-install.sh", server, "1.2.3", filepath.Join(t.TempDir(), "accept"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Behavior: install acceptance passes when the installed CLI, the server's X-Tinyhost-Version header and the skill all match.
func TestInstallAcceptancePassesWhenCLIAndServerVersionsMatch(t *testing.T) {
	srv := acceptanceServer("1.2.3")
	defer srv.Close()
	out, err := runAcceptance(t, srv.URL)
	if err != nil {
		t.Fatalf("acceptance failed: %v\n%s", err, out)
	}
	for _, want := range []string{"PASS version=1.2.3", "PASS server X-Tinyhost-Version=1.2.3", "PASS skill HTTP/CLI identical", "PASS login created authorization"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// Behavior: install acceptance fails when the server reports a different or missing X-Tinyhost-Version, even if the CLI matches the expected version.
func TestInstallAcceptanceFailsWhenServerVersionHeaderDiffers(t *testing.T) {
	for _, header := range []string{"9.9.9", ""} {
		srv := acceptanceServer(header)
		out, err := runAcceptance(t, srv.URL)
		srv.Close()
		if err == nil {
			t.Fatalf("header %q: acceptance passed, want failure:\n%s", header, out)
		}
		if !strings.Contains(out, "服务器版本不一致") {
			t.Errorf("header %q: output lacks server version mismatch:\n%s", header, out)
		}
		if strings.Contains(out, "PASS login") {
			t.Errorf("header %q: acceptance continued to login after a mismatch:\n%s", header, out)
		}
	}
}
