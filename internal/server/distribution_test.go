package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/cli"
	"github.com/reorx/tinyhost/internal/version"
)

// Behavior: HTTP skill and the CLI render the same embedded instructions for the configured server.
func TestSkillServedMatchesCLIRender(t *testing.T) {
	f := setup(t)
	status, body, h := f.request("GET", "/skill.md", "", nil)
	t.Setenv("TINYHOST_SERVER", f.s.Config.AppsURL)
	var out, stderr bytes.Buffer
	if cli.Run([]string{"skill"}, &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if status != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/markdown") || string(body) != out.String() {
		t.Fatalf("%d %s\nHTTP %s\nCLI %s", status, h, body, out.String())
	}
	for _, text := range []string{"name: tinyhost", f.s.Config.AppsURL, "--ttl", "--new", "data push"} {
		if !strings.Contains(string(body), text) {
			t.Errorf("skill marker %q", text)
		}
	}
	if strings.Contains(string(body), "{{SERVER}}") {
		t.Fatal("unrendered server placeholder")
	}
}

// Behavior: all Agent API responses carry the build version, including auth failures, unknown paths and wrong methods.
func TestAgentAPIAlwaysReportsVersion(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct{ method, path string }{{"GET", "/_/api/v1/apps"}, {"GET", "/_/api/v1/unknown"}, {"PATCH", "/_/api/v1/apps"}, {"POST", "/_/api/v1/device/code"}} {
		_, _, h := f.request(tc.method, tc.path, "", nil)
		if h.Get("X-Tinyhost-Version") != version.Version {
			t.Fatal(tc, h)
		}
	}
}

// Behavior: the service root explains installation and skill discovery without serving user content.
func TestRootExplainsInstallationAndSkill(t *testing.T) {
	f := setup(t)
	status, body, h := f.request("GET", "/", "", nil)
	if status != 200 || !strings.Contains(h.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", status, body)
	}
	for _, s := range []string{"tinyhost", f.s.Config.AppsURL + "/install.sh", "tinyhost skill", "/skill.md"} {
		if !strings.Contains(string(body), s) {
			t.Fatal(s, string(body))
		}
	}
	_, body, _ = f.request("HEAD", "/", "", nil)
	if len(body) != 0 {
		t.Fatal("HEAD body")
	}
}

// Behavior: admin input, conflict, not-found and rate-limit errors preserve statuses and render safe navigable HTML.
func TestAdminErrorsRenderTemplateAndKeepStatus(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1", "first")
	for _, tc := range []struct {
		path, form, text string
		status           int
	}{
		{"/apps", "slug=demo", "slug 已被占用", 409},
		{"/apps/demo/rename", "slug=Bad--slug", "", 400},
		{"/apps/demo/versions/99/delete", "", "", 404},
		{"/device/missing/approve", "name=", "需要名称", 400},
	} {
		w := authReq(f, "admin.test", "POST", tc.path, tc.form, c)
		if w.Code != tc.status || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), `data-error-page`) || !strings.Contains(w.Body.String(), tc.text) || !strings.Contains(w.Body.String(), `href="/"`) {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "<style") || strings.Contains(w.Body.String(), "style=") {
			t.Fatal("inline CSS")
		}
	}
	for range 6 {
		w := authReq(f, "admin.test", http.MethodPost, "/login", "password=bad")
		if w.Code == 429 {
			if !strings.Contains(w.Body.String(), "data-error-page") {
				t.Fatal(w.Body.String())
			}
			return
		}
	}
	t.Fatal("expected login rate limit")
}

// Behavior: missing apps in the admin console use HTML errors while the Agent API retains its JSON contract.
func TestAdminMissingAppUsesHTMLError(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	w := authReq(f, "admin.test", "GET", "/apps/missing-app", "", c)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "data-error-page") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	f.want("GET", "/_/api/v1/apps/missing-app", f.key, nil, 404)
}

// Behavior: trusted embedded styles are available on the apps origin so its root and expired-preview pages can obey the no-inline-style CSP.
func TestAppsSystemAssetsAreServedFromReservedPrefix(t *testing.T) {
	f := setup(t)
	status, body, h := f.request("GET", "/_/static/admin.css", "", nil)
	if status != 200 || !strings.Contains(h.Get("Content-Type"), "text/css") || len(body) == 0 {
		t.Fatalf("%d %s", status, body)
	}
	_, _, h = f.request("GET", "/", "", nil)
	if !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal(h)
	}
}

// Behavior: apps-origin system pages never show a form or password field and load trusted assets from /_/static/ at any preview depth.
func TestAppsSystemPagesHaveNoPasswordFieldAndUseReservedAssets(t *testing.T) {
	f := setup(t)
	p := f.preview(f.key, "key=site&ttl=1h", "one")
	root := f.want("GET", "/", "", nil, 200)
	f.now = time.Unix(p.ExpiresAt, 0)
	expired := f.want("GET", previewPath(p)+"deep/nested/page.html", "", nil, 410)
	for name, body := range map[string]string{"root": string(root), "expired": string(expired)} {
		for _, bad := range []string{"<form", "<input", "password", `"/static/`, "<style", "style="} {
			if strings.Contains(body, bad) {
				t.Fatalf("%s contains %q: %s", name, bad, body)
			}
		}
		for _, asset := range []string{`href="/_/static/admin.css"`, `src="/_/static/admin.js"`} {
			if !strings.Contains(body, asset) {
				t.Fatalf("%s missing %q: %s", name, asset, body)
			}
		}
	}
}
