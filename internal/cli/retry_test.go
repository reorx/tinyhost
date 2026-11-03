package cli

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every test that meets a failing server retries; keep the waits short.
func init() { retryDelays = []time.Duration{time.Millisecond, time.Millisecond} }

// cdn sits in front of the test server like Cloudflare: the first request for
// each method and path fails with 522 (or a dropped connection for whoami),
// later ones are forwarded. down makes every request fail.
type cdn struct {
	mu   sync.Mutex
	seen map[string]int
	down bool
	next http.Handler
}

func newCDN(t *testing.T, origin string) (*cdn, *httptest.Server) {
	t.Helper()
	target, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	// SetURL also sends the origin's Host, which the server routes on.
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target) }}
	c := &cdn{seen: map[string]int{}, next: proxy}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	return c, srv
}
func (c *cdn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	c.mu.Lock()
	c.seen[key]++
	fail := c.seen[key] == 1 || c.down
	c.mu.Unlock()
	if fail && strings.HasSuffix(r.URL.Path, "/whoami") {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	if fail {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(522)
		w.Write([]byte("<html>Connection timed out</html>"))
		return
	}
	c.next.ServeHTTP(w, r)
}
func (c *cdn) count(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[key]
}

// Behavior: read-only commands retry a CDN 52x or dropped connection a bounded number of times, while uploads, promotions and deletions are sent exactly once.
func TestReadOnlyRequestsRetryGatewayErrorsButWritesDoNot(t *testing.T) {
	f := setup(t)
	f.create()
	edge, srv := newCDN(t, f.http.URL)
	t.Setenv("TINYHOST_SERVER", srv.URL)
	for _, tc := range []struct {
		args []string
		key  string
	}{
		{[]string{"apps"}, "GET /_/api/v1/apps"},
		{[]string{"info", "demo"}, "GET /_/api/v1/apps/demo"},
		{[]string{"versions", "demo"}, "GET /_/api/v1/apps/demo/versions"},
		{[]string{"whoami"}, "GET /_/api/v1/whoami"},
		{[]string{"previews"}, "GET /_/api/v1/previews"},
	} {
		if code, out, errOut := run(tc.args...); code != 0 {
			t.Fatalf("%v: %d %s %s", tc.args, code, out, errOut)
		}
		if n := edge.count(tc.key); n != 2 {
			t.Errorf("%s sent %d times, want 2", tc.key, n)
		}
	}
	for _, tc := range []struct {
		args []string
		key  string
	}{
		{[]string{"deploy", f.site, "--app", "demo"}, "POST /_/api/v1/apps/demo/deploys"},
		{[]string{"promote", "demo", "v1"}, "POST /_/api/v1/apps/demo/promote"},
		{[]string{"delete", "demo"}, "DELETE /_/api/v1/apps/demo"},
	} {
		if code, _, errOut := run(tc.args...); code != 1 || !strings.Contains(errOut, "HTTP 522") {
			t.Fatalf("%v: %d %s", tc.args, code, errOut)
		}
		if n := edge.count(tc.key); n != 1 {
			t.Errorf("%s sent %d times, want exactly 1", tc.key, n)
		}
	}
	edge.mu.Lock()
	edge.down = true
	edge.mu.Unlock()
	if code, _, errOut := run("info", "demo"); code != 1 || !strings.Contains(errOut, "HTTP 522") {
		t.Fatalf("persistent 522: %d %s", code, errOut)
	}
	if n := edge.count("GET /_/api/v1/apps/demo"); n != 2+3 {
		t.Errorf("persistent 522 sent %d more requests, want 3 attempts", n-2)
	}
	if code, _, _ := run("whoami"); code != 1 {
		t.Fatalf("persistent drop: %d", code)
	}
	f.ok("info", "demo", "--server", f.http.URL)
}
