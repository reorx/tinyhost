package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/slug"
)

func rollback(c *context, args []string) (any, error) {
	fs := c.flags("rollback")
	msg := fs.String("m", "", "版本说明")
	pos, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 2 {
		return nil, usage("使用 rollback <slug> vN [-m 说明]")
	}
	if err := validateSlug(pos[0]); err != nil {
		return nil, err
	}
	ref, err := slug.ParseRef(pos[0] + "@" + pos[1])
	if err != nil || ref.Version == 0 {
		return nil, usage("版本必须为 vN，N 是正整数")
	}
	return c.release("/apps/"+ref.Name+"/rollback", api.RollbackRequest{Version: ref.Version, Message: *msg})
}

func publish(c *context, args []string) (any, error) {
	fs := c.flags("publish")
	msg := fs.String("m", "", "版本说明（默认沿用预览说明）")
	pos, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	var segment string
	switch len(pos) {
	case 1:
		origin, err := c.serverURL()
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(pos[0])
		if err != nil || u.User != nil || u.Scheme+"://"+u.Host != origin || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return nil, usage("请提供当前服务器的 App 预览链接")
		}
		segment = strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	case 2:
		segment = pos[0] + "@" + pos[1]
	default:
		return nil, usage("使用 publish <slug> <hash> 或 publish <App 预览链接>")
	}
	ref, err := slug.ParseRef(segment)
	if err != nil || ref.Preview == "" {
		return nil, usage("App 预览链接必须为 /<slug>@<12 位小写十六进制哈希>/")
	}
	return c.release("/apps/"+ref.Name+"/previews/"+ref.Preview+"/publish", api.PublishRequest{Message: *msg})
}

func (c *context) release(path string, req any) (api.DeployResult, error) {
	var result api.DeployResult
	payload, err := json.Marshal(req)
	if err != nil {
		return result, err
	}
	err = c.request("POST", path, nil, bytes.NewReader(payload), "application/json", &result)
	return result, err
}
