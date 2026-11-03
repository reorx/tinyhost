package server

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/token"
	"github.com/reorx/tinyhost/skill"
)

func consolePost(t *testing.T, f *fixture, c *http.Cookie, path, form string, status int) string {
	t.Helper()
	w := authReq(f, "admin.test", "POST", path, form, c)
	if w.Code != status {
		t.Fatalf("POST %s: %d want %d: %s", path, w.Code, status, w.Body.String())
	}
	return w.Body.String()
}
func consolePage(t *testing.T, f *fixture, c *http.Cookie, path string, contains ...string) string {
	t.Helper()
	w := authReq(f, "admin.test", "GET", path, "", c)
	if w.Code != 200 {
		t.Fatalf("GET %s: %d: %s", path, w.Code, w.Body.String())
	}
	for _, text := range contains {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatalf("GET %s missing %q: %s", path, text, w.Body.String())
		}
	}
	return w.Body.String()
}

// Behavior: renaming preserves main/version paths and queries across multiple renames and reserves every historical slug.
func TestRenameRedirectsOldPathsAndReservesOldSlug(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	consolePost(t, f, c, "/apps/demo/rename", "slug=renamed", 302)
	consolePost(t, f, c, "/apps/renamed/rename", "slug=final-name", 302)
	for _, p := range []string{"/demo/", "/demo@v1/app.js?q=%2F", "/renamed@v1/"} {
		status, _, h := f.request("GET", p, "", nil)
		want := strings.Replace(strings.Replace(p, "/demo", "/final-name", 1), "/renamed", "/final-name", 1)
		if status != 301 || h.Get("Location") != want {
			t.Fatal(p, status, h)
		}
	}
	f.content("/final-name/", "first")
	f.content("/final-name@v1/", "first")
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, archive(t, map[string]string{"index.html": "bad"}, false), 409)
	consolePost(t, f, c, "/apps", "slug=demo", 409)
	consolePost(t, f, c, "/apps", "slug=other-app", 302)
	consolePost(t, f, c, "/apps/final-name/rename", "slug=other-app", 409)
	consolePost(t, f, c, "/apps/final-name/rename", "slug=Bad--slug", 400)
	f.content("/final-name/", "first")
}

// Behavior: only non-current versions may be deleted; deleted URLs disappear and version numbers never get reused.
func TestDeleteVersionAndCurrentVersionIsProtected(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	f.deploy("", "second")
	consolePost(t, f, c, "/apps/demo/versions/2/delete", "", 409)
	consolePost(t, f, c, "/apps/demo/versions/1/delete", "", 302)
	f.want("GET", "/demo@v1/", "", nil, 404)
	f.content("/demo/", "second")
	consolePost(t, f, c, "/apps/demo/versions/1/delete", "", 404)
	consolePost(t, f, c, "/apps/demo/versions/no/delete", "", 400)
	if d := f.deploy("", "third"); d.Version != 3 {
		t.Fatal(d.Version)
	}
}

// Behavior: trash hides all content, restore preserves every version and alias, and purge permanently removes the app.
func TestRestoreFromTrash(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	f.deploy("", "second")
	consolePost(t, f, c, "/apps/demo/rename", "slug=restore-app", 302)
	a, err := f.s.Store.AppBySlug("restore-app")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	consolePost(t, f, c, "/trash/"+a.ID+"/purge", "", 404)
	consolePost(t, f, c, "/apps/restore-app/delete", "", 302)
	for _, p := range []string{"/restore-app/", "/restore-app@v1/", "/demo/"} {
		f.want("GET", p, "", nil, 404)
	}
	consolePage(t, f, c, "/trash", "restore-app", "7")
	consolePost(t, f, c, "/trash/"+a.ID+"/restore", "", 302)
	f.content("/restore-app/", "second")
	f.content("/restore-app@v1/", "first")
	if status, _, _ := f.request("GET", "/demo/", "", nil); status != 301 {
		t.Fatal(status)
	}
	consolePost(t, f, c, "/apps/restore-app/delete", "", 302)
	consolePost(t, f, c, "/trash/"+a.ID+"/purge", "", 302)
	consolePost(t, f, c, "/trash/"+a.ID+"/restore", "", 404)
	f.want("GET", "/restore-app@v1/", "", nil, 404)
	consolePost(t, f, c, "/apps", "slug=demo", 302)
}

