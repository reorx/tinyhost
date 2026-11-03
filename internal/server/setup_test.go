package server

import (
	"regexp"
	"strings"
	"testing"
)

const setupNotice = "tinyhost-server passwd"

// Behavior: before an owner password is set, the admin host serves the setup page's static assets so it renders styled, while every other path, including login, business pages and writes, still returns only the setup page.
func TestUninitializedAdminServesStaticAssetsAndOnlyTheSetupPage(t *testing.T) {
	f := setup(t)
	page := authReq(f, "admin.test", "GET", "/", "").Body.String()
	if !strings.Contains(page, setupNotice) {
		t.Fatal(page)
	}
	assets := regexp.MustCompile(`(?:href|src)="(/static/[^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(assets) < 3 {
		t.Fatalf("setup page assets: %v", assets)
	}
	for _, asset := range assets {
		want, err := viewFiles.ReadFile("views" + asset[1])
		if err != nil {
			t.Fatal(err)
		}
		w := authReq(f, "admin.test", "GET", asset[1], "")
		if w.Code != 200 || w.Body.String() != string(want) || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %s %.80q", asset[1], w.Code, w.Header().Get("Content-Type"), w.Body.String())
		}
	}
	if w := authReq(f, "admin.test", "HEAD", "/static/admin.css", ""); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("HEAD: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/login"}, {"GET", "/agents"}, {"GET", "/apps/demo"}, {"GET", "/events"},
		{"GET", "/staticx"}, {"GET", "/static/"}, {"GET", "/static/../agents"},
		{"POST", "/login"}, {"POST", "/apps"}, {"POST", "/static/admin.css"},
	} {
		w := authReq(f, "admin.test", tc.method, tc.path, "slug=demo&title=demo&password=x")
		if !strings.Contains(w.Body.String(), setupNotice) {
			t.Errorf("%s %s: %d %.200s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if apps, err := f.s.Store.ListApps(); err != nil || len(apps) != 0 {
		t.Fatalf("setup state created apps: %v %v", apps, err)
	}
}
