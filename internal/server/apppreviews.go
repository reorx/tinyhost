package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/store"
)

func (s *Server) appPreviewDTO(a *store.App, p *store.AppPreview) api.AppPreview {
	return api.AppPreview{Hash: p.Hash, URL: strings.TrimSuffix(s.mainURL(a.Slug), "/") + "@" + p.Hash + "/",
		Message: p.Message, CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		ExpiresAt: p.ExpiresAt, PublishedVersion: p.PublishedVersion}
}

func (s *Server) createAppPreview(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	ttl, ok := previewTTL(w, r)
	if !ok {
		return
	}
	msg := r.URL.Query().Get("message")
	if !validMessage(w, msg) {
		return
	}
	layer, summary, ok := s.unpack(w, r, "")
	if !ok {
		return
	}
	now := s.now()
	p, reused, err := s.Store.CommitAppPreview(a.ID, layer, summary.Manifest, msg, client(r).ID, now.Unix(), now.Add(ttl).Unix())
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.previewed", a.ID, map[string]any{"preview": p.Hash, "reused": reused})
	latest, err := s.Store.AppByID(a.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	if latest == nil || latest.DeletedAt != nil {
		s.mutationError(w, a, store.ErrAppNotFound)
		return
	}
	writeJSON(w, 201, api.AppPreviewResult{AppPreview: s.appPreviewDTO(latest, p), App: latest.Slug, CurrentVersion: latest.CurrentVersion, Reused: reused})
}

func (s *Server) listAppPreviews(w http.ResponseWriter, r *http.Request) {
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	rows, err := s.Store.AppPreviews(a.ID, s.now().Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := api.AppPreviewsResult{Previews: []api.AppPreview{}}
	for _, p := range rows {
		out.Previews = append(out.Previews, s.appPreviewDTO(a, p))
	}
	writeJSON(w, 200, out)
}

func (s *Server) appPreviewContent(w http.ResponseWriter, r *http.Request, a *store.App, hash, rest string) {
	if a.Visibility == "public" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	p, err := s.Store.AppPreview(a.ID, hash)
	if errors.Is(err, store.ErrPreviewNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	if p.ExpiresAt <= s.now().Unix() {
		s.expiredPreview(w, r, p.ExpiresAt, a.Slug)
		return
	}
	if p.Layer == nil {
		http.NotFound(w, r)
		return
	}
	v := &store.Version{BaseLayer: *p.Layer}
	if a.CurrentVersion != nil {
		current, err := s.Store.ResolveVersion(a.ID, 0)
		if err != nil {
			s.mutationError(w, a, err)
			return
		}
		v.Overlays = current.Overlays
	}
	s.resolveContent(w, r, v, rest, 0)
}

func readMutationJSON(w http.ResponseWriter, r *http.Request, req any, optional bool) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err == nil && optional && len(body) == 0 {
		return true
	}
	body = bytes.TrimSpace(body)
	if err != nil || len(body) == 0 || body[0] != '{' || json.Unmarshal(body, req) != nil {
		fail(w, 400, "bad_request", "请求体必须是一个 JSON 对象", "最多 4096 字节")
		return false
	}
	return true
}

func validMessage(w http.ResponseWriter, msg string) bool {
	if len(msg) > 1024 {
		fail(w, 400, "bad_request", "说明超过 1024 字节", "请缩短说明")
		return false
	}
	return true
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	var req api.RollbackRequest
	if !readMutationJSON(w, r, &req, false) || !validMessage(w, req.Message) {
		return
	}
	v, err := s.Store.CommitRollback(a.ID, req.Version, req.Message, client(r).ID)
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.rolled_back", a.ID, map[string]any{"version": v.Number, "rollback_of": req.Version, "message": v.Message})
	s.deployedLayer(w, a, v)
}

func (s *Server) publishAppPreview(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	var req api.PublishRequest
	if !readMutationJSON(w, r, &req, true) || !validMessage(w, req.Message) {
		return
	}
	hash := r.PathValue("hash")
	v, err := s.Store.PublishAppPreview(a.ID, hash, req.Message, client(r).ID, s.now().Unix())
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.preview_published", a.ID, map[string]any{"version": v.Number, "preview": hash, "message": v.Message})
	s.deployedLayer(w, a, v)
}

func (s *Server) deployedLayer(w http.ResponseWriter, a *store.App, v *store.Version) {
	files, size, err := s.Store.LayerStats(v.BaseLayer)
	if err != nil {
		s.internal(w, err)
		return
	}
	s.deployed(w, a, v, bundle.Summary{Files: files, Size: size}, true)
}
