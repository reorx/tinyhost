package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
	"golang.org/x/crypto/bcrypt"
)

//go:embed views/*.html views/static/*
var viewFiles embed.FS
var adminTemplates = template.Must(template.ParseFS(viewFiles, "views/*.html"))

type LoginView struct{ Next, Error, AdminURL string }
type DeviceView struct {
	Request *store.DeviceRequest
	Message string
	// Info is Request.Meta decoded for display; zero when there is no request.
	Info DeviceInfo
	// Name and TTL echo the approve form on the decision page; empty on deny.
	Name, TTL string
}
type DeviceInfo struct {
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	CLIVersion string `json:"cli_version"`
	RequestIP  string `json:"request_ip"`
}
type HomeView struct {
	Message string
	Apps    []AppRow
	Pending []*store.DeviceRequest
	Ready   bool
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTemplates.ExecuteTemplate(w, name, data); err != nil {
		s.logger().Error("render template", "error", err)
	}
}
func (s *Server) cookieName(kind string) string {
	name := "th_admin"
	if kind == "viewer" {
		name = "th_view"
	}
	if !s.Config.InsecureCookies {
		name = "__Host-" + name
	}
	return name
}
func (s *Server) sessionCookie(w http.ResponseWriter, kind, value string, remove bool) {
	c := &http.Cookie{Name: s.cookieName(kind), Value: value, Path: "/", HttpOnly: true, Secure: !s.Config.InsecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 86400, Expires: s.now().Add(30 * 24 * time.Hour)}
	if remove {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}
func (s *Server) hasSession(r *http.Request, kind string) (bool, error) {
	c, err := r.Cookie(s.cookieName(kind))
	if err != nil {
		return false, nil
	}
	return s.Store.ValidSession(token.Hash(c.Value), kind, s.now().Unix())
}
func safeNext(next string) bool {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n\x00") {
		return false
	}
	u, err := url.Parse(next)
	return err == nil && u.Host == "" && u.Scheme == "" && u.Opaque == ""
}
func (s *Server) adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.adminLogout)
	mux.HandleFunc("POST /logout-all", s.adminLogout)
	mux.HandleFunc("GET /{$}", s.consoleHome)
	mux.HandleFunc("POST /apps", s.consoleCreateApp)
	mux.HandleFunc("GET /apps/{slug}", s.consoleApp)
	for _, action := range []string{"visibility", "rename", "promote", "versions/{n}/delete", "data/remove", "delete"} {
		mux.HandleFunc("POST /apps/{slug}/"+action, s.consoleAppAction)
	}
	mux.HandleFunc("GET /trash", s.consoleTrash)
	mux.HandleFunc("POST /trash/{id}/restore", s.consoleTrashAction)
	mux.HandleFunc("POST /trash/{id}/purge", s.consoleTrashAction)
	mux.HandleFunc("GET /agents", s.consoleAgents)
	mux.HandleFunc("POST /agents", s.consoleIssueToken)
	mux.HandleFunc("POST /agents/{id}/revoke", s.consoleRevokeAgent)
	mux.HandleFunc("GET /previews", s.consolePreviews)
	mux.HandleFunc("POST /previews/{id}/delete", s.consoleDeletePreview)
	mux.HandleFunc("GET /events", s.consoleEvents)
	mux.HandleFunc("GET /device", s.devicePage)
	mux.HandleFunc("POST /device/{id}/approve", s.deviceDecision)
	mux.HandleFunc("POST /device/{id}/deny", s.deviceDecision)
	mux.HandleFunc("GET /view-auth", s.viewAuth)
	static, _ := fs.Sub(viewFiles, "views/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	return s.adminMiddleware(mux)
}

