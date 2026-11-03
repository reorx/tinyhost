package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/slug"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
	"github.com/reorx/tinyhost/internal/version"
	"github.com/reorx/tinyhost/skill"
)

type AppRow struct {
	*store.App
	CreatorName string
}
type VersionRow struct {
	*store.Version
	PublisherName, URL string
	Current            bool
}
type OverlayRow struct {
	store.Overlay
	PublisherName string
}
type AppView struct {
	App                  *store.App
	MainURL, CreatorName string
	Versions             []VersionRow
	Overlays             []OverlayRow
	Manifest             bundle.Manifest
	Events               []store.AuditEvent
}
type TrashRow struct {
	*store.App
	DaysRemaining int
}
type TrashView struct{ Apps []TrashRow }
type AgentRow struct {
	ID, Name, Status, LastUsedIP     string
	CreatedAt                        int64
	ExpiresAt, RevokedAt, LastUsedAt *int64
}
type AgentsView struct {
	Agents            []AgentRow
	Token, IssuedName string
	ExpiresAt         *int64
	// Quickstart is what the Owner pastes to an Agent. JS fills the name into
	// QuickstartNamed at QuickstartPlaceholder, so there is one copy of the text.
	Quickstart, QuickstartNamed, QuickstartPlaceholder string
	InstallUnavailable                                 bool
}
type EventsView struct {
	Events    []store.AuditEvent
	Apps      []*store.App
	AppFilter string
}

