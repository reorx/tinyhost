package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reorx/tinyhost/internal/api"
)

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload posts to the console upload endpoint as the Owner and returns the status and body.
func upload(f *fixture, c *http.Cookie, slug, query, contentType string, body []byte) (int, []byte) {
	r := httptest.NewRequest("POST", "http://admin.test/apps/"+slug+"/upload"+query, bytes.NewReader(body))
	r.Host = "admin.test"
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Origin", "http://admin.test")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func uploadOK(t *testing.T, f *fixture, c *http.Cookie, query, contentType string, body []byte) api.DeployResult {
	t.Helper()
	status, data := upload(f, c, "demo", query, contentType, body)
	if status != 201 {
		t.Fatalf("upload: %d %s", status, data)
	}
	var out api.DeployResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func uploadError(t *testing.T, f *fixture, c *http.Cookie, slug, query, contentType string, body []byte, status int, code string) api.ErrorDetail {
	t.Helper()
	got, data := upload(f, c, slug, query, contentType, body)
	if got != status {
		t.Fatalf("upload: %d want %d: %s", got, status, data)
	}
	var out api.Error
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("not JSON: %s", data)
	}
	if out.Error.Code != code {
		t.Fatalf("code %q want %q: %s", out.Error.Code, code, data)
	}
	return out.Error
}

func versionCount(t *testing.T, f *fixture) int {
	t.Helper()
	a, err := f.s.Store.AppBySlug("demo")
	if err != nil || a == nil {
		t.Fatal(err)
	}
	v, err := f.s.Store.Versions(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return len(v)
}

// Behavior: a tar.gz upload from the Owner creates a version whose nested, Chinese and very long paths are all served.
func TestOwnerUploadsTarGzAndEveryFileIsServed(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	long := strings.Repeat("d", 60) + "/" + strings.Repeat("e", 60) + "/file.txt"
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	files := map[string]string{"index.html": "<p>up</p>", "css/a.css": "a", "img/中文.txt": "zh", long: "long", "empty.txt": ""}
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg, Format: tar.FormatPAX})
		io.WriteString(tw, content)
	}
	tw.Close()
	gz.Close()
	out := uploadOK(t, f, c, "", "application/gzip", b.Bytes())
	if out.Version != 2 || out.Files != 5 || !out.Promoted || out.VersionURL != "http://apps.test/demo@v2/" {
		t.Fatalf("%+v", out)
	}
	f.content("/demo@v2/", "<p>up</p>")
	f.content("/demo@v2/css/a.css", "a")
	f.content("/demo@v2/img/%E4%B8%AD%E6%96%87.txt", "zh")
	f.content("/demo@v2/"+long, "long")
	f.content("/demo@v2/empty.txt", "")
	f.content("/demo/", "<p>up</p>")
}

// Behavior: a zip upload with a wrapping folder is stripped and served at the version link.
func TestOwnerUploadsZipAndEveryFileIsServed(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	data := zipBytes(t, map[string]string{"site/index.html": "zipped", "site/js/a.js": "js", "__MACOSX/site/._index.html": "x", "site/.DS_Store": "x"})
	out := uploadOK(t, f, c, "", "application/zip; charset=binary", data)
	if out.Version != 2 || out.Files != 2 {
		t.Fatalf("%+v", out)
	}
	f.content("/demo@v2/", "zipped")
	f.content("/demo@v2/js/a.js", "js")
	f.want("GET", "/demo@v2/.DS_Store", "", nil, 404)
	f.want("GET", "/demo@v2/__MACOSX/site/._index.html", "", nil, 404)
}

// Behavior: promote=0 keeps the main entry on the old version and the page says which version is still main.
func TestUploadWithoutPromoteKeepsMainVersion(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	out := uploadOK(t, f, c, "?promote=0", "application/zip", zipBytes(t, map[string]string{"index.html": "second"}))
	if out.Promoted || out.CurrentVersion == nil || *out.CurrentVersion != 1 {
		t.Fatalf("%+v", out)
	}
	f.content("/demo/", "first")
	f.content("/demo@v2/", "second")
	consolePage(t, f, c, "/apps/demo?uploaded=2", "没有设为主入口", "主入口仍是 v1")
}

