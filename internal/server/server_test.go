package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
)

type fixture struct {
	t          *testing.T
	s          *Server
	http       *httptest.Server
	key, other string
	now        time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tinyhost.db"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := blob.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, key: token.New(), other: token.New(), now: time.Now()}
	for name, key := range map[string]string{"one": f.key, "two": f.other} {
		if _, err = db.CreateClient(name, token.Hash(key), nil); err != nil {
			t.Fatal(err)
		}
	}
	f.s = &Server{
		Store: db, Blobs: b,
		Config: Config{AppsURL: "http://apps.test", AdminURL: "http://admin.test"},
		Now:    func() time.Time { return f.now },
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	f.http = httptest.NewServer(f.s.Handler())
	t.Cleanup(func() {
		f.http.Close()
		db.Close()
	})
	return f
}

func archive(t *testing.T, files map[string]string, link bool) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		h := &tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if link {
			h.Typeflag = tar.TypeSymlink
			h.Linkname = "index.html"
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !link {
			if _, err := io.WriteString(tw, content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (f *fixture) request(method, p, key string, body []byte) (int, []byte, http.Header) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.http.URL+p, bytes.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Host = "apps.test"
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return res.StatusCode, data, res.Header
}
func (f *fixture) want(method, p, key string, body []byte, status int) []byte {
	f.t.Helper()
	got, data, _ := f.request(method, p, key, body)
	if got != status {
		f.t.Fatalf("%s %s: status %d want %d: %s", method, p, got, status, data)
	}
	return data
}
func (f *fixture) deploy(query, content string) api.DeployResult {
	f.t.Helper()
	payload := archive(f.t, map[string]string{
		"index.html": content, "app.js": "script", "data/old.json": "old",
	}, false)
	data := f.want("POST", "/_/api/v1/apps/demo/deploys"+query, f.key, payload, 201)
	var out api.DeployResult
	if err := json.Unmarshal(data, &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}
func (f *fixture) push(dir, content string) {
	f.t.Helper()
	f.want("POST", "/_/api/v1/apps/demo/data?dir="+dir, f.other, archive(f.t, map[string]string{"new.json": content}, false), 201)
}
func (f *fixture) content(p, want string) {
	f.t.Helper()
	data := f.want("GET", p, "", nil, 200)
	if string(data) != want {
		f.t.Fatalf("%s: got %q want %q", p, data, want)
	}
}
func (f *fixture) gc() {
	f.t.Helper()
	if err := f.s.GC(); err != nil {
		f.t.Fatal(err)
	}
}

// Behavior: creating an app returns working main and version links with relative resources.
func TestDeployCreateReturnsVersionAndMainURLs(t *testing.T) {
	f := setup(t)
	d := f.deploy("?create=1&visibility=public", "first")
	if d.Version != 1 || d.MainURL != "http://apps.test/demo/" || d.VersionURL != "http://apps.test/demo@v1/" {
		t.Fatalf("%+v", d)
	}
	f.content("/demo/", "first")
	f.content("/demo@v1/", "first")
	f.content("/demo/app.js", "script")
}

// Behavior: redeploy changes main while previous version content remains unchanged.
func TestRedeployMovesMainButOldVersionIsImmutable(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "first")
	f.deploy("", "second")
	f.content("/demo/", "second")
	f.content("/demo@v1/", "first")
}

// Behavior: staging preserves main until an explicit promotion.
func TestNoPromoteKeepsMainAndPromoteSwitchesIt(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.deploy("", "two")
	f.deploy("?promote=0", "three")
	f.content("/demo/", "two")
	f.want("POST", "/_/api/v1/apps/demo/promote", f.other, []byte(`{"version":3}`), 200)
	f.content("/demo/", "three")
	f.want("POST", "/_/api/v1/apps/demo/promote", f.key, []byte(`{"version":99}`), 404)
}

// Behavior: publishing without create requires an existing app.
func TestDeployToMissingAppRequiresCreate(t *testing.T) {
	f := setup(t)
	b := f.want("POST", "/_/api/v1/apps/demo/deploys", f.key, nil, 404)
	if !bytes.Contains(b, []byte("app_not_found")) {
		t.Fatal(string(b))
	}
}

// Behavior: taken slugs report their creator and creation timestamp.
func TestCreateOnTakenSlugReportsCreator(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1", "one")
	b := f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.other, nil, 409)
	if !bytes.Contains(b, []byte("created_by")) || !bytes.Contains(b, []byte("created_at")) {
		t.Fatal(string(b))
	}
}

// Behavior: another agent's overlay survives redeploying stale bundled data.
func TestDataPushOverridesBundleDirAndSurvivesRedeploy(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.push("data/", "new")
	f.deploy("", "two")
	f.content("/demo/data/new.json", "new")
	f.want("GET", "/demo/data/old.json", "", nil, 404)
}

// Behavior: nested overlay directories are rejected with the existing directory list.
func TestNestedOverlayIsRejected(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1", "one")
	f.push("data/", "new")
	b := f.want("POST", "/_/api/v1/apps/demo/data?dir=data/sales/", f.key, archive(t, map[string]string{"x": "x"}, false), 409)
	if !bytes.Contains(b, []byte("data/")) {
		t.Fatal(string(b))
	}
}

// Behavior: removing an overlay exposes original bundle content in a new version.
func TestDataRmRevealsBundleContent(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.push("data/", "new")
	f.want("DELETE", "/_/api/v1/apps/demo/data?dir=data/", f.other, nil, 201)
	f.content("/demo/data/old.json", "old")
	f.want("DELETE", "/_/api/v1/apps/demo/data?dir=data/", f.key, nil, 404)
}

// Behavior: only the creator can delete and deleted content immediately disappears.
func TestOnlyCreatorCanDelete(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.want("DELETE", "/_/api/v1/apps/demo", f.other, nil, 403)
	f.want("DELETE", "/_/api/v1/apps/demo", f.key, nil, 200)
	f.want("GET", "/demo/", "", nil, 404)
}

// Behavior: deleted slugs remain reserved for seven days then GC releases them.
func TestDeletedSlugIsReservedUntilPurge(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1", "one")
	f.want("DELETE", "/_/api/v1/apps/demo", f.key, nil, 200)
	f.now = f.now.Add(6 * 24 * time.Hour)
	f.gc()
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, nil, 409)
	f.now = f.now.Add(2 * 24 * time.Hour)
	f.gc()
	f.deploy("?create=1", "new")
}

// Behavior: raw curl uploads cannot expose hidden files or metadata.
func TestDotfilesAreNeverServed(t *testing.T) {
	f := setup(t)
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1&visibility=public", f.key, archive(t, map[string]string{
		"index.html": "ok", ".env": "secret", ".git/config": "secret",
		"tinyhost.json": "{}", ".well-known/test": "allowed",
	}, false), 201)
	for _, p := range []string{".env", ".git/config", "tinyhost.json"} {
		f.want("GET", "/demo/"+p, "", nil, 404)
	}
	f.content("/demo/.well-known/test", "allowed")
}

