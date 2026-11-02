package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/slug"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/version"
)

func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	c := client(r)
	writeJSON(w, 200, api.WhoamiResult{
		Client:        api.Client{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt},
		ServerVersion: version.Version,
	})
}
func (s *Server) appDTO(a *store.App) api.App {
	return api.App{
		ID: a.ID, Slug: a.Slug, Title: a.Title, Visibility: a.Visibility,
		CreatedBy: a.CreatedBy, CurrentVersion: a.CurrentVersion,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, MainURL: s.mainURL(a.Slug),
	}
}
func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.ListApps()
	if err != nil {
		s.internal(w, err)
		return
	}
	out := api.AppsResult{Apps: []api.App{}}
	for _, a := range apps {
		out.Apps = append(out.Apps, s.appDTO(a))
	}
	writeJSON(w, 200, out)
}
func (s *Server) findApp(w http.ResponseWriter, r *http.Request) *store.App {
	a, err := s.Store.AppBySlug(r.PathValue("slug"))
	if err != nil {
		s.internal(w, err)
		return nil
	}
	if a == nil || a.DeletedAt != nil {
		fail(w, 404, "app_not_found", "App 不存在或已删除", "新建请加 --create")
		return nil
	}
	return a
}
func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	out := api.AppDetail{App: s.appDTO(a), Overlays: []api.Overlay{}}
	if a.CurrentVersion != nil {
		v, err := s.Store.ResolveVersion(a.ID, 0)
		if err != nil {
			s.internal(w, err)
			return
		}
		out.Manifest = v.Manifest
		for _, o := range v.Overlays {
			out.Overlays = append(out.Overlays, api.Overlay{Dir: o.Dir, PushedBy: o.PushedBy, PushedAt: o.PushedAt})
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	versions, err := s.Store.Versions(a.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := api.VersionsResult{Versions: []api.Version{}}
	for _, v := range versions {
		current := a.CurrentVersion != nil && *a.CurrentVersion == v.Number
		out.Versions = append(out.Versions, api.Version{
			Number: v.Number, Kind: v.Kind, DataDir: v.DataDir, Message: v.Message,
			CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, Current: current,
		})
	}
	writeJSON(w, 200, out)
}
func (s *Server) taken(w http.ResponseWriter, a *store.App) {
	deleted := a.DeletedAt != nil
	writeJSON(w, 409, api.Error{Error: api.ErrorDetail{
		Code: "slug_taken", Message: "slug 已被占用，创建者：" + a.CreatedBy,
		Hint:      "更换名字，或去掉 --create 发布新版本",
		CreatedBy: a.CreatedBy, CreatedAt: &a.CreatedAt, Deleted: &deleted,
	}})
}
func (s *Server) unpack(w http.ResponseWriter, r *http.Request, dir string) (*store.Layer, bundle.Summary, bool) {
	limits := bundle.Limits{RequireIndex: dir == "", MaxFiles: s.Config.MaxFiles}
	mb := s.Config.MaxUploadMB
	if mb == 0 {
		mb = 50
	}
	limits.MaxSize = int64(mb) << 20
	// Bound compressed bytes too; allow tar metadata and gzip overhead.
	count := limits.MaxFiles
	if count == 0 {
		count = 1000
	}
	r.Body = http.MaxBytesReader(w, r.Body, limits.MaxSize+int64(count)*2048+(2<<20))
	layer := &store.Layer{}
	summary, err := bundle.Unpack(r.Body, limits, func(p string, content io.Reader) error {
		hash, size, err := s.Blobs.Put(content)
		if err != nil {
			return err
		}
		layer.Files = append(layer.Files, store.File{Path: dir + p, Blob: hash, Size: size})
		return nil
	})
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.Is(err, bundle.ErrTooLarge) || errors.As(err, &maxErr) {
			fail(w, 413, "too_large", err.Error(), "减小上传大小或文件数量")
		} else {
			fail(w, 400, "bad_bundle", err.Error(), "上传含 index.html 的安全 tar.gz；数据包不要求 index.html")
		}
		return nil, summary, false
	}
	return layer, summary, true
}
func (s *Server) deploy(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	name := r.PathValue("slug")
	if !slug.Valid(name) {
		fail(w, 400, "slug_invalid", "slug 不合法", "使用 3–40 位小写字母、数字、连字符，首尾为字母或数字，不能含 -- 或保留字")
		return
	}
	q := r.URL.Query()
	a, err := s.Store.AppBySlug(name)
	if err != nil {
		s.internal(w, err)
		return
	}
	create := q.Get("create") == "1"
	if create {
		a, err = s.Store.SlugOwner(name)
		if err != nil {
			s.internal(w, err)
			return
		}
	}
	if create && a != nil {
		s.taken(w, a)
		return
	}
	if !create && (a == nil || a.DeletedAt != nil) {
		fail(w, 404, "app_not_found", "App 不存在或已删除", "新建请加 --create")
		return
	}
	visibility := q.Get("visibility")
	if visibility == "" {
		visibility = "private"
	}
	if visibility != "private" && visibility != "public" {
		fail(w, 400, "bad_request", "visibility 不合法", "使用 public 或 private")
		return
	}
	layer, summary, ok := s.unpack(w, r, "")
	if !ok {
		return
	}
	if q.Has("title") {
		summary.Manifest.Title = q.Get("title")
	}
	if create {
		a, err = s.Store.CreateApp(name, summary.Manifest.Title, visibility, client(r).ID)
		if err != nil {
			existing, lookupErr := s.Store.SlugOwner(name)
			if lookupErr == nil && existing != nil {
				s.taken(w, existing)
			} else {
				s.internal(w, err)
			}
			return
		}
		s.event(r, "app.created", a.ID, map[string]any{"slug": name})
	}
	promote := q.Get("promote") != "0"
	v, err := s.Store.CommitAppVersion(a.ID, layer, summary.Manifest, q.Get("message"), client(r).ID, promote)
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.deployed", a.ID, map[string]any{"version": v.Number, "message": v.Message})
	s.deployed(w, a, v, summary, promote)
}
func (s *Server) data(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	dir, err := store.NormalizeOverlayDir(r.URL.Query().Get("dir"))
	if err != nil {
		fail(w, 400, "overlay_conflict", err.Error(), "使用非根目录，不得含点开头的路径段")
		return
	}
	var layer *store.Layer
	var summary bundle.Summary
	if r.Method == "POST" {
		var ok bool
		layer, summary, ok = s.unpack(w, r, dir)
		if !ok {
			return
		}
	}
	v, err := s.Store.CommitDataVersion(a.ID, dir, layer, r.URL.Query().Get("message"), client(r).ID)
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	action := "data.pushed"
	if layer == nil {
		action = "data.removed"
	}
	s.event(r, action, a.ID, map[string]any{"version": v.Number, "dir": dir})
	s.deployed(w, a, v, summary, true)
}
func (s *Server) mutationError(w http.ResponseWriter, a *store.App, err error) {
	switch {
	case errors.Is(err, store.ErrAppNotFound):
		fail(w, 404, "app_not_found", "App 不存在或已删除", "检查 slug")
	case errors.Is(err, store.ErrVersionNotFound):
		fail(w, 404, "version_not_found", "版本不存在或已清理", "查询 versions")
	case errors.Is(err, store.ErrOverlayNotFound):
		fail(w, 404, "overlay_not_found", "覆盖目录不存在", "查询 App 详情")
	case errors.Is(err, store.ErrOverlayConflict):
		dirs := []string{}
		v, lookupErr := s.Store.ResolveVersion(a.ID, 0)
		if lookupErr != nil {
			s.internal(w, lookupErr)
			return
		}
		for _, o := range v.Overlays {
			dirs = append(dirs, o.Dir)
		}
		writeJSON(w, 409, api.Error{Error: api.ErrorDetail{Code: "overlay_conflict", Message: err.Error(), Hint: "覆盖目录不能互相嵌套", ExistingDirs: dirs}})
	default:
		s.internal(w, err)
	}
}
func (s *Server) deployed(w http.ResponseWriter, a *store.App, v *store.Version, summary bundle.Summary, promoted bool) {
	latest, err := s.Store.AppBySlug(a.Slug)
	if err != nil {
		s.internal(w, err)
		return
	}
	skipped := summary.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	writeJSON(w, 201, api.DeployResult{
		App: a.Slug, Version: v.Number, Kind: v.Kind, DataDir: v.DataDir,
		Files: summary.Files, Size: summary.Size,
		VersionURL: fmt.Sprintf("%s@v%d/", strings.TrimSuffix(s.mainURL(a.Slug), "/"), v.Number),
		MainURL:    s.mainURL(a.Slug), Promoted: promoted, CurrentVersion: latest.CurrentVersion,
		AppCreatedBy: a.CreatedBy, Skipped: skipped,
	})
}
func (s *Server) promote(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	var req api.PromoteRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		fail(w, 400, "bad_request", "无效的 JSON", "使用 {\"version\":3}")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		fail(w, 400, "bad_request", "JSON 后有多余数据", "只发送一个 JSON 对象")
		return
	}
	if err := s.Store.Promote(a.ID, req.Version); err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.promoted", a.ID, req)
	writeJSON(w, 200, api.PromoteResult{App: a.Slug, CurrentVersion: req.Version, MainURL: s.mainURL(a.Slug)})
}
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.findApp(w, r)
	if a == nil {
		return
	}
	if a.CreatedBy != client(r).ID {
		fail(w, 403, "not_creator", "只能删除自己创建的 App，创建者："+a.CreatedBy, "联系创建者或 Owner")
		return
	}
	if err := s.Store.SoftDeleteApp(a.ID); err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.event(r, "app.deleted", a.ID, map[string]any{"slug": a.Slug})
	writeJSON(w, 200, api.DeleteResult{App: a.Slug, Deleted: true, TrashDays: s.trashDays()})
}
