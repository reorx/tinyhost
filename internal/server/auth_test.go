package server

import (
	"encoding/json"
	"fmt"
	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/token"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authReq(f *fixture, host, method, p, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+p, strings.NewReader(body))
	r.Host = host
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://admin.test")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func adminCookie(t *testing.T, f *fixture) *http.Cookie {
	t.Helper()
	f.s.Store.SetSetting("owner_password_hash", "configured")
	plain := token.New()
	if err := f.s.Store.CreateSession(token.Hash(plain), "admin", "", "", f.now.Unix()); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "__Host-th_admin", Value: plain}
}
func device(t *testing.T, f *fixture) api.DeviceCodeResult {
	t.Helper()
	w := authReq(f, "apps.test", "POST", "/_/api/v1/device/code", `{"name":"agent","hostname":"host"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d api.DeviceCodeResult
	json.Unmarshal(w.Body.Bytes(), &d)
	return d
}
func approve(t *testing.T, f *fixture, d api.DeviceCodeResult, ttl string) {
	t.Helper()
	stored, err := f.s.Store.DeviceByCode(strings.ReplaceAll(d.UserCode, "-", ""))
	if err != nil || stored == nil {
		t.Fatal(err)
	}
	w := authReq(f, "admin.test", "POST", "/device/"+stored.ID+"/approve", "name=approved&ttl="+ttl, adminCookie(t, f))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func poll(f *fixture, d api.DeviceCodeResult) *httptest.ResponseRecorder {
	return authReq(f, "apps.test", "POST", "/_/api/v1/device/token", `{"device_code":"`+d.DeviceCode+`"}`)
}

// Behavior: owner approval creates a usable token only at polling and cannot be replayed.
func TestDeviceFlowApproveThenTokenWorks(t *testing.T) {
	f := setup(t)
	d := device(t, f)
	approve(t, f, d, "30d")
	w := poll(f, d)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result api.DeviceTokenResult
	json.Unmarshal(w.Body.Bytes(), &result)
	status, _, _ := f.request("GET", "/_/api/v1/whoami", result.Token, nil)
	if status != 200 {
		t.Fatal(status)
	}
	f.now = f.now.Add(3 * time.Second)
	if poll(f, d).Code != 400 {
		t.Fatal("replayed")
	}
}

// Behavior: denied and expired requests never mint tokens.
func TestDeniedAndExpiredDeviceRequests(t *testing.T) {
	for _, denied := range []bool{true, false} {
		f := setup(t)
		d := device(t, f)
		want := "expired_token"
		if denied {
			v, _ := f.s.Store.DeviceByCode(strings.ReplaceAll(d.UserCode, "-", ""))
			authReq(f, "admin.test", "POST", "/device/"+v.ID+"/deny", "", adminCookie(t, f))
			want = "access_denied"
		} else {
			f.now = f.now.Add(601 * time.Second)
		}
		w := poll(f, d)
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal(w.Body.String())
		}
	}
}

// Behavior: revoked clients get an actionable login hint.
func TestRevokedClientGets401WithLoginHint(t *testing.T) {
	f := setup(t)
	c, _ := f.s.Store.ClientByTokenHash(token.Hash(f.key))
	f.s.Store.RevokeClient(c.ID)
	status, b, _ := f.request("GET", "/_/api/v1/whoami", f.key, nil)
	if status != 401 || !strings.Contains(string(b), "tinyhost login") {
		t.Fatal(status, string(b))
	}
}

// Behavior: a one day device grant expires at its exact deadline.
func TestTokenTTLIsEnforced(t *testing.T) {
	f := setup(t)
	d := device(t, f)
	approve(t, f, d, "1d")
	w := poll(f, d)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result api.DeviceTokenResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if status, _, _ := f.request("GET", "/_/api/v1/whoami", result.Token, nil); status != 200 {
		t.Fatal(status)
	}
	f.now = f.now.Add(24 * time.Hour)
	status, _, _ := f.request("GET", "/_/api/v1/whoami", result.Token, nil)
	if status != 401 {
		t.Fatal(status)
	}
}

// Behavior: private navigation goes through admin login and a single use ticket, preserving path and query.
func TestPrivateAppRedirectsThroughAdminAndBack(t *testing.T) {
	f := setup(t)
	f.s.Store.SetSetting("owner_password_hash", "configured")
	f.request("POST", "/_/api/v1/apps/private-app/deploys?create=1", f.key, archive(t, map[string]string{"index.html": "private"}, false))
	r := httptest.NewRequest("GET", "http://apps.test/private-app@v1/?q=yes", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	login := authReq(f, "admin.test", "GET", u.RequestURI(), "")
	if login.Code != 302 || !strings.HasPrefix(login.Header().Get("Location"), "/login?") {
		t.Fatal(login.Code, login.Header())
	}
	w = authReq(f, "admin.test", "GET", u.RequestURI(), "", adminCookie(t, f))
	u, _ = url.Parse(w.Header().Get("Location"))
	ticketPath := u.RequestURI()
	w = authReq(f, "apps.test", "GET", ticketPath, "")
	if w.Code != 302 || w.Header().Get("Location") != "/private-app@v1/?q=yes" {
		t.Fatal(w.Code, w.Header())
	}
	cookies := w.Result().Cookies()
	w = authReq(f, "apps.test", "GET", "/private-app@v1/?q=yes", "", cookies...)
	if w.Code != 200 || w.Body.String() != "private" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	if authReq(f, "apps.test", "GET", ticketPath, "").Code != 400 {
		t.Fatal("ticket replay")
	}
}

// Behavior: app cookies cannot authorize admin approval or any controlled Agent API write.
func TestAppPageCannotPerformControlledActions(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	for _, p := range []string{"/device/d_test/approve", "/apps/demo/delete", "/apps/demo/visibility", "/apps/demo/upload"} {
		r := httptest.NewRequest("POST", "http://admin.test"+p, nil)
		r.AddCookie(c)
		r.AddCookie(&http.Cookie{Name: "__Host-th_view", Value: c.Value})
		r.Header.Set("Origin", "http://apps.test")
		r.Header.Set("Sec-Fetch-Site", "same-site")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	for _, item := range []struct{ method, path string }{{"POST", "/apps/x/deploys"}, {"POST", "/apps/x/data"}, {"DELETE", "/apps/x/data"}, {"POST", "/apps/x/promote"}, {"DELETE", "/apps/x"}, {"POST", "/logout"}} {
		w := authReq(f, "apps.test", item.method, "/_/api/v1"+item.path, "", c)
		if w.Code != 401 {
			t.Fatal(item, w.Code)
		}
	}
}

// Behavior: logout-all invalidates admin and viewer sessions in every browser.
func TestLogoutAllInvalidatesEverySession(t *testing.T) {
	f := setup(t)
	a := adminCookie(t, f)
	b := adminCookie(t, f)
	f.s.Store.CreateSession(token.Hash("viewer"), "viewer", "", "", f.now.Unix())
	w := authReq(f, "admin.test", "POST", "/logout-all", "", a)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}
	if authReq(f, "admin.test", "GET", "/", "", b).Code != 302 {
		t.Fatal("admin survived")
	}
	ok, err := f.s.Store.ValidSession(token.Hash("viewer"), "viewer", f.now.Unix())
	if ok || err != nil {
		t.Fatal(ok, err)
	}
}

// Behavior: missing Origin without same-origin fetch metadata is rejected, including login.
func TestAdminPostWithoutOriginRejected(t *testing.T) {
	f := setup(t)
	r := httptest.NewRequest("POST", "http://admin.test/login", nil)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

// Behavior: every admin response prevents framing and caching.
func TestAdminHeadersDenyFraming(t *testing.T) {
	f := setup(t)
	w := authReq(f, "admin.test", "GET", "/login", "")
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Header())
	}
}

// Behavior: view-auth rejects offsite, backslash and control-character destinations.
func TestViewAuthRejectsOffsiteNext(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	for _, p := range []string{"//evil.test", "https://evil.test", "/\\evil", "/\r\nevil"} {
		w := authReq(f, "admin.test", "GET", "/view-auth?next="+url.QueryEscape(p), "", c)
		if w.Code != 400 {
			t.Fatal(p, w.Code)
		}
	}
}

// Behavior: repeated login attempts are bounded per IP.
func TestLoginRateLimited(t *testing.T) {
	f := setup(t)
	f.s.Store.SetSetting("owner_password_hash", "invalid")
	for i := 0; i < 6; i++ {
		w := authReq(f, "admin.test", "POST", "/login", "password=bad")
		if i == 5 && w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
}

// Behavior: device requests are bounded to ten per minute per IP.
func TestDeviceCodeRateLimited(t *testing.T) {
	f := setup(t)
	for i := 0; i < 11; i++ {
		w := authReq(f, "apps.test", "POST", "/_/api/v1/device/code", `{"name":"agent"}`)
		if i == 10 && w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
}

// Behavior: correct passwords issue host-only expiring cookies; logout revokes only that browser and same-origin fallback is accepted.
func TestAdminPasswordLoginLogoutAndCookiePolicy(t *testing.T) {
	f := setup(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), 12)
	if err != nil {
		t.Fatal(err)
	}
	f.s.Store.SetSetting("owner_password_hash", string(hash))
	for _, insecure := range []bool{false, true} {
		f.s.Config.InsecureCookies = insecure
		w := authReq(f, "admin.test", "POST", "/login", "password=password&next=%2Fdevice%3Fcode%3DTEST")
		if w.Code != 302 {
			t.Fatal(w.Code, w.Body.String())
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal(cookies)
		}
		c := cookies[0]
		if c.Secure == insecure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.MaxAge != 30*86400 || c.SameSite != http.SameSiteLaxMode {
			t.Fatal(c)
		}
		w = authReq(f, "admin.test", "GET", "/", "", c)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		r := httptest.NewRequest("POST", "http://admin.test/logout", nil)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(c)
		w = httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 302 {
			t.Fatal(w.Code)
		}
		if authReq(f, "admin.test", "GET", "/", "", c).Code != 302 {
			t.Fatal("logout failed")
		}
	}
}

// Behavior: pending polls enforce a two-second minimum, decisions are final, and session deadlines are fixed.
func TestDevicePollingAndSessionExpiry(t *testing.T) {
	f := setup(t)
	d := device(t, f)
	if !strings.Contains(poll(f, d).Body.String(), "authorization_pending") {
		t.Fatal("pending")
	}
	if !strings.Contains(poll(f, d).Body.String(), "slow_down") {
		t.Fatal("slow_down")
	}
	approve(t, f, d, "forever")
	v, _ := f.s.Store.DeviceByCode(strings.ReplaceAll(d.UserCode, "-", ""))
	if err := f.s.Store.DecideDevice(v.ID, "denied", "", nil, f.now.Unix()); err == nil {
		t.Fatal("decision replaced")
	}
	c := adminCookie(t, f)
	f.now = f.now.Add(30 * 24 * time.Hour)
	if authReq(f, "admin.test", "GET", "/", "", c).Code != 302 {
		t.Fatal("session renewed")
	}
}

// Behavior: the global login budget applies across source IPs and refills over time.
func TestGlobalLoginRateLimitRefills(t *testing.T) {
	f := setup(t)
	if err := f.s.Store.SetSetting("owner_password_hash", "invalid"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 31; i++ {
		r := httptest.NewRequest("POST", "http://admin.test/login", strings.NewReader("password=bad"))
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i)
		r.Header.Set("Origin", "http://admin.test")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		want := 401
		if i == 30 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(i, w.Code)
		}
	}
	f.now = f.now.Add(time.Minute)
	if w := authReq(f, "admin.test", "POST", "/login", "password=bad"); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

// Behavior: the approval page shows the Agent's reported host, platform, CLI version and source IP as separate facts, and the approval result echoes the granted name and lifetime.
func TestDevicePageShowsRequestFactsAndApprovalResult(t *testing.T) {
	f := setup(t)
	c := adminCookie(t, f)
	w := authReq(f, "apps.test", "POST", "/_/api/v1/device/code", `{"name":"agent","hostname":"build-box","os":"linux","arch":"arm64","cli_version":"v9.8.7"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d api.DeviceCodeResult
	json.Unmarshal(w.Body.Bytes(), &d)
	page := authReq(f, "admin.test", "GET", "/device?code="+d.UserCode, "", c)
	for _, want := range []string{d.UserCode, "build-box", "linux/arm64", "v9.8.7", "192.0.2.1"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("missing %q: %s", want, page.Body.String())
		}
	}
	stored, _ := f.s.Store.DeviceByCode(strings.ReplaceAll(d.UserCode, "-", ""))
	result := authReq(f, "admin.test", "POST", "/device/"+stored.ID+"/approve", "name=renamed-agent&ttl=forever", c)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "renamed-agent") || !strings.Contains(result.Body.String(), "永久") {
		t.Fatal(result.Code, result.Body.String())
	}
}
