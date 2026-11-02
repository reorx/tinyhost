package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/slug"
	"github.com/reorx/tinyhost/internal/version"
	"github.com/reorx/tinyhost/skill"
)

func init() {
	register(&command{Name: "deploy", Summary: "[路径] --app SLUG [--create --public --title 标题] [-m 说明] [--no-promote]", Run: deploy})
	register(&command{Name: "data", Summary: "push <路径> --app SLUG --to 目录/ [-m 说明] | rm --app SLUG --to 目录/", Sub: map[string]*command{
		"push": {Name: "push", Run: pushData}, "rm": {Name: "rm", Run: removeData},
	}})
	register(&command{Name: "apps", Summary: "列出所有 App", Run: apps})
	register(&command{Name: "info", Summary: "<slug> 查看 App、数据声明和覆盖目录", Run: info})
	register(&command{Name: "versions", Summary: "<slug> 列出版本和主入口", Run: versions})
	register(&command{Name: "promote", Summary: "<slug> <vN> 切换主入口", Run: promote})
	register(&command{Name: "delete", Summary: "<slug> 删除自己创建的 App（进入回收站）", Run: deleteApp})
	register(&command{Name: "whoami", Summary: "查看当前 Agent 和服务器", Run: whoami})
	register(&command{Name: "version", Summary: "显示 CLI 版本", Run: localVersion})
	register(&command{Name: "help", Summary: "显示帮助", Run: help})
	register(&command{Name: "skill", Summary: "显示 Agent 使用说明（M3 补全）", Run: skillCommand})
}
func deploy(c *context, args []string) (any, error) {
	fs := c.flags("deploy")
	var app, title, message string
	var create, public, noPromote bool
	fs.StringVar(&app, "app", "", "App slug")
	fs.StringVar(&title, "title", "", "标题")
	fs.StringVar(&message, "m", "", "版本说明")
	fs.BoolVar(&create, "create", false, "新建 App")
	fs.BoolVar(&public, "public", false, "新建为公开 App")
	fs.BoolVar(&noPromote, "no-promote", false, "不切换主入口")
	positional, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	if len(positional) > 1 {
		return nil, usage("deploy 最多接受一个路径")
	}
	if public && !create {
		return nil, usage("--public 只能与 --create 一起使用；更新不改变可见性")
	}
	source := "."
	if len(positional) == 1 {
		source = positional[0]
	}
	packed, summary, err := bundle.Pack(source)
	if err != nil {
		return nil, err
	}
	defer os.Remove(packed)
	if app == "" {
		app = summary.Manifest.App
	}
	if app == "" {
		return nil, usage("缺少 App，请指定 --app，或在 tinyhost.json 中设置 app")
	}
	if err = validateSlug(app); err != nil {
		return nil, err
	}
	query := url.Values{"message": {message}}
	if create {
		query.Set("create", "1")
	}
	if public {
		query.Set("visibility", "public")
	}
	if noPromote {
		query.Set("promote", "0")
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "title" {
			query.Set("title", title)
		}
	})
	result, err := c.upload("/apps/"+app+"/deploys", query, packed, summary)
	if err != nil {
		return nil, err
	}
	if create {
		if err = rememberApp(source, app); err != nil {
			fmt.Fprintf(c.stderr, "警告：发布已成功，但 tinyhost.json 写回失败：%v；后续请显式传 --app %s\n", err, app)
		}
	}
	return result, nil
}
func rememberApp(source, app string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	root := source
	if !info.IsDir() {
		root = filepath.Dir(source)
	}
	path := filepath.Join(root, "tinyhost.json")
	object := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		decoder := json.NewDecoder(strings.NewReader(string(b)))
		decoder.UseNumber()
		if err = decoder.Decode(&object); err != nil {
			return err
		}
		if object == nil {
			return fmt.Errorf("tinyhost.json 必须为 JSON 对象")
		}
	}
	object["app"] = app
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
func validateSlug(app string) error {
	if !slug.Valid(app) {
		return &commandError{detail: api.ErrorDetail{Code: "slug_invalid", Message: "slug 不合法：" + app, Hint: "长度 3–40，仅小写字母、数字、连字符，首尾为字母或数字，不含连续 --，不能用 api/admin/www/static/assets/login/logout/auth"}, exit: 1}
	}
	return nil
}
func pushData(c *context, args []string) (any, error)   { return dataCommand(c, args, true) }
func removeData(c *context, args []string) (any, error) { return dataCommand(c, args, false) }
func dataCommand(c *context, args []string, push bool) (any, error) {
	fs := c.flags("data")
	var app, dir, message string
	fs.StringVar(&app, "app", "", "App slug")
	fs.StringVar(&dir, "to", "", "覆盖目录")
	fs.StringVar(&message, "m", "", "版本说明")
	positional, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	expected := 0
	if push {
		expected = 1
	}
	if len(positional) != expected || app == "" || dir == "" {
		return nil, usage("data push 需要一个路径；data rm 不接受路径；两者都必须提供 --app 和 --to")
	}
	if err = validateSlug(app); err != nil {
		return nil, err
	}
	query := url.Values{"dir": {dir}, "message": {message}}
	path := "/apps/" + app + "/data"
	if !push {
		var result api.DeployResult
		err = c.request("DELETE", path, query, nil, "", &result)
		return result, err
	}
	packed, summary, err := bundle.Pack(positional[0], bundle.Options{Data: true})
	if err != nil {
		return nil, err
	}
	defer os.Remove(packed)
	return c.upload(path, query, packed, summary)
}
func (c *context) upload(path string, query url.Values, packed string, summary bundle.Summary) (api.DeployResult, error) {
	c.uploadWarnings(summary)
	file, err := os.Open(packed)
	if err != nil {
		return api.DeployResult{}, err
	}
	defer file.Close()
	var result api.DeployResult
	err = c.request("POST", path, query, file, "application/gzip", &result)
	result.Warnings = summary.PathWarnings
	return result, err
}
func positionals(c *context, name string, args []string, count int) ([]string, error) {
	result, err := c.parse(c.flags(name), args)
	if err != nil {
		return nil, err
	}
	if len(result) != count {
		return nil, usage(fmt.Sprintf("%s 需要 %d 个位置参数", name, count))
	}
	if count > 0 {
		if err = validateSlug(result[0]); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func apps(c *context, args []string) (any, error) {
	if _, err := positionals(c, "apps", args, 0); err != nil {
		return nil, err
	}
	var result api.AppsResult
	err := c.request("GET", "/apps", nil, nil, "", &result)
	return result, err
}
func info(c *context, args []string) (any, error) {
	p, err := positionals(c, "info", args, 1)
	if err != nil {
		return nil, err
	}
	var result api.AppDetail
	err = c.request("GET", "/apps/"+p[0], nil, nil, "", &result)
	return result, err
}
func versions(c *context, args []string) (any, error) {
	p, err := positionals(c, "versions", args, 1)
	if err != nil {
		return nil, err
	}
	var result api.VersionsResult
	err = c.request("GET", "/apps/"+p[0]+"/versions", nil, nil, "", &result)
	return result, err
}
func promote(c *context, args []string) (any, error) {
	p, err := positionals(c, "promote", args, 2)
	if err != nil {
		return nil, err
	}
	number, err := strconv.Atoi(strings.TrimPrefix(p[1], "v"))
	if err != nil || number < 1 || !strings.HasPrefix(p[1], "v") {
		return nil, usage("版本必须为 vN，N 是正整数")
	}
	payload, err := json.Marshal(api.PromoteRequest{Version: number})
	if err != nil {
		return nil, err
	}
	var result api.PromoteResult
	err = c.request("POST", "/apps/"+p[0]+"/promote", nil, strings.NewReader(string(payload)), "application/json", &result)
	return result, err
}
func deleteApp(c *context, args []string) (any, error) {
	p, err := positionals(c, "delete", args, 1)
	if err != nil {
		return nil, err
	}
	var result api.DeleteResult
	err = c.request("DELETE", "/apps/"+p[0], nil, nil, "", &result)
	return result, err
}
func whoami(c *context, args []string) (any, error) {
	if _, err := positionals(c, "whoami", args, 0); err != nil {
		return nil, err
	}
	var result api.WhoamiResult
	err := c.request("GET", "/whoami", nil, nil, "", &result)
	return result, err
}
func localVersion(c *context, args []string) (any, error) {
	_, err := positionals(c, "version", args, 0)
	return api.VersionResult{Version: version.Version}, err
}
func help(c *context, args []string) (any, error) {
	_, err := positionals(c, "help", args, 0)
	return api.HelpResult{Help: helpText()}, err
}
func skillCommand(c *context, args []string) (any, error) {
	_, err := positionals(c, "skill", args, 0)
	return api.SkillResult{Skill: skill.Text}, err
}