// Behavior: HTTP uploads containing traversal or links are rejected without creating apps.
func TestUnpackRejectsTraversalAndSymlinks(t *testing.T) {
	for _, link := range []bool{false, true} {
		f := setup(t)
		f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, archive(t, map[string]string{"../escape": "x"}, link), 400)
		a, err := f.s.Store.AppBySlug("demo")
		if err != nil || a != nil {
			t.Fatalf("app leaked: %v %v", a, err)
		}
	}
}

// Behavior: private content stays inaccessible even with a bearer token on content routes.
func TestPrivateAppShowsLoginRequired(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1", "secret")
	for _, p := range []string{"/demo/", "/demo@v1/"} {
		b := f.want("GET", p, f.key, nil, 401)
		if !bytes.Contains(b, []byte("需要登录")) || bytes.Contains(b, []byte("secret")) {
			t.Fatal(string(b))
		}
	}
}

// Behavior: GC retains twenty newest data versions, all app versions, and an older promoted version.
func TestGCKeepsLatestDataVersionsAndAllAppVersions(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.deploy("", "two")
	for i := 0; i < 25; i++ {
		f.push("data/", "new")
	}
	f.want("POST", "/_/api/v1/apps/demo/promote", f.key, []byte(`{"version":3}`), 200)
	f.gc()
	b := f.want("GET", "/_/api/v1/apps/demo/versions", f.key, nil, 200)
	var out api.VersionsResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Versions) != 23 {
		t.Fatalf("versions=%d", len(out.Versions))
	}
	f.want("GET", "/demo@v4/", "", nil, 404)
	f.content("/demo@v1/", "one")
	f.content("/demo@v3/", "two")
}

