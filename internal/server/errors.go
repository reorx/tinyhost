package server

import (
	"github.com/reorx/tinyhost/internal/store"
	"net/http"
)

func (s *Server) consoleFindApp(w http.ResponseWriter, r *http.Request) *store.App {
	app, err := s.Store.AppBySlug(r.PathValue("slug"))
	if err != nil {
		s.adminInternal(w, err)
		return nil
	}
	if app == nil || app.DeletedAt != nil {
		s.adminError(w, "App 不存在或已删除", 404)
		return nil
	}
	return app
}

type ErrorView struct {
	Status                      int
	Title, Description, BackURL string
}

func (s *Server) adminError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "error.html", ErrorView{Status: status, Title: http.StatusText(status), Description: message, BackURL: "/"})
}
func (s *Server) adminInternal(w http.ResponseWriter, err error) {
	s.logger().Error("admin request failed", "error", err)
	s.adminError(w, "服务器内部错误，请稍后重试", 500)
}
