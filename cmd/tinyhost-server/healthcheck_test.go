package main

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/reorx/tinyhost/internal/server"
)

// Behavior: healthcheck reads the listen address from --listen or TINYHOST_LISTEN, probes the local server without credentials, and succeeds only while it is serving.
func TestHealthcheckSucceedsOnlyWhileServerIsUp(t *testing.T) {
	s, err := openServer(t.TempDir(), server.Config{AppsURL: "http://apps.test", AdminURL: "http://admin.test"})
	if err != nil {
		t.Fatal(err)
	}
	s.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	defer s.Store.Close()
	live := httptest.NewServer(s.Handler())
	defer live.Close()
	_, port, _ := net.SplitHostPort(live.Listener.Addr().String())
	// Probing must not create a data directory where it runs.
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	for _, listen := range []string{live.Listener.Addr().String(), ":" + port, "0.0.0.0:" + port} {
		out.Reset()
		if err := run([]string{"healthcheck", "--listen", listen, "--apps-url", "http://elsewhere.test"}, &out); err != nil {
			t.Fatalf("%s: %v", listen, err)
		}
		if strings.TrimSpace(out.String()) != "ok" {
			t.Fatalf("%s: %q", listen, out.String())
		}
	}
	t.Setenv("TINYHOST_LISTEN", ":"+port)
	if err := run([]string{"healthcheck"}, &out); err != nil {
		t.Fatalf("TINYHOST_LISTEN: %v", err)
	}
	if entries, _ := os.ReadDir("."); len(entries) != 0 {
		t.Fatalf("healthcheck wrote %v", entries)
	}
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer unhealthy.Close()
	if err := run([]string{"healthcheck", "--listen", unhealthy.Listener.Addr().String()}, &out); err == nil {
		t.Fatal("503 reported healthy")
	}
	addr := live.Listener.Addr().String()
	live.Close()
	if err := run([]string{"healthcheck", "--listen", addr}, &out); err == nil {
		t.Fatal("stopped server reported healthy")
	}
}