// Behavior: data pushed after rollback uses the promoted app bundle rather than the newest one.
func TestDataPushBuildsOnCurrentVersionAfterRollback(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "one")
	f.deploy("", "two")
	f.want("POST", "/_/api/v1/apps/demo/promote", f.key, []byte(`{"version":1}`), 200)
	f.push("data/", "new")
	f.content("/demo/", "one")
}

// Behavior: paths resolve exact files, html extensions, directory redirects, and app 404s without overlay fallback.
func TestPathResolution(t *testing.T) {
	f := setup(t)
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1&visibility=public", f.key, archive(t, map[string]string{
		"index.html": "home", "about.html": "about", "docs/index.html": "docs",
		"404.html": "custom", "data/old.html": "old",
	}, false), 201)
	f.push("data/", "new")
	for _, tc := range []struct {
		p        string
		status   int
		body     string
		location string
	}{
		{p: "/demo/about", status: 200, body: "about"},
		{p: "/demo/docs", status: 301, location: "/demo/docs/"},
		{p: "/demo/docs/", status: 200, body: "docs"},
		{p: "/demo/missing", status: 404, body: "custom"},
		{p: "/demo/data/old", status: 404, body: "custom"},
		{p: "/demo", status: 301, location: "/demo/"},
	} {
		t.Run(tc.p, func(t *testing.T) {
			status, b, h := f.request("GET", tc.p, "", nil)
			if status != tc.status || (tc.body != "" && string(b) != tc.body) || (tc.location != "" && h.Get("Location") != tc.location) {
				t.Fatalf("%d %q %v", status, b, h)
			}
		})
	}
}

// Behavior: revoked, expired, absent and cookie-only credentials never authorize API requests.
func TestRevokedOrExpiredTokenIsRejected(t *testing.T) {
	f := setup(t)
	c, err := f.s.Store.ClientByTokenHash(token.Hash(f.key))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Store.RevokeClient(c.ID); err != nil {
		t.Fatal(err)
	}
	expiry := f.now.Unix() - 1
	key := token.New()
	if _, err = f.s.Store.CreateClient("expired", token.Hash(key), &expiry); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{f.key, key, "", "bad"} {
		f.want("GET", "/_/api/v1/whoami", k, nil, 401)
	}
	req, _ := http.NewRequest("DELETE", f.http.URL+"/_/api/v1/apps/demo", nil)
	req.Host = "apps.test"
	req.AddCookie(&http.Cookie{Name: "__Host-th_view", Value: f.other})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
}

