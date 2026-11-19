package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/token"
)

const appPreviewEndpoint = "/_/api/v1/apps/demo/previews"

func (f *fixture) appPreview() string {
	f.t.Helper()
	body := archive(f.t, map[string]string{"index.html": "candidate"}, false)
	b := f.want("POST", appPreviewEndpoint+"?ttl=1h&message=review", f.key, body, 201)
	var p struct{ Hash string }
	if err := json.Unmarshal(b, &p); err != nil || len(p.Hash) != 12 {
		f.t.Fatal(string(b), err)
	}
	return p.Hash
}

// Behavior: preview content follows live overlays while publish and rollback produce immutable versions with provenance and real layer sizes.
func TestAppPreviewServesLiveDataAndPublishesReviewedBytes(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "original")
	hash := f.appPreview()
	path := "/demo@" + hash + "/"
	f.content(path, "candidate")
	f.content("/demo/", "original")
	f.push("data/", "updated")
	f.content(path+"data/new.json", "updated")
	if _, _, h := f.request("GET", path, "", nil); h.Get("Cache-Control") != "no-cache" {
		t.Fatal(h)
	}
	publish := appPreviewEndpoint + "/" + hash + "/publish"
	for _, body := range []string{`null`, `{} {}`, strings.Repeat(" ", 4097) + "{}"} {
		f.want("POST", publish, f.key, []byte(body), 400)
		f.want("POST", "/_/api/v1/apps/demo/rollback", f.key, []byte(body), 400)
	}
	b := f.want("POST", publish, f.other, nil, 201)
	var result api.DeployResult
	if err := json.Unmarshal(b, &result); err != nil || result.Version != 3 || result.Files != 1 || result.Size != 9 || !strings.Contains(string(b), `"preview":"`+hash+`"`) {
		t.Fatal(string(b), err)
	}
	f.content("/demo/", "candidate")
	f.content("/demo/data/new.json", "updated")
	f.want("POST", publish, f.key, nil, 409)
	b = f.want("POST", "/_/api/v1/apps/demo/rollback", f.key, []byte(`{"version":1,"message":"restore"}`), 201)
	if !strings.Contains(string(b), `"rollback_of":1`) {
		t.Fatal(string(b))
	}
	f.content("/demo/", "original")
	f.content("/demo/data/new.json", "updated")
	for _, p := range []string{"/demo@invalid/", "/demo@000000000000/"} {
		f.want("GET", p, "", nil, 404)
	}
	consolePost(t, f, adminCookie(t, f), "/apps/demo/rename", "slug=renamed", 302)
	if status, _, h := f.request("GET", path+"data/new.json?q=yes", "", nil); status != 301 || h.Get("Location") != "/renamed@"+hash+"/data/new.json?q=yes" {
		t.Fatal(status, h)
	}
}

// Behavior: private preview metadata stays behind viewer authorization, including expired links; GC eventually removes tombstones.
func TestPrivateAppPreviewExpiresBehindViewerLogin(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1", "private")
	hash := f.appPreview()
	path := "/demo@" + hash + "/"
	for _, p := range []string{path, "/demo@000000000000/"} {
		r := httptest.NewRequest("GET", "http://apps.test"+p, nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "/view-auth?next=") {
			t.Fatal(w.Code, w.Header())
		}
	}
	if err := f.s.Store.CreateSession(token.Hash("viewer"), "viewer", "", "", f.now.Unix()); err != nil {
		t.Fatal(err)
	}
	c := &http.Cookie{Name: "__Host-th_view", Value: "viewer"}
	w := authReq(f, "apps.test", "GET", path, "", c)
	if w.Code != 200 || w.Body.String() != "candidate" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	f.now = f.now.Add(time.Hour)
	f.gc()
	w = authReq(f, "apps.test", "GET", path, "", c)
	if w.Code != 410 || !strings.Contains(w.Body.String(), "预览已过期") || w.Header().Get("Content-Security-Policy") != trustedAppsPageCSP {
		t.Fatal(w.Code, w.Header())
	}
	f.want("POST", appPreviewEndpoint+"/"+hash+"/publish", f.other, nil, 410)
	f.now = f.now.Add(7 * 24 * time.Hour)
	f.gc()
	f.want("POST", appPreviewEndpoint+"/"+hash+"/publish", f.other, nil, 404)
	if w := authReq(f, "apps.test", "GET", path, "", c); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Behavior: Owner sees provenance and active previews and can confirm a rollback with version and audit attribution.
func TestOwnerCanReviewPreviewsAndRollBackPackages(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1", "original")
	hash := f.appPreview()
	f.want("POST", appPreviewEndpoint+"/"+hash+"/publish", f.key, nil, 201)
	consolePage(t, f, c, "/apps/demo", "来自预览 "+hash, "已发布为 v2", "回滚到此版本", "App 预览")
	consolePost(t, f, c, "/apps/demo/rollback", "version=1", 302)
	consolePage(t, f, c, "/apps/demo", "回滚自 v1")
	consolePage(t, f, c, "/events", "创建 App 预览", "发布预览", "回滚 App 包")
	consolePost(t, f, c, "/apps/demo/rollback", "version=1", 409)
}
