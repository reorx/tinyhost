package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/notify"
	"github.com/reorx/tinyhost/internal/server"
	"github.com/reorx/tinyhost/internal/token"
	"github.com/reorx/tinyhost/internal/version"
)

func notificationServer(t *testing.T, ntfy string) (*server.Server, *notify.Hub, string) {
	t.Helper()
	s, err := openServer(t.TempDir(), server.Config{AppsURL: "http://apps.test", AdminURL: "http://admin.test"})
	if err != nil {
		t.Fatal(err)
	}
	s.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := notify.New(notify.Config{NtfyURL: ntfy}, s.Logger)
	s.OnEvent = notificationCallback(s, h)
	key := token.New()
	if _, err = s.Store.CreateClient("writer", token.Hash(key), nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(); s.Store.Close() })
	return s, h, key
}
func publishBundle(t *testing.T, s *server.Server, key, query string) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("hello"), 0600)
	path, _, err := bundle.Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	body, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	r := httptest.NewRequest("POST", "http://apps.test/_/api/v1/apps/demo/deploys"+query, body)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// Behavior: a successful device request asynchronously sends its exact approval URL and public verification details to ntfy.
func TestDeviceRequestNotifiesWithApproveLink(t *testing.T) {
	type received struct {
		Header http.Header
		Body   string
	}
	messages := make(chan received, 2)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		messages <- received{r.Header.Clone(), string(b)}
	}))
	defer ntfy.Close()
	s, h, _ := notificationServer(t, ntfy.URL)
	r := httptest.NewRequest("POST", "http://apps.test/_/api/v1/device/code", strings.NewReader(`{"name":"cloud agent","hostname":"sandbox"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d api.DeviceCodeResult
	json.Unmarshal(w.Body.Bytes(), &d)
	h.Close()
	select {
	case m := <-messages:
		if m.Header.Get("Click") != d.VerificationURIComplete || !strings.Contains(m.Body, d.UserCode) || !strings.Contains(m.Body, "sandbox") || strings.Contains(m.Body, d.DeviceCode) {
			t.Fatal(m)
		}
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}
	events, err := s.Store.Events("")
	if err != nil || len(events) != 1 || events[0].Action != "device.requested" {
		t.Fatal(events, err)
	}
}

// Behavior: ntfy failure never changes creation or deployment results and published content remains available.
func TestNotifyFailureDoesNotAffectDeploy(t *testing.T) {
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer ntfy.Close()
	s, h, key := notificationServer(t, ntfy.URL)
	for _, q := range []string{"?create=1&visibility=public", "?message=second"} {
		w := publishBundle(t, s, key, q)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h.Close()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://apps.test/demo/", nil))
	if w.Code != 200 || w.Body.String() != "hello" {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Behavior: bundle creation and publishing produce useful ntfy titles, actors and version messages, linked to the main URL.
func TestAppNotificationsDescribeCreationAndPublishedVersion(t *testing.T) {
	type notice struct{ Title, Click, Body string }
	notices := make(chan notice, 4)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		title, err := new(mime.WordDecoder).DecodeHeader(r.Header.Get("Title"))
		if err != nil {
			t.Error(err)
		}
		notices <- notice{title, r.Header.Get("Click"), string(body)}
	}))
	defer ntfy.Close()
	s, h, key := notificationServer(t, ntfy.URL)
	w := publishBundle(t, s, key, "?create=1&title=Report&message=initial")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	h.Close()
	if len(notices) != 2 {
		t.Fatal(len(notices))
	}
	created, deployed := <-notices, <-notices
	if created.Title != "新 App：demo" || !strings.Contains(created.Body, "writer") || !strings.Contains(created.Body, "Report") {
		t.Fatal(created)
	}
	if deployed.Title != "demo v1 已发布" || !strings.Contains(deployed.Body, "initial") || !strings.Contains(deployed.Body, "writer") {
		t.Fatal(deployed)
	}
	if created.Click != "http://apps.test/demo/" || deployed.Click != created.Click {
		t.Fatal(created, deployed)
	}
}

// Behavior: distribution and notification flags honor environment defaults and CLI overrides, with version-specific local cache paths.
func TestDistributionAndNotificationFlags(t *testing.T) {
	t.Setenv("TINYHOST_NTFY_URL", "http://ntfy.test/topic")
	t.Setenv("TINYHOST_NOTIFY_EVENTS", "app.deleted")
	var c server.Config
	var n notify.Config
	dir, _, err := flags("serve", []string{"--data-dir", t.TempDir(), "--release-repo", "owner/custom", "--dl-dir", "dist", "--ntfy-url", "http://override.test/topic", "--ntfy-token", "secret", "--webhook-url", "http://hook.test"}, &c, &n)
	if err != nil || c.ReleaseRepo != "owner/custom" || c.DLDir != "dist" || n.NtfyURL != "http://override.test/topic" || n.NtfyToken != "secret" || n.WebhookURL != "http://hook.test" || n.Events != "app.deleted" {
		t.Fatal(c, n, err)
	}
	s, err := openServer(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Store.Close()
	if !strings.Contains(s.Config.DLCacheDir, filepath.Join("dl", strings.TrimPrefix(version.Version, "v"))) {
		t.Fatal(s.Config.DLCacheDir)
	}
}
