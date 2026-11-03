package server

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/reorx/tinyhost/internal/version"
)

// health answers /_/health on every Host, so `tinyhost-server healthcheck`
// needs only the listen address. It reveals nothing beyond the build version.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Tinyhost-Version", version.Version)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		s.logger().Error("health check failed", "error", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method == "GET" {
		_, _ = io.WriteString(w, "ok\n")
	}
}
