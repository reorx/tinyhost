package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/notify"
	"github.com/reorx/tinyhost/internal/server"
)

// Keep notification formatting and backend wiring outside the service core.
func notificationCallback(s *server.Server, h *notify.Hub) func(server.Event) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(e server.Event) {
		m, err := notificationMessage(s, e)
		if err != nil {
			logger.Error("prepare notification failed", "event", e.Action, "error", err)
			return
		}
		h.Publish(m)
	}
}
func notificationMessage(s *server.Server, e server.Event) (notify.Message, error) {
	var detail struct {
		Name, Hostname, IP, Code, URL, Message string
		Version                                int
	}
	if err := json.Unmarshal([]byte(e.Detail), &detail); err != nil {
		return notify.Message{}, err
	}
	m := notify.Message{Event: e.Action, Title: e.Action, Message: e.Detail, At: time.Now().Unix()}
	if e.Action == "device.requested" {
		m.Title = detail.Name + " 申请使用 tinyhost"
		m.Message = fmt.Sprintf("验证码：%s\n主机名：%s\nIP：%s", detail.Code, detail.Hostname, detail.IP)
		m.URL = detail.URL
		return m, nil
	}
	if e.AppID == "" {
		return m, nil
	}
	app, err := s.Store.AppByID(e.AppID)
	if err != nil {
		return m, err
	}
	if app == nil {
		return m, nil
	}
	m.URL = strings.TrimRight(s.Config.AppsURL, "/") + "/" + app.Slug + "/"
	actor := e.Actor
	clients, err := s.Store.Clients()
	if err != nil {
		return m, err
	}
	for _, c := range clients {
		if c.ID == e.Actor {
			actor = c.Name
			break
		}
	}
	if actor == "owner" {
		actor = "Owner"
	}
	switch e.Action {
	case "app.created":
		m.Title = "新 App：" + app.Slug
		m.Message = fmt.Sprintf("创建者：%s\n标题：%s", actor, app.Title)
	case "app.deployed":
		m.Title = fmt.Sprintf("%s v%d 已发布", app.Slug, detail.Version)
		m.Message = fmt.Sprintf("发布者：%s\n说明：%s", actor, detail.Message)
	default:
		m.Title = app.Slug + "：" + e.Action
		m.Message = fmt.Sprintf("操作者：%s\n%s", actor, e.Detail)
	}
	return m, nil
}
