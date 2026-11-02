package server

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/token"
)

type rateBucket struct {
	Tokens float64
	At     time.Time
}

func (s *Server) allowRate(key string, limit int) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.rates == nil {
		s.rates = make(map[string]rateBucket)
	}
	now := s.now()
	b, ok := s.rates[key]
	if !ok {
		b = rateBucket{Tokens: float64(limit), At: now}
	}
	b.Tokens += now.Sub(b.At).Seconds() * float64(limit) / 60
	if b.Tokens > float64(limit) {
		b.Tokens = float64(limit)
	}
	b.At = now
	allowed := b.Tokens >= 1
	if allowed {
		b.Tokens--
	}
	s.rates[key] = b
	// Evict idle IPs so unauthenticated traffic cannot grow the map forever.
	for k, v := range s.rates {
		if now.Sub(v.At) > 2*time.Minute {
			delete(s.rates, k)
		}
	}
	return allowed
}
func (s *Server) requestIP(r *http.Request) string {
	if s.Config.TrustedProxyHeader != "" {
		if v := r.Header.Get(s.Config.TrustedProxyHeader); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func decodeAuthJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	if err := d.Decode(v); err != nil {
		fail(w, 400, "bad_request", "无效 JSON", "")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "bad_request", "只接受一个 JSON 对象", "")
		return false
	}
	return true
}
func (s *Server) deviceCode(w http.ResponseWriter, r *http.Request) {
	if !s.allowRate("device:"+s.requestIP(r), 10) {
		fail(w, 429, "rate_limited", "申请过于频繁", "一分钟后重试")
		return
	}
	var input api.DeviceCodeRequest
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 256 {
		fail(w, 400, "bad_request", "需要名称（最多 256 字节）", "")
		return
	}
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	code := ""
	for range 8 {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			s.internal(w, err)
			return
		}
		code += string(alphabet[n.Int64()])
	}
	meta, _ := json.Marshal(struct {
		api.DeviceCodeRequest
		RequestIP string `json:"request_ip"`
	}{input, s.requestIP(r)})
	plain := token.New()
	if _, err := s.Store.CreateDevice(token.Hash(plain), code, input.Name, string(meta), s.now().Unix()); err != nil {
		s.internal(w, err)
		return
	}
	display := code[:4] + "-" + code[4:]
	uri := strings.TrimRight(s.Config.AdminURL, "/") + "/device"
	writeJSON(w, 200, api.DeviceCodeResult{DeviceCode: plain, UserCode: display, VerificationURI: uri, VerificationURIComplete: uri + "?code=" + display, ExpiresIn: 600, Interval: 3})
}
func (s *Server) deviceToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DeviceCode string `json:"device_code"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	plain := token.New()
	c, code, err := s.Store.PollDevice(token.Hash(input.DeviceCode), token.Hash(plain), s.now().Unix())
	if err != nil {
		s.internal(w, err)
		return
	}
	if code != "" {
		messages := map[string]string{"authorization_pending": "等待批准", "slow_down": "轮询过于频繁", "access_denied": "已拒绝", "expired_token": "已过期"}
		fail(w, 400, code, messages[code], "")
		return
	}
	writeJSON(w, 200, api.DeviceTokenResult{Token: plain, Client: api.Client{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt}, ExpiresAt: c.ExpiresAt})
}
func (s *Server) agentLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeClient(client(r).ID); err != nil {
		s.internal(w, err)
		return
	}
	s.event(r, "agent.revoked", "", nil)
	writeJSON(w, 200, api.LogoutResult{LoggedOut: true})
}
