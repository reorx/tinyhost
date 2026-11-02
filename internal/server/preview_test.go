package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type previewResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
	Reused    bool   `json:"reused"`
}

func (f *fixture) preview(key, query, content string) previewResponse {
	f.t.Helper()
	b := f.want("POST", "/_/api/v1/previews?"+query, key, archive(f.t, map[string]string{"index.html": content, "app.js": "relative script", "about.html": "about", "docs/index.html": "docs", "404.html": "missing"}, false), 201)
	var p previewResponse
	if err := json.Unmarshal(b, &p); err != nil {
		f.t.Fatal(err)
	}
	return p
}
func previewPath(p previewResponse) string { return strings.TrimPrefix(p.URL, "http://apps.test") }

// Behavior: unguessable preview links serve content and relative assets without credentials, with M1 path rules.
func TestPreviewIsPublicByUnguessableLink(t *testing.T) {
	f := setup(t)
	p := f.preview(f.key, "key=site", "<script src=\"./app.js\"></script>")
	path := previewPath(p)
	token := strings.TrimSuffix(strings.TrimPrefix(path, "/_p/"), "/")
	if len(token) != 20 || strings.Trim(token, "abcdefghijklmnopqrstuvwxyz234567") != "" {
		t.Fatal(token)
	}
	f.content(path, "<script src=\"./app.js\"></script>")
	f.content(path+"app.js", "relative script")
	f.content(path+"about", "about")
	f.want("GET", path+"docs", "", nil, 301)
	f.want("GET", path+"missing", "", nil, 404)
	f.want("GET", path+"%2e%2e/index.html", "", nil, 404)
	f.want("GET", path+"tinyhost.json", "", nil, 404)
	_, _, h := f.request("GET", path, "", nil)
	if h.Get("Cache-Control") != "no-cache" || h.Get("X-Robots-Tag") != "noindex" || h.Get("Service-Worker-Allowed") != "" {
		t.Fatal(h)
	}
	f.gc()
	f.content(path+"app.js", "relative script")
	f.want("GET", "/_p/aaaaaaaaaaaaaaaaaaaa/", "", nil, 404)
	b := f.want("GET", "/_/api/v1/apps", f.key, nil, 200)
	if string(b) != "{\"apps\":[]}\n" {
		t.Fatal(string(b))
	}
	f.want("POST", "/_/api/v1/previews?key=site", "", nil, 401)
}

// Behavior: same client and key update the content and expiry while preserving the link, including after expiry and GC.
func TestPreviewReusesLinkPerClientAndKey(t *testing.T) {
	f := setup(t)
	first := f.preview(f.key, "key=site&ttl=1h", "one")
	f.now = f.now.Add(2 * time.Hour)
	f.gc()
	second := f.preview(f.key, "key=site&ttl=2h", "two")
	if second.URL != first.URL || second.ID != first.ID || !second.Reused || second.ExpiresAt != f.now.Add(2*time.Hour).Unix() {
		t.Fatalf("%+v %+v", first, second)
	}
	f.content(previewPath(first), "two")
}

// Behavior: new creates an independent link and leaves the original key and content intact.
func TestPreviewNewCreatesSeparateLink(t *testing.T) {
	f := setup(t)
	a := f.preview(f.key, "key=site", "one")
	b := f.preview(f.key, "key=site&new=1", "two")
	if a.URL == b.URL || b.Reused {
		t.Fatal(a, b)
	}
	f.content(previewPath(a), "one")
	c := f.preview(f.key, "key=site", "three")
	if c.URL != a.URL {
		t.Fatal(c)
	}
}

