// Package server provides Host-separated HTTP routing and the Bearer-only Agent API.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
)

type Config struct {
	AppsURL, AdminURL                                  string
	MaxUploadMB, MaxFiles, DataVersionsKeep, TrashDays int
	TrustedProxyHeader                                 string
	InsecureCookies                                    bool
}

func (c Config) Validate() error {
	apps, err := url.Parse(c.AppsURL)
	if err != nil || !validOrigin(apps) {
		return fmt.Errorf("apps-url must be an http(s) origin")
	}
	if c.AdminURL != "" {
		admin, err := url.Parse(c.AdminURL)
		if err != nil || !validOrigin(admin) {
			return fmt.Errorf("admin-url must be an http(s) origin")
		}
		if strings.EqualFold(apps.Host, admin.Host) {
			return fmt.Errorf("apps and admin hosts must differ")
		}
	}
	if c.MaxUploadMB < 0 || c.MaxFiles < 0 || c.DataVersionsKeep < 0 || c.TrashDays < 0 {
		return fmt.Errorf("limits must be positive")
	}
	return nil
}

func validOrigin(u *url.URL) bool {
	return u != nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}

type Event struct{ Actor, Action, AppID, Detail string }
type Server struct {
	Store   *store.Store
	Blobs   *blob.Store
	Config  Config
	Now     func() time.Time
	Logger  *slog.Logger
	OnEvent func(Event)
	// Upload and GC must not race between blob publication and SQL commit.
	// The lock also protects GC's reference snapshot against new uploads.
	lifecycle sync.RWMutex
	rateMu    sync.Mutex
	rates     map[string]rateBucket
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
func (s *Server) trashDays() int {
	if s.Config.TrashDays > 0 {
		return s.Config.TrashDays
	}
	return 7
}
func (s *Server) mainURL(slug string) string {
	return strings.TrimRight(s.Config.AppsURL, "/") + "/" + slug + "/"
}

type requestState struct{ client *store.Client }
type stateKey struct{}
type responseLog struct {
	http.ResponseWriter
	status, bytes int
}

func (w *responseLog) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseLog) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	adminHandler := s.adminHandler()
	mux.HandleFunc("POST /_/api/v1/device/code", s.deviceCode)
	mux.HandleFunc("POST /_/api/v1/device/token", s.deviceToken)
	mux.HandleFunc("POST /_/api/v1/logout", s.auth(s.agentLogout))
	mux.HandleFunc("GET /_/api/v1/whoami", s.auth(s.whoami))
	mux.HandleFunc("POST /_/api/v1/previews", s.auth(s.createPreview))
	mux.HandleFunc("GET /_/api/v1/previews", s.auth(s.listPreviews))
	mux.HandleFunc("DELETE /_/api/v1/previews/{id}", s.auth(s.deletePreview))
	mux.HandleFunc("GET /_/api/v1/apps", s.auth(s.apps))
	mux.HandleFunc("GET /_/api/v1/apps/{slug}", s.auth(s.info))
	mux.HandleFunc("GET /_/api/v1/apps/{slug}/versions", s.auth(s.versions))
	mux.HandleFunc("POST /_/api/v1/apps/{slug}/deploys", s.auth(s.deploy))
	mux.HandleFunc("POST /_/api/v1/apps/{slug}/data", s.auth(s.data))
	mux.HandleFunc("DELETE /_/api/v1/apps/{slug}/data", s.auth(s.data))
	mux.HandleFunc("POST /_/api/v1/apps/{slug}/promote", s.auth(s.promote))
	mux.HandleFunc("DELETE /_/api/v1/apps/{slug}", s.auth(s.deleteApp))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logw := &responseLog{ResponseWriter: w}
		state := &requestState{}
		r = r.WithContext(context.WithValue(r.Context(), stateKey{}, state))
		defer func() {
			status := logw.status
			if status == 0 {
				status = 200
			}
			client := ""
			if state.client != nil {
				client = state.client.ID
			}
			s.logger().Info("request",
				"method", r.Method, "host", r.Host, "path", r.URL.Path,
				"status", status, "bytes", logw.bytes,
				"duration_ms", time.Since(start).Milliseconds(), "client", client)
		}()
		w = logw
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Robots-Tag", "noindex")
		apps, _ := url.Parse(s.Config.AppsURL)
		admin, _ := url.Parse(s.Config.AdminURL)
		if apps == nil || !strings.EqualFold(r.Host, apps.Host) {
			if admin != nil && admin.Host != "" && strings.EqualFold(r.Host, admin.Host) {
				adminHandler.ServeHTTP(w, r)
			} else {
				http.Error(w, "Misdirected Request", 421)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_/api/v1/") {
			w.Header().Set("Cache-Control", "no-store")
			mux.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/_/auth" && (r.Method == "GET" || r.Method == "HEAD") {
			s.redeemView(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_p/") {
			s.previewContent(w, r)
			return
		}
		s.content(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, message, hint string) {
	writeJSON(w, status, api.Error{Error: api.ErrorDetail{Code: code, Message: message, Hint: hint}})
}
func (s *Server) internal(w http.ResponseWriter, err error) {
	s.logger().Error("request failed", "error", err)
	fail(w, 500, "internal_error", "服务器内部错误", "请稍后重试")
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			fail(w, 401, "unauthorized", "需要有效的 Agent token", "运行 tinyhost login 重新授权")
			return
		}
		c, err := s.Store.ClientByTokenHash(token.Hash(parts[1]))
		if err != nil {
			s.internal(w, err)
			return
		}
		if c == nil || c.RevokedAt != nil || (c.ExpiresAt != nil && *c.ExpiresAt <= s.now().Unix()) {
			fail(w, 401, "unauthorized", "token 无效、已撤销或已过期", "运行 tinyhost login 重新授权")
			return
		}
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		if s.Config.TrustedProxyHeader != "" {
			if value := r.Header.Get(s.Config.TrustedProxyHeader); value != "" {
				ip = value
			}
		}
		if err = s.Store.TouchClient(c.ID, ip); err != nil {
			s.internal(w, err)
			return
		}
		r.Context().Value(stateKey{}).(*requestState).client = c
		next(w, r)
	}
}
func client(r *http.Request) *store.Client {
	return r.Context().Value(stateKey{}).(*requestState).client
}
func (s *Server) event(r *http.Request, action, app string, detail any) {
	data, err := json.Marshal(detail)
	if err != nil {
		s.logger().Error("encode event", "error", err)
		return
	}
	e := Event{Actor: client(r).ID, Action: action, AppID: app, Detail: string(data)}
	if err = s.Store.AddEvent(e.Actor, e.Action, e.AppID, e.Detail); err != nil {
		s.logger().Error("record event", "error", err)
		return
	}
	if s.OnEvent != nil {
		s.OnEvent(e)
	}
}