// Behavior: freshly reused unreferenced blobs survive GC while old unused blobs are removed.
func TestGCDoesNotDeleteFreshlyReusedBlob(t *testing.T) {
	f := setup(t)
	hash, _, err := f.s.Blobs.Put(strings.NewReader("reuse"))
	if err != nil {
		t.Fatal(err)
	}
	oldHash, _, err := f.s.Blobs.Put(strings.NewReader("old"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := f.s.Blobs.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	file.Close()
	old := time.Now().Add(-2 * time.Hour)
	if err = os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	oldFile, err := f.s.Blobs.Open(oldHash)
	if err != nil {
		t.Fatal(err)
	}
	oldName := oldFile.Name()
	oldFile.Close()
	if err = os.Chtimes(oldName, old, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.Blobs.Put(strings.NewReader("reuse")); err != nil {
		t.Fatal(err)
	}
	f.gc()
	file, err = f.s.Blobs.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = f.s.Blobs.Open(oldHash); !os.IsNotExist(err) {
		t.Fatalf("old blob: %v", err)
	}
}

// Behavior: content supports cache validation, range and HEAD with secure headers.
func TestContentHeadersAndConditionalRequests(t *testing.T) {
	f := setup(t)
	f.deploy("?create=1&visibility=public", "hello")
	_, _, h := f.request("GET", "/demo/", "", nil)
	if h.Get("Cache-Control") != "no-cache" || h.Get("ETag") == "" || h.Get("Service-Worker-Allowed") != "" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(h)
	}
	_, _, h = f.request("GET", "/demo@v1/", "", nil)
	if !strings.Contains(h.Get("Cache-Control"), "immutable") {
		t.Fatal(h)
	}
	for _, tc := range []struct {
		method, header, value string
		status                int
		body                  string
	}{
		{"GET", "If-None-Match", h.Get("ETag"), 304, ""}, {"GET", "Range", "bytes=1-3", 206, "ell"}, {"HEAD", "", "", 200, ""},
	} {
		req, _ := http.NewRequest(tc.method, f.http.URL+"/demo@v1/", nil)
		req.Host = "apps.test"
		if tc.header != "" {
			req.Header.Set(tc.header, tc.value)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != tc.status || string(b) != tc.body {
			t.Fatalf("%d %q %v", res.StatusCode, b, err)
		}
	}
}

// Behavior: simultaneous fifty MiB uploads commit successfully without SQLite busy errors.
func TestConcurrent50MBUploads(t *testing.T) {
	if testing.Short() {
		t.Skip("large upload")
	}
	f := setup(t)
	f.deploy("?create=1&visibility=public", "first")
	content := make([]byte, 50<<20)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	payload := archive(t, map[string]string{"index.html": string(content)}, false)
	t.Logf("each uncompressed payload: %d bytes; each HTTP archive: %d bytes", len(content), len(payload))
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() { f.want("POST", "/_/api/v1/apps/demo/deploys", f.key, payload, 201) })
	}
	wg.Wait()
	t.Logf("two concurrent 50MiB uploads: %s, zero failures", time.Since(start))
}

// Behavior: Host separates admin and unknown origins, reserves system paths, and exposes only safe API DTOs.
func TestHostRoutingAndQueries(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		host   string
		status int
	}{{"admin.test", 200}, {"other.test", 421}} {
		req, err := http.NewRequest("GET", f.http.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("host %s: %d", tc.host, res.StatusCode)
		}
	}
	f.deploy("?create=1&visibility=public&title=Report", "home")
	f.push("data/", "new")
	for _, p := range []string{"whoami", "apps", "apps/demo", "apps/demo/versions"} {
		b := f.want("GET", "/_/api/v1/"+p, f.key, nil, 200)
		if bytes.Contains(b, []byte("token_hash")) || bytes.Contains(b, []byte(f.key)) {
			t.Fatal("secret in response")
		}
	}
	var detail api.AppDetail
	if err := json.Unmarshal(f.want("GET", "/_/api/v1/apps/demo", f.key, nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.App.Title != "Report" || len(detail.Overlays) != 1 {
		t.Fatalf("%+v", detail)
	}
	for _, p := range []string{"/_/secret", "/_p/token/", "/api/", "/demo/%2e%2e/index.html", "/demo/data%5cnew.json"} {
		f.want("GET", p, "", nil, 404)
	}
	f.want("GET", "/", "", nil, 200)
	f.want("GET", "/robots.txt", "", nil, 200)
}

// Behavior: empty apps can receive data and invalid uploads do not consume versions or slugs.
func TestUploadValidationAndEmptyApp(t *testing.T) {
	f := setup(t)
	c, err := f.s.Store.ClientByTokenHash(token.Hash(f.key))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Store.CreateApp("empty", "", "public", c.ID); err != nil {
		t.Fatal(err)
	}
	f.want("GET", "/empty/", "", nil, 404)
	f.want("POST", "/_/api/v1/apps/empty/data?dir=data/", f.key, archive(t, map[string]string{"x": "x"}, false), 201)
	f.content("/empty/data/x", "x")
	f.want("GET", "/empty/", "", nil, 404)
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, archive(t, map[string]string{"missing": "x"}, false), 400)
	f.want("POST", "/_/api/v1/apps/admin/deploys?create=1", f.key, nil, 400)
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1&visibility=bad", f.key, nil, 400)
	f.deploy("?create=1", "one")
	for _, dir := range []string{"/", ".hidden/", "data/../"} {
		f.want("POST", "/_/api/v1/apps/demo/data?dir="+dir, f.key, nil, 400)
	}
	for _, body := range []string{`{"version":1} {}`, `{`, `null`} {
		status, _, _ := f.request("POST", "/_/api/v1/apps/demo/promote", f.key, []byte(body))
		if status < 400 {
			t.Fatalf("accepted %s", body)
		}
	}
}

// Behavior: configured upload limits reject excessive bytes and entries before publishing.
func TestUploadLimits(t *testing.T) {
	f := setup(t)
	f.s.Config.MaxUploadMB = 1
	f.s.Config.MaxFiles = 1
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, archive(t, map[string]string{"index.html": strings.Repeat("x", (1<<20)+1)}, false), 413)
	f.want("POST", "/_/api/v1/apps/demo/deploys?create=1", f.key, archive(t, map[string]string{"index.html": "x", "extra": "x"}, false), 413)
	a, err := f.s.Store.AppBySlug("demo")
	if err != nil || a != nil {
		t.Fatalf("%v %v", a, err)
	}
}
