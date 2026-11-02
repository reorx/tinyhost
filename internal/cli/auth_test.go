package cli

import (
	"encoding/json"
	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/server"
	"github.com/reorx/tinyhost/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/api"
)

// Behavior: pending login exits four, resumes the same device code and saves a private server-scoped credential after approval.
func TestLoginTimesOutAsPendingAndResumesSameCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TINYHOST_TOKEN", "")
	codes := 0
	approved := false
	received := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_/api/v1/device/code":
			codes++
			json.NewEncoder(w).Encode(api.DeviceCodeResult{DeviceCode: "secret", UserCode: "BCDF-GHJK", VerificationURIComplete: "http://admin.test/device?code=BCDF-GHJK", ExpiresIn: 600, Interval: 3})
		case "/_/api/v1/device/token":
			if !approved {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(api.Error{Error: api.ErrorDetail{Code: "authorization_pending"}})
			} else {
				json.NewEncoder(w).Encode(api.DeviceTokenResult{Token: "saved-token", Client: api.Client{ID: "c_test", Name: "approved"}})
			}
		case "/_/api/v1/whoami":
			received = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(api.WhoamiResult{Client: api.Client{ID: "c_test", Name: "approved"}})
		case "/_/api/v1/logout":
			received = r.Header.Get("Authorization")
			json.NewEncoder(w).Encode(api.LogoutResult{LoggedOut: true})
		}
	}))
	defer s.Close()
	t.Setenv("TINYHOST_SERVER", s.URL)
	code, out, errOut := run("login", "--wait", "0", "--json")
	if code != 4 {
		t.Fatal(code, out, errOut)
	}
	checkGolden(t, "login_pending", out)
	approved = true
	code, out, errOut = run("login", "--wait", "1", "--json")
	if code != 0 || codes != 1 {
		t.Fatal(code, codes, out, errOut)
	}
	checkGolden(t, "login", out)
	dir, _ := os.UserConfigDir()
	info, err := os.Stat(filepath.Join(dir, "tinyhost", "credentials.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	code, _, _ = run("whoami")
	if code != 0 || received != "Bearer saved-token" {
		t.Fatal(code, received)
	}
	code, out, errOut = run("logout", "--json")
	if code != 0 {
		t.Fatal(code, out, errOut)
	}
	checkGolden(t, "logout", out)
	if key, err := credentialToken(s.URL); key != "" || err != nil {
		t.Fatal(key, err)
	}
}

// Behavior: manually issued environment tokens work and login does not create a device request.
func TestManuallyIssuedTokenWorksFromEnv(t *testing.T) {
	f := setup(t)
	f.ok("whoami")
	before := f.requests.Load()
	code, out, errOut := run("login", "--json")
	if code != 0 || f.requests.Load() != before || !strings.Contains(out, "environment") {
		t.Fatal(code, out, errOut)
	}
	checkGolden(t, "login_env", out)
}

// Behavior: a denied saved request is removed so the next login can start afresh.
func TestDeniedLoginClearsPending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TINYHOST_TOKEN", "")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(api.Error{Error: api.ErrorDetail{Code: "access_denied", Message: "已拒绝"}})
	}))
	defer s.Close()
	t.Setenv("TINYHOST_SERVER", s.URL)
	if err := saveAuthFile("pending-login.json", pendingLogin{Server: s.URL, DeviceCode: "secret", ExpiresAt: time.Now().Unix() + 600, Interval: 3}); err != nil {
		t.Fatal(err)
	}
	code, _, _ := run("login", "--wait", "1")
	if code == 0 {
		t.Fatal(code)
	}
	var pending pendingLogin
	if err := readAuthFile("pending-login.json", &pending); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// Behavior: the real device endpoint can be approved while CLI polling and the saved token deploys without environment credentials.
func TestLoginApprovalThenDeploysWithSavedCredential(t *testing.T) {
	f := setup(t)
	t.Setenv("TINYHOST_TOKEN", "")
	// The test server delegates the device protocol to the real server and approves immediately before its first poll.
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	blobs, err := blob.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(nil)
	origin := "http://" + s.Listener.Addr().String()
	srv := &server.Server{Store: db, Blobs: blobs, Config: server.Config{AppsURL: origin, AdminURL: "http://admin.test"}, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	handler := srv.Handler()
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_/api/v1/device/code" {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			var result api.DeviceCodeResult
			json.Unmarshal(rec.Body.Bytes(), &result)
			d, err := db.DeviceByCode(strings.ReplaceAll(result.UserCode, "-", ""))
			if err != nil || d == nil {
				t.Error(err)
				return
			}
			ttl := int64(86400)
			if err = db.DecideDevice(d.ID, "approved", "real-agent", &ttl, time.Now().Unix()); err != nil {
				t.Error(err)
			}
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			w.Write(rec.Body.Bytes())
			return
		}
		handler.ServeHTTP(w, r)
	})
	s.Start()
	defer s.Close()
	t.Setenv("TINYHOST_SERVER", s.URL)
	code, out, errOut := run("login", "--name", "real-agent", "--wait", "5")
	if code != 0 {
		t.Fatal(code, out, errOut)
	}
	code, out, errOut = run("deploy", f.site, "--app", "saved-app", "--create")
	if code != 0 {
		t.Fatal(code, out, errOut)
	}
}

// Behavior: a slow poll cannot extend the configured wait deadline and preserves its pending request for resumption.
func TestLoginWaitBoundsSlowNetworkPoll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TINYHOST_TOKEN", "")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(api.Error{Error: api.ErrorDetail{Code: "authorization_pending"}})
	}))
	defer s.Close()
	t.Setenv("TINYHOST_SERVER", s.URL)
	if err := saveAuthFile("pending-login.json", pendingLogin{Server: s.URL, DeviceCode: "secret", ExpiresAt: time.Now().Unix() + 600, Interval: 3}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	code, out, errOut := run("login", "--wait", "1", "--json")
	if code != 4 || time.Since(start) > 1800*time.Millisecond {
		t.Fatal(code, time.Since(start), out, errOut)
	}
}