func (s *Server) consoleForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		s.adminError(w, "无效表单", 400)
		return false
	}
	return true
}
func (s *Server) consoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrAppNotFound), errors.Is(err, store.ErrVersionNotFound), errors.Is(err, store.ErrOverlayNotFound):
		s.adminError(w, err.Error(), 404)
	case errors.Is(err, store.ErrSlugTaken), errors.Is(err, store.ErrCurrentVersion), errors.Is(err, store.ErrOverlayConflict):
		s.adminError(w, err.Error(), 409)
	case errors.Is(err, store.ErrInvalidInput):
		s.adminError(w, err.Error(), 400)
	default:
		s.adminInternal(w, err)
	}
}
func (s *Server) creatorNames() (map[string]string, error) {
	clients, err := s.Store.Clients()
	if err != nil {
		return nil, err
	}
	names := map[string]string{"owner": "Owner"}
	for _, c := range clients {
		names[c.ID] = c.Name
	}
	return names, nil
}
func actorName(names map[string]string, id string) string {
	if n, ok := names[id]; ok {
		return n
	}
	return id
}
func (s *Server) consoleHome(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.ListApps()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	pending, err := s.Store.PendingDevices(s.now().Unix())
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	names, err := s.creatorNames()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	view := HomeView{Ready: true, Pending: pending, Apps: []AppRow{}}
	for _, a := range apps {
		view.Apps = append(view.Apps, AppRow{a, actorName(names, a.CreatedBy)})
	}
	s.render(w, "home.html", view)
}
func (s *Server) consoleCreateApp(w http.ResponseWriter, r *http.Request) {
	if !s.consoleForm(w, r) {
		return
	}
	name := r.PostForm.Get("slug")
	visibility := r.PostForm.Get("visibility")
	if visibility == "" {
		visibility = "private"
	}
	title := r.PostForm.Get("title")
	if !slug.Valid(name) || (visibility != "public" && visibility != "private") || len(title) > 1024 {
		s.adminError(w, "slug、标题或可见性不合法", 400)
		return
	}
	a, err := s.Store.CreateApp(name, title, visibility, "owner")
	if err != nil {
		owner, lookupErr := s.Store.SlugOwner(name)
		if lookupErr != nil {
			s.adminInternal(w, lookupErr)
		} else if owner != nil {
			s.adminError(w, "slug 已被占用", 409)
		} else {
			s.adminInternal(w, err)
		}
		return
	}
	s.ownerAppEvent("app.created", a.ID, map[string]any{"slug": name})
	http.Redirect(w, r, "/apps/"+a.Slug, 302)
}
func (s *Server) consoleApp(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a := s.consoleFindApp(w, r)
	if a == nil {
		return
	}
	versions, err := s.Store.Versions(a.ID)
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	names, err := s.creatorNames()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	events, err := s.Store.Events(a.ID)
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	view := AppView{App: a, MainURL: s.mainURL(a.Slug), CreatorName: actorName(names, a.CreatedBy), Events: events, Versions: []VersionRow{}, Overlays: []OverlayRow{}}
	for _, v := range versions {
		current := a.CurrentVersion != nil && *a.CurrentVersion == v.Number
		view.Versions = append(view.Versions, VersionRow{v, actorName(names, v.CreatedBy), fmt.Sprintf("%s@v%d/", strings.TrimSuffix(view.MainURL, "/"), v.Number), current})
	}
	if a.CurrentVersion != nil {
		v, err := s.Store.ResolveVersion(a.ID, 0)
		if err != nil {
			s.consoleError(w, err)
			return
		}
		view.Manifest = v.Manifest
		for _, o := range v.Overlays {
			view.Overlays = append(view.Overlays, OverlayRow{o, actorName(names, o.PushedBy)})
		}
	}
	s.render(w, "app.html", view)
}
func (s *Server) consoleAppAction(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if !s.consoleForm(w, r) {
		return
	}
	a := s.consoleFindApp(w, r)
	if a == nil {
		return
	}
	target := "/apps/" + a.Slug
	action := ""
	detail := map[string]any{"slug": a.Slug}
	var err error
	switch {
	case strings.HasSuffix(r.URL.Path, "/visibility"):
		visibility := r.PostForm.Get("visibility")
		err = s.Store.SetVisibility(a.ID, visibility)
		action = "app.visibility_changed"
		detail["visibility"] = visibility
	case strings.HasSuffix(r.URL.Path, "/rename"):
		name := r.PostForm.Get("slug")
		err = s.Store.RenameApp(a.ID, name)
		action = "app.renamed"
		detail["new_slug"] = name
		target = "/apps/" + name
	case strings.HasSuffix(r.URL.Path, "/promote"):
		n, parseErr := strconv.Atoi(r.PostForm.Get("version"))
		if parseErr != nil || n <= 0 {
			s.adminError(w, "无效版本号", 400)
			return
		}
		err = s.Store.Promote(a.ID, n)
		action = "app.promoted"
		detail["version"] = n
	case r.PathValue("n") != "":
		n, parseErr := strconv.Atoi(r.PathValue("n"))
		if parseErr != nil || n <= 0 {
			s.adminError(w, "无效版本号", 400)
			return
		}
		err = s.Store.DeleteVersion(a.ID, n)
		action = "app.version_deleted"
		detail["version"] = n
	case strings.HasSuffix(r.URL.Path, "/data/remove"):
		dir, parseErr := store.NormalizeOverlayDir(r.PostForm.Get("dir"))
		if parseErr != nil {
			s.adminError(w, parseErr.Error(), 400)
			return
		}
		var v *store.Version
		v, err = s.Store.CommitDataVersion(a.ID, dir, nil, "", "owner")
		action = "data.removed"
		detail["dir"] = dir
		if v != nil {
			detail["version"] = v.Number
		}
	case strings.HasSuffix(r.URL.Path, "/delete"):
		err = s.Store.SoftDeleteApp(a.ID)
		action = "app.deleted"
		target = "/trash"
	}
	if err != nil {
		s.consoleError(w, err)
		return
	}
	s.ownerAppEvent(action, a.ID, detail)
	http.Redirect(w, r, target, 302)
}
func (s *Server) consoleTrash(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.TrashApps()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	view := TrashView{Apps: []TrashRow{}}
	for _, a := range apps {
		seconds := *a.DeletedAt + int64(s.trashDays())*86400 - s.now().Unix()
		days := max(0, int((seconds+86399)/86400))
		view.Apps = append(view.Apps, TrashRow{a, days})
	}
	s.render(w, "trash.html", view)
}
func (s *Server) consoleTrashAction(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a, err := s.Store.AppByID(r.PathValue("id"))
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	if a == nil || a.DeletedAt == nil {
		s.adminError(w, "页面不存在", 404)
		return
	}
	action := "app.restored"
	target := "/apps/" + a.Slug
	if strings.HasSuffix(r.URL.Path, "/purge") {
		err = s.Store.PurgeApp(a.ID)
		action = "app.purged"
		target = "/trash"
	} else {
		err = s.Store.RestoreApp(a.ID)
	}
	if err != nil {
		s.consoleError(w, err)
		return
	}
	s.ownerAppEvent(action, a.ID, map[string]any{"slug": a.Slug})
	http.Redirect(w, r, target, 302)
}
func (s *Server) agentRows() ([]AgentRow, error) {
	clients, err := s.Store.Clients()
	if err != nil {
		return nil, err
	}
	rows := []AgentRow{}
	for _, c := range clients {
		status := "有效"
		if c.RevokedAt != nil {
			status = "已撤销"
		} else if c.ExpiresAt != nil && *c.ExpiresAt <= s.now().Unix() {
			status = "已过期"
		}
		rows = append(rows, AgentRow{c.ID, c.Name, status, c.LastUsedIP, c.CreatedAt, c.ExpiresAt, c.RevokedAt, c.LastUsedAt})
	}
	return rows, nil
}
func (s *Server) agentsView(rows []AgentRow) AgentsView {
	return AgentsView{
		Agents:                rows,
		Quickstart:            skill.Quickstart(s.Config.AppsURL, ""),
		QuickstartNamed:       skill.Quickstart(s.Config.AppsURL, skill.QuickstartNamePlaceholder),
		QuickstartPlaceholder: skill.QuickstartNamePlaceholder,
		InstallUnavailable:    !skill.InstallAvailable(version.Version, s.Config.DLDir != ""),
	}
}
func (s *Server) consoleAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.agentRows()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	s.render(w, "agents.html", s.agentsView(rows))
}
func (s *Server) consoleIssueToken(w http.ResponseWriter, r *http.Request) {
	if !s.consoleForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" || len(name) > 256 {
		s.adminError(w, "需要名称（最多 256 字节）", 400)
		return
	}
	var expires *int64
	seconds := int64(30 * 86400)
	switch r.PostForm.Get("ttl") {
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
		deadline := s.now().Unix() + seconds
		expires = &deadline
	}
	plain := token.New()
	c, err := s.Store.CreateClient(name, token.Hash(plain), expires)
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	s.ownerEvent("agent.issued", map[string]any{"client_id": c.ID, "name": c.Name})
	rows, err := s.agentRows()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	view := s.agentsView(rows)
	view.Token, view.IssuedName, view.ExpiresAt = plain, c.Name, expires
	s.render(w, "agents.html", view)
}
func (s *Server) consoleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	clients, err := s.Store.Clients()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	var found *store.Client
	for _, c := range clients {
		if c.ID == r.PathValue("id") {
			found = c
			break
		}
	}
	if found == nil {
		s.adminError(w, "页面不存在", 404)
		return
	}
	if err = s.Store.RevokeClient(found.ID); err != nil {
		s.adminInternal(w, err)
		return
	}
	s.ownerEvent("agent.revoked", map[string]any{"client_id": found.ID, "name": found.Name})
	http.Redirect(w, r, "/agents", 302)
}
func (s *Server) consoleEvents(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("app")
	events, err := s.Store.Events(filter)
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	apps, err := s.Store.ListApps()
	if err != nil {
		s.adminInternal(w, err)
		return
	}
	s.render(w, "events.html", EventsView{Events: events, Apps: apps, AppFilter: filter})
}
