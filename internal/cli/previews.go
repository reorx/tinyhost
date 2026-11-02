package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/token"
)

func init() {
	register(&command{Name: "preview", Summary: "[路径] [--ttl 24h] [--new] 创建或更新临时预览", Run: previewCommand})
	register(&command{Name: "previews", Summary: "列出自己的预览 | rm <链接或 ID>", Run: previewsCommand})
}

func previewCommand(c *context, args []string) (any, error) {
	fs := c.flags("preview")
	var rawTTL string
	var fresh bool
	fs.StringVar(&rawTTL, "ttl", "24h", "有效期（1h–168h）")
	fs.BoolVar(&fresh, "new", false, "创建新链接")
	positional, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	if len(positional) > 1 {
		return nil, usage("preview 最多接受一个路径")
	}
	ttl, err := time.ParseDuration(rawTTL)
	if err != nil || ttl < time.Hour || ttl > 168*time.Hour {
		return nil, usage("预览有效期必须在 1h–168h 之间，例如 --ttl 24h")
	}
	source := "."
	if len(positional) == 1 {
		source = positional[0]
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	origin, err := c.serverURL()
	if err != nil {
		return nil, err
	}
	packed, summary, err := bundle.Pack(source)
	if err != nil {
		return nil, err
	}
	defer os.Remove(packed)
	keys := map[string]string{}
	if err = readAuthFile("previews.json", &keys); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if keys == nil {
		keys = map[string]string{}
	}
	index := origin + "|" + absolute
	key := keys[index]
	if key == "" || fresh {
		key = token.New()
		keys[index] = key
		// Persist before uploading so a network failure can be retried with the same
		// key. --new rotates the local key; old links keep their own server records.
		if err = saveAuthFile("previews.json", keys); err != nil {
			return nil, err
		}
	}
	c.uploadWarnings(summary)
	file, err := os.Open(packed)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	query := url.Values{"key": {key}, "ttl": {ttl.String()}, "source": {filepath.Base(absolute)}}
	var result api.PreviewResult
	err = c.request("POST", "/previews", query, file, "application/gzip", &result)
	result.Warnings = summary.PathWarnings
	return result, err
}

func previewsCommand(c *context, args []string) (any, error) {
	positional, err := c.parse(c.flags("previews"), args)
	if err != nil {
		return nil, err
	}
	if len(positional) == 0 {
		var result api.PreviewsResult
		err = c.request("GET", "/previews", nil, nil, "", &result)
		return result, err
	}
	if len(positional) != 2 || positional[0] != "rm" {
		return nil, usage("使用 previews 或 previews rm <链接或 ID>")
	}
	id := positional[1]
	if strings.Contains(id, "://") {
		origin, err := c.serverURL()
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(id)
		if err != nil || u.User != nil || u.Scheme+"://"+u.Host != origin || !strings.HasPrefix(u.Path, "/_p/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, usage("请提供当前服务器的预览链接或 ID")
		}
		value := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/_p/"), "/")
		if len(value) != 20 || strings.Trim(value, "abcdefghijklmnopqrstuvwxyz234567") != "" {
			return nil, usage("预览链接必须为 /_p/<20 位 token>/")
		}
		id = value
	} else if !strings.HasPrefix(id, "p_") || strings.ContainsAny(id, "/\\?#") {
		return nil, usage("预览 ID 必须以 p_ 开头")
	}

	var result api.DeletePreviewResult
	err = c.request("DELETE", "/previews/"+url.PathEscape(id), nil, nil, "", &result)
	return result, err
}

func (c *context) uploadWarnings(summary bundle.Summary) {
	for _, warning := range summary.Warnings {
		fmt.Fprintln(c.stderr, "警告："+warning)
	}
	if len(summary.PathWarnings) == 0 {
		return
	}
	fmt.Fprintf(c.stderr, "⚠ 发现 %d 处以 / 开头的路径，在 tinyhost 上会加载失败：\n", len(summary.PathWarnings))
	for _, w := range summary.PathWarnings {
		fmt.Fprintf(c.stderr, "  %s:%d  %s\n", w.File, w.Line, w.Text)
	}
	fmt.Fprintln(c.stderr, "  改成相对路径，例如 ./app.js、data/sales.json")
}
