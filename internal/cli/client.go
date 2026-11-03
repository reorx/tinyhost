package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/api"
)

var errServerNotConfigured = usage("请用 --server、TINYHOST_SERVER 或 config.json 设置服务器地址")

// retryDelays spaces the extra attempts of a read-only request.
var retryDelays = []time.Duration{time.Second, 2 * time.Second}

// gatewayFailure reports a CDN that could not reach the server (52x, 502-504)
// or a failed connection. Client timeouts are not retried: they already waited.
func gatewayFailure(response *http.Response, err error) bool {
	if err != nil {
		var netErr net.Error
		return !errors.As(err, &netErr) || !netErr.Timeout()
	}
	switch response.StatusCode {
	case 502, 503, 504, 520, 521, 522, 523, 524:
		return true
	}
	return false
}

func (c *context) serverURL() (string, error) {
	raw := c.server
	if raw == "" {
		raw = os.Getenv("TINYHOST_SERVER")
	}
	if raw == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		path := filepath.Join(dir, "tinyhost", "config.json")
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("读取 %s：%w", path, err)
		}
		if err == nil {
			var config struct {
				Server string `json:"server"`
			}
			if err = json.Unmarshal(data, &config); err != nil {
				return "", fmt.Errorf("解析 %s：%w", path, err)
			}
			raw = config.Server
		}
	}
	if raw == "" {
		return "", errServerNotConfigured
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", usage("服务器地址必须为 http(s)://host[:port]，不带路径、凭证、查询或片段")
	}
	return strings.TrimRight(raw, "/"), nil
}
func (c *context) request(method, path string, query url.Values, body io.Reader, contentType string, result any) error {
	key, err := c.token()
	if err != nil {
		return err
	}
	if key == "" {
		return &commandError{detail: api.ErrorDetail{Code: "unauthorized", Message: "缺少 Agent token", Hint: "运行 tinyhost login 重新授权"}, exit: 3}
	}
	return c.httpRequest(method, path, query, body, contentType, key, result)
}
func (c *context) httpRequest(method, path string, query url.Values, body io.Reader, contentType, key string, result any) error {
	origin, err := c.serverURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, origin+"/_/api/v1"+path, body)
	if err != nil {
		return err
	}
	req.URL.RawQuery = query.Encode()
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	timeout := c.requestTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	// Read-only requests ride out a CDN that briefly cannot reach the server.
	// Writes are never repeated: the first attempt may already have applied.
	for _, delay := range retryDelays {
		if method != "GET" || !gatewayFailure(response, err) {
			break
		}
		if err == nil {
			response.Body.Close()
		}
		time.Sleep(delay)
		response, err = client.Do(req)
	}
	if err != nil {
		return err
	}
	defer response.Body.Close()
	c.remoteVersion = response.Header.Get("X-Tinyhost-Version")
	c.remoteOrigin = origin
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure api.Error
		if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&failure); err != nil || failure.Error.Code == "" {
			return fmt.Errorf("服务器返回 HTTP %d，响应不是 tinyhost JSON 错误", response.StatusCode)
		}
		exit := 1
		if response.StatusCode == http.StatusUnauthorized {
			exit = 3
		}
		return &commandError{detail: failure.Error, exit: exit}
	}
	if err = json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("解析服务器响应：%w", err)
	}
	return nil
}
