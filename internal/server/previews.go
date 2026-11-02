package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
)

func (s *Server) previewURL(p *store.Preview) string {
	return strings.TrimRight(s.Config.AppsURL, "/") + "/_p/" + p.Token + "/"
}

func (s *Server) createPreview(w http.ResponseWriter, r *http.Request) {
	ttl := 24 * time.Hour
	if raw := r.URL.Query().Get("ttl"); raw != "" {
		var err error
		ttl, err = time.ParseDuration(raw)
		if err != nil || ttl < time.Hour || ttl > 168*time.Hour {
			fail(w, 400, "invalid_ttl", "预览有效期必须在 1h–168h 之间", "使用 --ttl 24h")
			return
		}
	}
	key := r.URL.Query().Get("key")
	if r.URL.Query().Get("new") == "1" {
		key = token.New()
	}
	if key == "" || len(key) > 1024 || len(r.URL.Query().Get("source")) > 4096 {
		fail(w, 400, "invalid_preview", "需要 key，且 key/source 不得超过 1024/4096 字节", "为本地目录生成稳定随机 key")
		return
	}
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	layer, _, ok := s.unpack(w, r, "")
	if !ok {
		return
	}
	now := s.now()
	p, reused, err := s.Store.CommitPreview(client(r).ID, key, r.URL.Query().Get("source"), layer, now.Unix(), now.Add(ttl).Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, 201, api.PreviewResult{ID: p.ID, URL: s.previewURL(p), ExpiresAt: p.ExpiresAt, Reused: reused})
}

func (s *Server) listPreviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Previews(client(r).ID, s.now().Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := api.PreviewsResult{Previews: []api.Preview{}}
	for _, p := range rows {
		out.Previews = append(out.Previews, api.Preview{ID: p.ID, URL: s.previewURL(p), Source: p.Source, ExpiresAt: p.ExpiresAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt})
	}
	writeJSON(w, 200, out)
}

func (s *Server) deletePreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// A URL contains the token rather than the ID. Resolve it here so deleting
	// an expired link does not depend on the active-only listing endpoint.
	if !strings.HasPrefix(id, "p_") {
		p, err := s.Store.PreviewByToken(id)
		if err != nil {
			s.internal(w, err)
			return
		}
		if p != nil && p.ClientID == client(r).ID {
			id = p.ID
		}
	}
	deleted, err := s.Store.DeletePreview(id, client(r).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !deleted {
		fail(w, 404, "preview_not_found", "预览不存在", "运行 tinyhost previews 查看自己的预览")
		return
	}
	writeJSON(w, 200, api.DeletePreviewResult{ID: id, Deleted: true})
}

func (s *Server) previewContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	value, rest, slash := strings.Cut(strings.TrimPrefix(r.URL.Path, "/_p/"), "/")
	p, err := s.Store.PreviewByToken(value)
	if err != nil {
		s.internal(w, err)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	if p.ExpiresAt <= s.now().Unix() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		if r.Method != "HEAD" {
			s.render(w, "preview-expired.html", ExpiredPreviewView{ExpiresAt: p.ExpiresAt})
		}
		return
	}
	if !slash {
		location := r.URL.EscapedPath() + "/"
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, 301)
		return
	}
	if p.Layer == nil {
		http.NotFound(w, r)
		return
	}
	s.resolveContent(w, r, &store.Version{BaseLayer: *p.Layer}, rest, 0)
}

type ExpiredPreviewView struct{ ExpiresAt int64 }
type PreviewRow struct {
	*store.Preview
	URL, CreatorName string
	Expired          bool
}
type PreviewsView struct{ Previews []PreviewRow }

func (s *Server) consolePreviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Previews("", s.now().Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	names, err := s.creatorNames()
	if err != nil {
		s.internal(w, err)
		return
	}
	view := PreviewsView{Previews: []PreviewRow{}}
	for _, p := range rows {
		view.Previews = append(view.Previews, PreviewRow{Preview: p, URL: s.previewURL(p), CreatorName: actorName(names, p.ClientID), Expired: p.ExpiresAt <= s.now().Unix()})
	}
	s.render(w, "previews.html", view)
}

func (s *Server) consoleDeletePreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleted, err := s.Store.DeletePreview(id, "")
	if err != nil {
		s.internal(w, err)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}
	s.ownerEvent("preview.deleted", map[string]string{"preview_id": id})
	http.Redirect(w, r, "/previews", 302)
}
