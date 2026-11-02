package server

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/slug"
	"github.com/reorx/tinyhost/internal/store"
)

func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	if r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("这是一个 tinyhost 服务，Agent 请阅读 /skill.md\n"))
		return
	}
	if r.URL.Path == "/robots.txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	segment, rest, hasSlash := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	name, number, err := slug.ParseSegment(segment)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !hasSlash {
		location := r.URL.EscapedPath() + "/"
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, 301)
		return
	}
	a, err := s.Store.SlugOwner(name)
	if err != nil {
		s.internal(w, err)
		return
	}
	if a == nil || a.DeletedAt != nil {
		http.NotFound(w, r)
		return
	}
	if a.Slug != name {
		newSegment := a.Slug
		if number != 0 {
			newSegment += fmt.Sprintf("@v%d", number)
		}
		_, escapedRest, _ := strings.Cut(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
		location := "/" + newSegment + "/" + escapedRest
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, http.StatusMovedPermanently)
		return
	}
	// Authorization precedes version and file lookup to avoid disclosing private metadata.
	if a.Visibility != "public" {
		w.Header().Set("Cache-Control", "no-store")
		ok, err := s.hasSession(r, "viewer")
		if err != nil {
			s.internal(w, err)
			return
		}
		if !ok {
			if r.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, strings.TrimRight(s.Config.AdminURL, "/")+"/view-auth?next="+url.QueryEscape(r.URL.RequestURI()), 302)
			} else {
				http.Error(w, "需要登录", 401)
			}
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	v, err := s.Store.ResolveVersion(a.ID, number)
	if err != nil {
		if errors.Is(err, store.ErrVersionNotFound) || errors.Is(err, store.ErrAppNotFound) {
			if number == 0 && a.CurrentVersion == nil {
				http.Error(w, "尚未发布", 404)
			} else {
				http.NotFound(w, r)
			}
		} else {
			s.internal(w, err)
		}
		return
	}
	// URL.Path is already decoded by net/http; never decode a second time.
	// Reject traversal instead of normalizing across the app's URL boundary.
	if strings.ContainsAny(rest, "\\\x00") {
		http.NotFound(w, r)
		return
	}
	for _, part := range strings.Split(rest, "/") {
		if part == ".." || (strings.HasPrefix(part, ".") && part != ".well-known") {
			http.NotFound(w, r)
			return
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+rest), "/")
	if clean == "tinyhost.json" {
		http.NotFound(w, r)
		return
	}
	if rest == "" || strings.HasSuffix(rest, "/") {
		clean = path.Join(clean, "index.html")
	}
	file, err := s.Store.LookupVersionFile(v, clean)
	if err != nil {
		s.internal(w, err)
		return
	}
	if file == nil && rest != "" && !strings.HasSuffix(rest, "/") {
		file, err = s.Store.LookupVersionFile(v, clean+".html")
		if err != nil {
			s.internal(w, err)
			return
		}
		if file == nil {
			file, err = s.Store.LookupVersionFile(v, clean+"/index.html")
			if err != nil {
				s.internal(w, err)
				return
			}
			if file != nil {
				location := r.URL.EscapedPath() + "/"
				if r.URL.RawQuery != "" {
					location += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, location, 301)
				return
			}
		}
	}
	status := 200
	if file == nil {
		file, err = s.Store.LookupVersionFile(v, "404.html")
		if err != nil {
			s.internal(w, err)
			return
		}
		if file == nil {
			http.NotFound(w, r)
			return
		}
		status = 404
	}
	s.serveFile(w, r, file, number, status)
}
func contentType(name string) string {
	extra := map[string]string{
		".js": "text/javascript", ".mjs": "text/javascript", ".json": "application/json",
		".wasm": "application/wasm", ".webmanifest": "application/manifest+json",
		".md": "text/markdown", ".csv": "text/csv",
	}
	ext := strings.ToLower(path.Ext(name))
	value := extra[ext]
	if value == "" {
		value = mime.TypeByExtension(ext)
	}
	if value == "" {
		value = "application/octet-stream"
	}
	if (strings.HasPrefix(value, "text/") || value == "application/json" || value == "application/manifest+json") && !strings.Contains(value, "charset=") {
		value += "; charset=utf-8"
	}
	return value
}
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, file *store.File, number, status int) {
	data, err := s.Blobs.Open(file.Blob)
	if err != nil {
		s.internal(w, err)
		return
	}
	defer data.Close()
	w.Header().Set("Content-Type", contentType(file.Path))
	w.Header().Set("ETag", `"`+file.Blob+`"`)
	if w.Header().Get("Cache-Control") == "private, no-cache" {
		// Viewer-authorized responses must never enter shared caches.
	} else if number == 0 {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if status != 200 {
		// A custom error document remains a 404 for conditional and range requests.
		if w.Header().Get("Cache-Control") != "private, no-cache" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.WriteHeader(status)
		if r.Method != "HEAD" {
			_, _ = data.WriteTo(w)
		}
		return
	}
	http.ServeContent(w, r, file.Path, time.Time{}, data)
}