// Behavior: the default upload moves the main entry to the new version and the banner says so.
func TestUploadPromotesByDefaultAndBannerLinksTheVersion(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	uploadOK(t, f, c, "", "application/zip", zipBytes(t, map[string]string{"index.html": "second"}))
	page := consolePage(t, f, c, "/apps/demo?uploaded=2", "data-upload-result", "已发布 v2", "主入口已切到 v2", `href="http://apps.test/demo@v2/"`, `target="_blank"`)
	if strings.Contains(page, "data-path-warnings") {
		t.Fatal("clean upload shows warnings")
	}
}

// Behavior: invalid or unknown ?uploaded values show no banner and do not fail the page.
func TestBannerIgnoresBadUploadedParameter(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	for _, q := range []string{"99", "x", "0", "-1", ""} {
		page := consolePage(t, f, c, "/apps/demo?uploaded="+q)
		if strings.Contains(page, "data-upload-result") {
			t.Fatal(q)
		}
	}
}

// Behavior: the message becomes the version message, the audit log credits the Owner and the notification callback gets the version.
func TestUploadRecordsMessageAuditAndNotifies(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	var events []Event
	f.s.OnEvent = func(e Event) { events = append(events, e) }
	uploadOK(t, f, c, "?message="+"hello%20owner", "application/zip", zipBytes(t, map[string]string{"index.html": "x"}))
	a, _ := f.s.Store.AppBySlug("demo")
	versions, _ := f.s.Store.Versions(a.ID)
	found := false
	for _, v := range versions {
		if v.Number == 2 && v.Message == "hello owner" && v.CreatedBy == "owner" {
			found = true
		}
	}
	if !found {
		t.Fatal(versions)
	}
	audit, err := f.s.Store.Events(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if audit[0].Actor != "owner" || audit[0].Action != "app.deployed" {
		t.Fatalf("%+v", audit[0])
	}
	if len(events) != 1 || events[0].Actor != "owner" || !strings.Contains(events[0].Detail, `"version":2`) {
		t.Fatalf("%+v", events)
	}
}

// Behavior: an upload keeps the overlay directories that an Agent pushed earlier.
func TestUploadKeepsExistingOverlays(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	f.push("data/", "pushed")
	uploadOK(t, f, c, "", "application/zip", zipBytes(t, map[string]string{"index.html": "second"}))
	f.content("/demo@v3/data/new.json", "pushed")
	f.content("/demo@v3/", "second")
}

// Behavior: absolute paths in uploaded files are listed with file and line in the banner; clean uploads show no warning block.
func TestBannerListsAbsolutePathWarnings(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	uploadOK(t, f, c, "", "application/zip", zipBytes(t, map[string]string{"index.html": "<html>\n<script src=\"/assets/a.js\"></script>\n</html>"}))
	page := consolePage(t, f, c, "/apps/demo?uploaded=2", "data-path-warnings", "index.html:2")
	if !strings.Contains(page, "/assets/a.js") {
		t.Fatal(page)
	}
	uploadOK(t, f, c, "", "application/zip", zipBytes(t, map[string]string{"index.html": "<p>clean</p>"}))
	if page = consolePage(t, f, c, "/apps/demo?uploaded=3", "data-upload-result"); strings.Contains(page, "data-path-warnings") {
		t.Fatal("warnings for clean version")
	}
}

// Behavior: failed uploads return a JSON error with a stable code and never create a version.
func TestFailedUploadsReturnJSONErrorsAndCreateNoVersion(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	f.s.Config.MaxFiles = 2
	uploadError(t, f, c, "demo", "", "application/zip", zipBytes(t, map[string]string{"a": "1", "b": "2", "index.html": "3"}), 413, "too_large")
	f.s.Config.MaxFiles = 0
	e := uploadError(t, f, c, "demo", "", "application/zip", zipBytes(t, map[string]string{"README.md": "x"}), 400, "no_index")
	if !strings.Contains(e.Message, "index.html") {
		t.Fatal(e)
	}
	e = uploadError(t, f, c, "demo", "", "application/zip", zipBytes(t, map[string]string{"repo/README.md": "x", "repo/dist/index.html": "y"}), 400, "no_index")
	if !strings.Contains(e.Hint, "repo/dist/") {
		t.Fatal(e)
	}
	uploadError(t, f, c, "demo", "", "application/gzip", archive(t, map[string]string{"x.txt": "x"}, false), 400, "no_index")
	uploadError(t, f, c, "demo", "", "application/zip", []byte("not a zip"), 400, "bad_bundle")
	uploadError(t, f, c, "demo", "", "application/zip", zipBytes(t, map[string]string{"../x": "x", "index.html": "y"}), 400, "bad_bundle")
	uploadError(t, f, c, "demo", "", "application/gzip", archive(t, map[string]string{"index.html": "x"}, true), 400, "bad_bundle")
	uploadError(t, f, c, "demo", "", "text/plain", []byte("x"), 415, "unsupported_type")
	uploadError(t, f, c, "demo", "?message="+strings.Repeat("m", 1025), "application/zip", zipBytes(t, map[string]string{"index.html": "x"}), 400, "bad_request")
	uploadError(t, f, c, "nope", "", "application/zip", zipBytes(t, map[string]string{"index.html": "x"}), 404, "app_not_found")
	if n := versionCount(t, f); n != 1 {
		t.Fatalf("versions = %d", n)
	}
}

// Behavior: an encrypted zip is rejected with an explicit message.
func TestEncryptedZipIsRejected(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.CreateRaw(&zip.FileHeader{Name: "index.html", Method: zip.Store, Flags: 1})
	io.WriteString(w, "secret")
	zw.Close()
	e := uploadError(t, f, c, "demo", "", "application/zip", b.Bytes(), 400, "bad_bundle")
	if !strings.Contains(e.Message, "加密") {
		t.Fatal(e)
	}
	if versionCount(t, f) != 1 {
		t.Fatal("version created")
	}
}

// Behavior: a request body beyond the byte limit is rejected as too large.
func TestOversizedUploadBodyIsTooLarge(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	f.s.Config.MaxUploadMB = 1
	f.s.Config.MaxFiles = 1
	body := bytes.Repeat([]byte{1}, 4<<20)
	uploadError(t, f, c, "demo", "", "application/zip", body, 413, "too_large")
	big := make([]byte, 4<<20)
	rand.Read(big)
	uploadError(t, f, c, "demo", "", "application/gzip", archive(t, map[string]string{"index.html": string(big)}, false), 413, "too_large")
	if versionCount(t, f) != 1 {
		t.Fatal("version created")
	}
}

// Behavior: a deleted app cannot be uploaded to.
func TestUploadToDeletedAppIsNotFound(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	consolePost(t, f, c, "/apps/demo/delete", "", 302)
	uploadError(t, f, c, "demo", "", "application/zip", zipBytes(t, map[string]string{"index.html": "x"}), 404, "app_not_found")
}

// Behavior: without an Owner session, or from the apps origin, or with only an Agent token, nothing is uploaded.
func TestUploadRequiresOwnerSessionAndAdminOrigin(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.deploy("?create=1&visibility=public", "first")
	data := zipBytes(t, map[string]string{"index.html": "x"})
	if status, _ := upload(f, nil, "demo", "", "application/zip", data); status != 401 {
		t.Fatal(status)
	}
	r := httptest.NewRequest("POST", "http://admin.test/apps/demo/upload", bytes.NewReader(data))
	r.Host = "admin.test"
	r.Header.Set("Content-Type", "application/zip")
	r.Header.Set("Authorization", "Bearer "+f.key)
	r.Header.Set("Origin", "http://admin.test")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "http://admin.test/apps/demo/upload", bytes.NewReader(data))
	r.Host = "admin.test"
	r.Header.Set("Content-Type", "application/zip")
	r.AddCookie(c)
	r.Header.Set("Origin", "http://apps.test")
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if versionCount(t, f) != 1 {
		t.Fatal("version created")
	}
}

// Behavior: the app page shows the upload panel with the configured limits, the script and an upload-aware empty state.
func TestAppPageRendersUploadPanelWithLimits(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	f.s.Config.MaxUploadMB = 7
	f.s.Config.MaxFiles = 42
	consolePost(t, f, c, "/apps", "slug=demo&visibility=public", 302)
	consolePage(t, f, c, "/apps/demo", "data-upload", `data-max-bytes="7340032"`, `data-max-files="42"`, "/static/upload.js", "7 MB", "42 个文件", "上传新版本", "tinyhost deploy")
	f.s.Config.MaxUploadMB = 0
	f.s.Config.MaxFiles = 0
	consolePage(t, f, c, "/apps/demo", `data-max-bytes="52428800"`, `data-max-files="1000"`)
}