// Behavior: audit pages include owner and Agent operations, filter by stable app ID, and show app history on detail pages.
func TestEventsRecordedForOwnerAndAgentActions(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	var callbacks []Event
	f.s.OnEvent = func(e Event) { callbacks = append(callbacks, e) }
	f.deploy("?create=1&visibility=public", "first")
	f.push("data/", "data")
	f.want("POST", "/_/api/v1/apps/demo/promote", f.key, []byte(`{"version":1}`), 200)
	consolePost(t, f, c, "/apps/demo/promote", "version=1", 302)
	consolePost(t, f, c, "/apps/demo/visibility", "visibility=private", 302)
	f.want("GET", "/demo/", "", nil, 401)
	consolePost(t, f, c, "/apps/demo/visibility", "visibility=public", 302)
	consolePost(t, f, c, "/apps/demo/rename", "slug=events-app", 302)
	consolePost(t, f, c, "/apps/events-app/versions/2/delete", "", 302)
	a, _ := f.s.Store.AppBySlug("events-app")
	consolePage(t, f, c, "/apps/events-app", "app.deployed", "app.visibility_changed")
	consolePost(t, f, c, "/apps/events-app/delete", "", 302)
	consolePost(t, f, c, "/trash/"+a.ID+"/restore", "", 302)
	consolePost(t, f, c, "/apps", "slug=unrelated", 302)
	d := device(t, f)
	approve(t, f, d, "30d")
	body := consolePost(t, f, c, "/agents", "name=manual-agent&ttl=1d", 200)
	re := regexp.MustCompile(`th_[A-Za-z0-9_-]{43}`)
	key := re.FindString(body)
	if key == "" {
		t.Fatal(body)
	}
	stored, _ := f.s.Store.ClientByTokenHash(token.Hash(key))
	if stored == nil {
		t.Fatal("manual token not stored")
	}
	consolePost(t, f, c, "/agents/"+stored.ID+"/revoke", "", 302)
	f.want("POST", "/_/api/v1/apps/agent-trash/deploys?create=1", f.key, archive(t, map[string]string{"index.html": "trash"}, false), 201)
	f.want("DELETE", "/_/api/v1/apps/agent-trash", f.key, nil, 200)
	consolePage(t, f, c, "/events", "app.created", "app.deployed", "data.pushed", "app.promoted", "app.deleted", "agent.approved", "agent.revoked", "app.visibility_changed", "owner")
	filtered := consolePage(t, f, c, "/events?app="+url.QueryEscape(a.ID), "app.restored", "app.renamed", "app.version_deleted")
	_, audit, _ := strings.Cut(filtered, "<table>")
	if strings.Contains(audit, "unrelated") || strings.Contains(audit, "agent.approved") {
		t.Fatal(filtered)
	}
	agentActions := map[string]bool{}
	for _, e := range callbacks {
		if strings.HasPrefix(e.Action, "app.") && e.Actor == "owner" && e.AppID == "" {
			t.Fatal(e)
		}
		if e.Actor != "owner" {
			agentActions[e.Action] = true
		}
	}
	for _, action := range []string{"app.deployed", "data.pushed", "app.promoted", "app.deleted"} {
		if !agentActions[action] {
			t.Fatal("missing Agent audit", action)
		}
	}
}

// Behavior: console creates default-private empty apps, lists live requests, exposes version/overlay metadata, and removes overlays as a new version.
func TestConsolePagesAndOverlayRemoval(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	consolePost(t, f, c, "/apps", "slug=empty-app&title=Empty", 302)
	a, _ := f.s.Store.AppBySlug("empty-app")
	if a == nil || a.CreatedBy != "owner" || a.Visibility != "private" || a.CurrentVersion != nil {
		t.Fatal(a)
	}
	d := device(t, f)
	consolePage(t, f, c, "/", "empty-app", "Empty", strings.ReplaceAll(d.UserCode, "-", ""))
	f.now = f.now.Add(601 * time.Second)
	body := consolePage(t, f, c, "/")
	if strings.Contains(body, strings.ReplaceAll(d.UserCode, "-", "")) {
		t.Fatal("expired request listed")
	}
	f.deploy("?create=1&visibility=public", "first")
	f.push("data/", "data")
	consolePage(t, f, c, "/apps/demo", "data/", "/data/remove", "/demo@v1/", "/demo@v2/")
	consolePost(t, f, c, "/apps/demo/data/remove", "dir=data%2F", 302)
	f.content("/demo/data/old.json", "old")
	consolePost(t, f, c, "/apps/demo/data/remove", "dir=data%2F", 404)
	for _, form := range []string{"slug=Bad", "slug=valid-app&visibility=invalid"} {
		consolePost(t, f, c, "/apps", form, 400)
	}
	consolePost(t, f, c, "/apps/demo/visibility", "visibility=invalid", 400)
	consolePost(t, f, c, "/apps/demo/promote", "version=0", 400)
	consolePage(t, f, c, "/events", "data.removed")
}

