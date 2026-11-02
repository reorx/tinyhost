package notify

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Behavior: default subscriptions forward only device requests, new apps and app bundles, with identical webhook events and ntfy metadata.
func TestNotifyDefaultEvents(t *testing.T) {
	headers := make(chan http.Header, 8)
	webhooks := make(chan Message, 8)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { headers <- r.Header.Clone() }))
	defer ntfy.Close()
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m Message
		json.NewDecoder(r.Body).Decode(&m)
		webhooks <- m
	}))
	defer hook.Close()
	h := New(Config{NtfyURL: ntfy.URL, NtfyToken: "secret", WebhookURL: hook.URL}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, event := range []string{"device.requested", "app.created", "app.deployed", "data.pushed", "app.promoted", "app.deleted", "preview.created"} {
		h.Publish(Message{Event: event, Title: "测试", Message: "内容", URL: "https://apps.test/demo/", At: 123})
	}
	h.Close()
	if len(headers) != 3 || len(webhooks) != 3 {
		t.Fatalf("ntfy=%d hook=%d", len(headers), len(webhooks))
	}
	for range 3 {
		hd := <-headers
		m := <-webhooks
		if hd.Get("Click") != m.URL || hd.Get("Title") == "" || hd.Get("Tags") == "" || hd.Get("Priority") == "" || hd.Get("Authorization") != "Bearer secret" || m.At != 123 {
			t.Fatal(hd, m)
		}
	}
	// Explicit subscriptions replace defaults.
	h = New(Config{WebhookURL: hook.URL, Events: "data.pushed"}, nil)
	h.Publish(Message{Event: "app.created"})
	h.Publish(Message{Event: "data.pushed"})
	h.Close()
	if len(webhooks) != 1 || (<-webhooks).Event != "data.pushed" {
		t.Fatal("custom events")
	}
}

// Behavior: publishing to a full queue never waits for the backend, and shutdown safely drains accepted notifications.
func TestNotifyQueueIsBoundedAndNonblocking(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}))
	defer remote.Close()
	h := New(Config{NtfyURL: remote.URL}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h.Publish(Message{Event: "app.created"})
	<-started
	done := make(chan struct{})
	go func() {
		for range 200 {
			h.Publish(Message{Event: "app.created"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queue blocked")
	}
	close(release)
	h.Close()
	h.Publish(Message{Event: "app.created"})
}

// Behavior: failed notifications are logged without leaking bearer credentials.
func TestNotifyFailureIsLogged(t *testing.T) {
	var log strings.Builder
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer remote.Close()
	h := New(Config{NtfyURL: remote.URL, NtfyToken: "secret"}, slog.New(slog.NewJSONHandler(&log, nil)))
	h.Publish(Message{Event: "app.deployed"})
	h.Close()
	if !strings.Contains(log.String(), "503") || strings.Contains(log.String(), "secret") {
		t.Fatal(log.String())
	}
}
