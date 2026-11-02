package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reorx/tinyhost/internal/version"
)

// Behavior: older release CLIs append one upgrade hint on stderr even on API errors, without contaminating JSON; dev builds stay quiet.
func TestOlderCLIPrintsUpgradeHintAtEnd(t *testing.T) {
	previous := version.Version
	defer func() { version.Version = previous }()
	serverVersion := "1.10.0"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tinyhost-Version", serverVersion)
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"code":"unauthorized","message":"bad token","hint":"login"}}`))
	}))
	defer remote.Close()
	t.Setenv("TINYHOST_TOKEN", "test")
	for _, tc := range []struct {
		local, remote string
		want          bool
	}{{"1.9.0", "1.10.0", true}, {"v1.9.0", "v1.10.0", true}, {"1.10.0", "1.9.0", false}, {"1.10.0", "1.10.0", false}, {"dev", "1.10.0", false}, {"1.0.0", "dev", false}, {"1.0.0", "bad", false}} {
		version.Version = tc.local
		serverVersion = tc.remote
		code, out, stderr := run("--server", remote.URL, "apps", "--json")
		if code != 3 || !strings.Contains(out, `"unauthorized"`) || strings.Contains(out, "curl") {
			t.Fatalf("%d %s", code, out)
		}
		command := "curl -fsSL " + remote.URL + "/install.sh | sh"
		if strings.Contains(stderr, command) != tc.want {
			t.Fatalf("%+v: %s", tc, stderr)
		}
		if tc.want && !strings.HasSuffix(stderr, command+"\n") {
			t.Fatal(stderr)
		}
	}
}