// adminMiddleware wraps every admin route, including future console POST routes.
func (s *Server) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csp := "default-src 'self'; frame-ancestors 'none'; form-action 'self'"
		if r.URL.Path == "/login" {
			// Chromium applies form-action to the entire login redirect chain,
			// including the final apps-origin hop that redeems a viewer ticket.
			// AppsURL is a validated origin; keep this exception on login only.
			csp += " " + strings.TrimRight(s.Config.AppsURL, "/")
		}
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == "POST" {
			origin := r.Header.Get("Origin")
			if (origin != "" && origin != strings.TrimRight(s.Config.AdminURL, "/")) || (origin == "" && r.Header.Get("Sec-Fetch-Site") != "same-origin") {
				s.adminError(w, "Origin 不匹配", 403)
				return
			}
		}
		password, err := s.Store.Setting("owner_password_hash")
		if err != nil {
			s.adminInternal(w, err)
			return
		}
		if password == "" {
			// The setup page needs its own CSS and icons. Only canonical asset
			// paths pass, so nothing else is reachable through the mux.
			if (r.Method == "GET" || r.Method == "HEAD") && strings.HasPrefix(r.URL.Path, "/static/") && path.Clean(r.URL.Path) == r.URL.Path {
				next.ServeHTTP(w, r)
				return
			}
			s.render(w, "home.html", HomeView{Message: "请先在服务器上运行 tinyhost-server passwd"})
			return
		}
		if r.URL.Path != "/login" && !strings.HasPrefix(r.URL.Path, "/static/") {
			ok, err := s.hasSession(r, "admin")
			if err != nil {
				s.adminInternal(w, err)
				return
			}
			if !ok {
				if r.Method == "GET" || r.Method == "HEAD" {
					http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 302)
				} else {
					s.adminError(w, "需要登录", 401)
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login.html", LoginView{Next: r.URL.Query().Get("next"), AdminURL: s.Config.AdminURL})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allowRate("login:"+s.requestIP(r), 5) || !s.allowRate("login-global", 30) {
		s.adminError(w, "登录过于频繁", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		s.adminError(w, "无效表单", 400)
		return
	}
	hash, err := s.Store.Setting("owner_password_hash")
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.FormValue("password"))) != nil {
		w.WriteHeader(401)
		s.render(w, "login.html", LoginView{Next: r.FormValue("next"), Error: "密码错误", AdminURL: s.Config.AdminURL})
		return
	}
	plain := token.New()
	if err = s.Store.CreateSession(token.Hash(plain), "admin", s.requestIP(r), r.UserAgent(), s.now().Unix()); err != nil {
		s.adminInternal(w, err)
		return
	}
	s.sessionCookie(w, "admin", plain, false)
	next := r.FormValue("next")
	if !safeNext(next) {
		next = "/"
	}
	http.Redirect(w, r, next, 302)
}
func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	var err error
	if r.URL.Path == "/logout-all" {
		err = s.Store.DeleteAllSessions()
	} else if c, e := r.Cookie(s.cookieName("admin")); e == nil {
		err = s.Store.DeleteSession(token.Hash(c.Value))
	}
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	s.ownerEvent("owner.logged_out", nil)
	s.sessionCookie(w, "admin", "", true)
	http.Redirect(w, r, "/login", 302)
}
func (s *Server) ownerEvent(action string, detail any) {
	s.ownerAppEvent(action, "", detail)
}
func (s *Server) ownerAppEvent(action, app string, detail any) {
	// Reuse the event callback contract without requiring an Agent request context.
	data, err := json.Marshal(detail)
	if err != nil {
		s.logger().Error("encode owner event", "error", err)
		return
	}
	e := Event{Actor: "owner", Action: action, AppID: app, Detail: string(data)}
	if err := s.Store.AddEvent(e.Actor, e.Action, e.AppID, e.Detail); err != nil {
		s.logger().Error("record owner event", "error", err)
		return
	}
	if s.OnEvent != nil {
		s.OnEvent(e)
	}
}
func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(r.URL.Query().Get("code")))
	d, err := s.Store.DeviceByCode(code)
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	msg := ""
	if d == nil {
		msg = "验证码不存在"
	} else if d.ExpiresAt <= s.now().Unix() {
		msg = "申请已过期"
		d = nil
	} else if d.Status != "pending" {
		msg = "申请已处理：" + d.Status
		d = nil
	}
	view := DeviceView{Request: d, Message: msg}
	if d != nil {
		// Meta is written by deviceCode; a malformed value only leaves the fields blank.
		_ = json.Unmarshal([]byte(d.Meta), &view.Info)
	}
	s.render(w, "device.html", view)
}
func (s *Server) deviceDecision(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		s.adminError(w, "无效表单", 400)
		return
	}
	status := "denied"
	name := strings.TrimSpace(r.FormValue("name"))
	var ttl *int64
	if strings.HasSuffix(r.URL.Path, "/approve") {
		status = "approved"
		if name == "" || len(name) > 256 {
			s.adminError(w, "需要名称", 400)
			return
		}
		seconds := int64(30 * 86400)
		switch r.FormValue("ttl") {
		case "1d":
			seconds = 86400
		case "30d", "":
		case "forever":
			seconds = 0
		default:
			s.adminError(w, "无效有效期", 400)
			return
		}
		if seconds != 0 {
			ttl = &seconds
		}
	}
	if err := s.Store.DecideDevice(r.PathValue("id"), status, name, ttl, s.now().Unix()); err != nil {
		s.adminError(w, err.Error(), 400)
		return
	}
	s.ownerEvent("agent."+status, struct {
		RequestID string `json:"request_id"`
	}{r.PathValue("id")})
	view := DeviceView{Message: status}
	if status == "approved" {
		view.Name = name
		view.TTL = r.FormValue("ttl")
	}
	s.render(w, "device.html", view)
}
func (s *Server) viewAuth(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if !safeNext(next) {
		s.adminError(w, "无效 next", 400)
		return
	}
	plain := token.New()
	if err := s.Store.CreateTicket(token.Hash(plain), next, s.now().Unix()); err != nil {
		s.adminInternal(w, err)
		return
	}
	http.Redirect(w, r, strings.TrimRight(s.Config.AppsURL, "/")+"/_/auth?"+url.Values{"ticket": {plain}, "next": {next}}.Encode(), 302)
}
func (s *Server) redeemView(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	next := r.URL.Query().Get("next")
	if !safeNext(next) {
		http.Error(w, "无效 next", 400)
		return
	}
	ok, err := s.Store.ConsumeTicket(token.Hash(r.URL.Query().Get("ticket")), next, s.now().Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	if !ok {
		http.Error(w, "票据无效或过期", 400)
		return
	}
	plain := token.New()
	if err = s.Store.CreateSession(token.Hash(plain), "viewer", s.requestIP(r), r.UserAgent(), s.now().Unix()); err != nil {
		s.internal(w, err)
		return
	}
	s.sessionCookie(w, "viewer", plain, false)
	http.Redirect(w, r, next, 302)
}
