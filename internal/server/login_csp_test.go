package server

import (
	"net/url"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Behavior: only login responses allow the configured apps origin in form navigation, including retries and redirects.
func TestOnlyLoginAllowsConfiguredAppsOriginInFormNavigation(t *testing.T) {
	for _, origin := range []string{"http://apps.test", "https://private.example:8443/"} {
		t.Run(origin, func(t *testing.T) {
			f := setup(t)
			f.s.Config.AppsURL = origin
			cookie := adminCookie(t, f)
			hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.s.Store.SetSetting("owner_password_hash", string(hash)); err != nil {
				t.Fatal(err)
			}
			apps, err := url.Parse(origin)
			if err != nil {
				t.Fatal(err)
			}
			base := "default-src 'self'; frame-ancestors 'none'; form-action 'self'"
			loginCSP := base + " " + apps.Scheme + "://" + apps.Host
			next := "/view-auth?next=" + url.QueryEscape("/private-app/nested.html?proof=return")
			for _, tc := range []struct {
				name, method, path, body, csp string
				status                        int
			}{
				{"login page", "GET", "/login?next=" + url.QueryEscape(next), "", loginCSP, 200},
				{"wrong password", "POST", "/login", "password=wrong&next=" + url.QueryEscape(next), loginCSP, 401},
				{"successful login", "POST", "/login", "password=correct&next=" + url.QueryEscape(next), loginCSP, 302},
				{"apps page", "GET", "/", "", base, 200},
				{"agents page", "GET", "/agents", "", base, 200},
				{"view ticket redirect", "GET", next, "", base, 302},
				{"unmatched route", "GET", "/login/other", "", base, 404},
				{"other POST", "POST", "/apps/missing/delete", "", base, 404},
			} {
				t.Run(tc.name, func(t *testing.T) {
					w := authReq(f, "admin.test", tc.method, tc.path, tc.body, cookie)
					if w.Code != tc.status {
						t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
					}
					if got := w.Header().Get("Content-Security-Policy"); got != tc.csp {
						t.Fatalf("CSP = %q, want %q", got, tc.csp)
					}
					if tc.name == "successful login" && w.Header().Get("Location") != next {
						t.Fatalf("login lost destination: %q", w.Header().Get("Location"))
					}
					if tc.name == "view ticket redirect" {
						u, err := url.Parse(w.Header().Get("Location"))
						if err != nil || u.Scheme != apps.Scheme || u.Host != apps.Host || u.Path != "/_/auth" || u.Query().Get("next") != "/private-app/nested.html?proof=return" || u.Query().Get("ticket") == "" {
							t.Fatalf("view ticket redirect changed: %q", w.Header().Get("Location"))
						}
					}
				})
			}
		})
	}
}