// Behavior: manual tokens are only rendered once, enforce expiry, and revoked or expired agents show accurate statuses.
func TestConsoleManualTokenAndAgentStates(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	body := consolePost(t, f, c, "/agents", "name=manual&ttl=1d", 200)
	key := regexp.MustCompile(`th_[A-Za-z0-9_-]{43}`).FindString(body)
	if key == "" {
		t.Fatal(body)
	}
	var who api.WhoamiResult
	json.Unmarshal(f.want("GET", "/_/api/v1/whoami", key, nil, 200), &who)
	if who.Client.ExpiresAt == nil || *who.Client.ExpiresAt != f.now.Unix()+86400 {
		t.Fatal(who)
	}
	page := consolePage(t, f, c, "/agents", "manual", "有效")
	if strings.Contains(page, key) || strings.Contains(page, token.Hash(key)) {
		t.Fatal("token leaked")
	}
	f.now = f.now.Add(86400 * time.Second)
	consolePage(t, f, c, "/agents", "已过期")
	f.want("GET", "/_/api/v1/whoami", key, nil, 401)
	consolePost(t, f, c, "/agents/"+who.Client.ID+"/revoke", "", 302)
	consolePage(t, f, c, "/agents", "已撤销")
	consolePost(t, f, c, "/agents", "name=x&ttl=bad", 400)
	consolePost(t, f, c, "/agents", "name=&ttl=1d", 400)
	consolePost(t, f, c, "/agents/missing/revoke", "", 404)
}

// Behavior: the Agents page offers a quickstart panel with the instance's real apps address in both the default and the named text.
func TestAgentsPageHasQuickstartPanel(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	page := consolePage(t, f, c, "/agents",
		html.EscapeString(skill.Quickstart("http://apps.test", "")),
		html.EscapeString(skill.Quickstart("http://apps.test", skill.QuickstartNamePlaceholder)),
		`data-placeholder="`+skill.QuickstartNamePlaceholder+`"`,
		"http://apps.test/install.sh",
	)
	if strings.Contains(page, "example.com") || strings.Contains(page, "style=") || strings.Contains(page, "<style") {
		t.Fatal(page)
	}
	if strings.Index(page, "quickstart-title") > strings.Index(page, "issue-title") {
		t.Fatal("quickstart panel must come before the manual token panel")
	}
}

// Behavior: after issuing a token by hand, the one-time token and the quickstart panel are on the same page.
func TestIssuedTokenPageKeepsQuickstartPanel(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	body := consolePost(t, f, c, "/agents", "name=manual&ttl=1d", 200)
	if !regexp.MustCompile(`th_[A-Za-z0-9_-]{43}`).MatchString(body) {
		t.Fatal(body)
	}
	if !strings.Contains(body, html.EscapeString(skill.Quickstart("http://apps.test", ""))) {
		t.Fatal(body)
	}
}

// Behavior: the dev-build warning shows only while the server has no local install packages.
func TestAgentsPageWarnsWhenInstallUnavailable(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	const warning = "data-install-unavailable"
	if page := consolePage(t, f, c, "/agents"); !strings.Contains(page, warning) {
		t.Fatal("dev build without DLDir must warn")
	}
	f.s.Config.DLDir = t.TempDir()
	if page := consolePage(t, f, c, "/agents"); strings.Contains(page, warning) {
		t.Fatal("DLDir configured, warning must go away")
	}
}