// Behavior: expiry is enforced without GC; GC releases layers and keeps tombstones for seven days.
func TestExpiredPreviewShowsGoneThenNotFound(t *testing.T) {
	f := setup(t)
	p := f.preview(f.key, "key=site&ttl=1h", "one")
	f.now = time.Unix(p.ExpiresAt, 0)
	b := f.want("GET", previewPath(p), "", nil, 410)
	if !strings.Contains(string(b), "已过期") {
		t.Fatal(string(b))
	}
	f.gc()
	refs, err := f.s.Store.ReferencedBlobs()
	if err != nil || len(refs) != 0 {
		t.Fatal(refs, err)
	}
	f.want("GET", previewPath(p), "", nil, 410)
	f.now = f.now.Add(7*24*time.Hour + time.Second)
	f.gc()
	f.want("GET", previewPath(p), "", nil, 404)
}

// Behavior: TTL defaults to one day and accepts only Go durations in the inclusive one-hour to seven-day range.
func TestPreviewTTLBounds(t *testing.T) {
	f := setup(t)
	for _, ttl := range []string{"1h", "168h", ""} {
		p := f.preview(f.key, "new=1&ttl="+ttl, "ok")
		d := 24 * time.Hour
		if ttl != "" {
			d, _ = time.ParseDuration(ttl)
		}
		if p.ExpiresAt != f.now.Add(d).Unix() {
			t.Fatal(p)
		}
	}
	for _, ttl := range []string{"30d", "59m", "169h", "-1h", "nonsense"} {
		f.want("POST", "/_/api/v1/previews?key=site&ttl="+ttl, f.key, nil, 400)
	}
	f.want("POST", "/_/api/v1/previews", f.key, nil, 400)
}

// Behavior: listing and deletion are scoped to the caller and expired entries are excluded.
func TestPreviewsAreScopedToClient(t *testing.T) {
	f := setup(t)
	a := f.preview(f.key, "key=site&ttl=1h&source=site", "one")
	b := f.preview(f.other, "key=site", "two")
	raw := f.want("GET", "/_/api/v1/previews", f.key, nil, 200)
	if !strings.Contains(string(raw), a.ID) || strings.Contains(string(raw), b.ID) {
		t.Fatal(string(raw))
	}
	f.want("DELETE", "/_/api/v1/previews/"+b.ID, f.key, nil, 404)
	f.now = f.now.Add(time.Hour)
	raw = f.want("GET", "/_/api/v1/previews", f.key, nil, 200)
	if strings.Contains(string(raw), a.ID) {
		t.Fatal(string(raw))
	}
	f.want("DELETE", "/_/api/v1/previews/"+a.ID, f.key, nil, 200)
	f.want("GET", previewPath(a), "", nil, 404)
}

// Behavior: Owner sees all clients' previews, can delete any with Origin protection, and leaves an audit event.
func TestAdminCanDeleteAnyPreview(t *testing.T) {
	f := setup(t)
	a := f.preview(f.key, "key=a&source=first", "one")
	b := f.preview(f.other, "key=b&source=second", "two")
	cookie := adminCookie(t, f)
	consolePage(t, f, cookie, "/previews", a.ID, b.ID, "first", "second")
	req, _ := http.NewRequest("POST", "http://admin.test/previews/"+b.ID+"/delete", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	consolePost(t, f, cookie, "/previews/"+b.ID+"/delete", "", 302)
	f.want("GET", previewPath(b), "", nil, 404)
	consolePage(t, f, cookie, "/events", "preview.deleted")
}

// Behavior: a preview link remains deletable by its public token after expiry, only by its creator.
func TestExpiredPreviewCanBeDeletedByLinkToken(t *testing.T) {
	f := setup(t)
	p := f.preview(f.key, "key=site&ttl=1h", "one")
	f.now = f.now.Add(2 * time.Hour)
	f.gc()
	value := strings.TrimSuffix(strings.TrimPrefix(previewPath(p), "/_p/"), "/")
	f.want("DELETE", "/_/api/v1/previews/"+value, f.other, nil, 404)
	b := f.want("DELETE", "/_/api/v1/previews/"+value, f.key, nil, 200)
	if !strings.Contains(string(b), p.ID) {
		t.Fatal(string(b))
	}
	f.want("GET", previewPath(p), "", nil, 404)
}
