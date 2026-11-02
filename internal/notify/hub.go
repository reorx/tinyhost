// Package notify delivers optional asynchronous notifications, independent of server operations.
package notify

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const DefaultEvents = "device.requested,app.created,app.deployed"

type Config struct{ NtfyURL, NtfyToken, WebhookURL, Events string }
type Message struct {
	Event   string `json:"event"`
	Title   string `json:"title"`
	Message string `json:"message"`
	URL     string `json:"url"`
	At      int64  `json:"at"`
}
type Hub struct {
	config Config
	logger *slog.Logger
	events map[string]bool
	queue  chan Message
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func New(c Config, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	events := c.Events
	if events == "" {
		events = DefaultEvents
	}
	h := &Hub{config: c, logger: logger, events: map[string]bool{}, queue: make(chan Message, 100), done: make(chan struct{})}
	for _, e := range strings.Split(events, ",") {
		h.events[strings.TrimSpace(e)] = true
	}
	go h.run()
	return h
}
func (h *Hub) Publish(m Message) {
	if !h.events[m.Event] {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	select {
	case h.queue <- m:
	default:
		h.logger.Warn("notification queue full; dropped", "event", m.Event)
	}
}
func (h *Hub) Close() {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		close(h.queue)
	}
	h.mu.Unlock()
	<-h.done
}
func (h *Hub) run() {
	defer close(h.done)
	for m := range h.queue {
		if h.config.NtfyURL != "" {
			if err := h.ntfy(m); err != nil {
				h.logger.Error("ntfy notification failed", "event", m.Event, "error", err)
			}
		}
		if h.config.WebhookURL != "" {
			if err := h.webhook(m); err != nil {
				h.logger.Error("webhook notification failed", "event", m.Event, "error", err)
			}
		}
	}
}
func (h *Hub) ntfy(m Message) error {
	r, err := http.NewRequest("POST", h.config.NtfyURL, strings.NewReader(m.Message))
	if err != nil {
		return err
	}
	// RFC 2047 keeps Unicode titles legal in HTTP/1 header values; ntfy decodes it.
	r.Header.Set("Title", "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(m.Title))+"?=")
	r.Header.Set("Click", m.URL)
	r.Header.Set("Tags", "package")
	r.Header.Set("Priority", "default")
	r.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if h.config.NtfyToken != "" {
		r.Header.Set("Authorization", "Bearer "+h.config.NtfyToken)
	}
	return send(r)
}
func (h *Hub) webhook(m Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	r, err := http.NewRequest("POST", h.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	return send(r)
}
func send(r *http.Request) error {
	response, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
	// Do not log URL errors: webhook URLs may contain credentials or secret paths.
	if err != nil {
		return fmt.Errorf("notification transport failed")
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if err != nil {
		return fmt.Errorf("notification response read failed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notification HTTP %d", response.StatusCode)
	}
	return nil
}
