package server

import (
	"net/http/httptest"
	"testing"

	"github.com/reorx/tinyhost/internal/version"
)

// Behavior: /_/health answers on any Host without credentials, reveals only the version, rejects writes, and reports 503 when the database is unusable.
func TestHealthEndpointReportsDatabaseStateOnAnyHost(t *testing.T) {
	f := setup(t)
	for _, host := range []string{"apps.test", "admin.test", "127.0.0.1:3471"} {
		for _, method := range []string{"GET", "HEAD"} {
			r := httptest.NewRequest(method, "http://"+host+"/_/health", nil)
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			body := "ok\n"
			if method == "HEAD" {
				body = ""
			}
			if w.Code != 200 || w.Body.String() != body || w.Header().Get("X-Tinyhost-Version") != version.Version || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s %s: %d %q %v", method, host, w.Code, w.Body.String(), w.Header())
			}
			if cookies := w.Result().Cookies(); len(cookies) != 0 {
				t.Fatalf("%s %s set cookies %v", method, host, cookies)
			}
		}
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "http://apps.test/_/health", nil))
	if w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	f.s.Store.Close()
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://apps.test/_/health", nil))
	if w.Code != 503 || w.Body.String() != "unavailable\n" {
		t.Fatalf("closed database: %d %q", w.Code, w.Body.String())
	}
}
