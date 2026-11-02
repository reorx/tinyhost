package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/reorx/tinyhost/internal/api"
)

func timestamp(unix int64) string { return time.Unix(unix, 0).UTC().Format(time.RFC3339) }
func current(number *int) string {
	if number == nil {
		return "尚未发布"
	}
	return fmt.Sprintf("v%d", *number)
}
func (c *context) printHuman(value any) error {
	var b strings.Builder
	switch result := value.(type) {
	case api.DeployResult:
		fmt.Fprintf(&b, "✓ %s v%d 已发布（%d 个文件，%d 字节）\n", result.App, result.Version, result.Files, result.Size)
		if result.DataDir != nil {
			fmt.Fprintf(&b, "  覆盖目录：%s\n", *result.DataDir)
		}
		fmt.Fprintf(&b, "  版本链接：%s\n", result.VersionURL)
		if result.Promoted {
			fmt.Fprintf(&b, "  主入口：  %s\n", result.MainURL)
		} else {
			fmt.Fprintf(&b, "  主入口仍指向 %s：%s\n", current(result.CurrentVersion), result.MainURL)
		}
		fmt.Fprintf(&b, "  创建者：%s\n", result.AppCreatedBy)
	case api.PreviewResult:
		fmt.Fprintf(&b, "✓ 预览已更新，%s 过期\n  %s\n", timestamp(result.ExpiresAt), result.URL)
	case api.PreviewsResult:
		fmt.Fprintf(&b, "有效预览：%d\n", len(result.Previews))
		for _, p := range result.Previews {
			fmt.Fprintf(&b, "%s  %s  %s  %s\n  %s\n", p.ID, p.Source, timestamp(p.ExpiresAt), "过期", p.URL)
		}
	case api.DeletePreviewResult:
		fmt.Fprintf(&b, "✓ 预览 %s 已删除\n", result.ID)
	case api.AppsResult:
		w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\t标题\t可见性\t当前版本\t创建者\t更新时间")
		for _, app := range result.Apps {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", app.Slug, app.Title, app.Visibility, current(app.CurrentVersion), app.CreatedBy, timestamp(app.UpdatedAt))
		}
		if err := w.Flush(); err != nil {
			return err
		}
	case api.AppDetail:
		app := result.App
		fmt.Fprintf(&b, "%s — %s\n主入口：%s\n可见性：%s\n创建者：%s\n当前版本：%s\n", app.Slug, app.Title, app.MainURL, app.Visibility, app.CreatedBy, current(app.CurrentVersion))
		if app.CurrentVersion != nil {
			fmt.Fprintf(&b, "版本链接：%s@v%d/\n", strings.TrimSuffix(app.MainURL, "/"), *app.CurrentVersion)
		}
		b.WriteString("声明的数据目录：\n")
		dirs := make([]string, 0, len(result.Manifest.Data))
		for dir := range result.Manifest.Data {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			fmt.Fprintf(&b, "  %s %s\n", dir, result.Manifest.Data[dir])
		}
		b.WriteString("现有覆盖目录：\n")
		for _, overlay := range result.Overlays {
			fmt.Fprintf(&b, "  %s %s %s\n", overlay.Dir, overlay.PushedBy, timestamp(overlay.PushedAt))
		}
	case api.VersionsResult:
		for _, v := range result.Versions {
			kind := "App 包"
			if v.Kind == "data" {
				kind = "数据"
				if v.DataDir != nil {
					kind += " " + *v.DataDir
				}
			}
			marker := ""
			if v.Current {
				marker = " ← 主入口"
			}
			fmt.Fprintf(&b, "v%d  %s  %s  %s  %s%s\n", v.Number, kind, v.CreatedBy, v.Message, timestamp(v.CreatedAt), marker)
		}
	case api.PromoteResult:
		fmt.Fprintf(&b, "✓ %s 主入口已指向 v%d\n  %s\n", result.App, result.CurrentVersion, result.MainURL)
	case api.DeleteResult:
		fmt.Fprintf(&b, "✓ %s 已删除；%d 天内可以请 Owner 在后台恢复\n", result.App, result.TrashDays)
	case api.WhoamiResult:
		origin, err := c.serverURL()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "Agent：%s（%s）\n服务器：%s\n服务端版本：%s\n", result.Client.Name, result.Client.ID, origin, result.ServerVersion)
		if result.Client.ExpiresAt != nil {
			fmt.Fprintf(&b, "token 到期：%s\n", timestamp(*result.Client.ExpiresAt))
		} else {
			b.WriteString("token 到期：永久\n")
		}
	case api.LoginResult:
		switch result.Status {
		case "environment":
			b.WriteString("正在使用环境变量里的 token\n")
		case "already_logged_in":
			fmt.Fprintf(&b, "已登录为 %s\n", result.Name)
		case "pending":
			b.WriteString("等待中；用户批准后再运行一次 tinyhost login\n")
		default:
			fmt.Fprintf(&b, "✓ 已授权为 %s\n", result.Name)
		}
	case api.LogoutResult:
		b.WriteString("✓ 已退出\n")
	case api.VersionResult:
		fmt.Fprintln(&b, result.Version)
	case api.HelpResult:
		b.WriteString(result.Help)
	case api.SkillResult:
		b.WriteString(result.Skill)
	default:
		return fmt.Errorf("未知输出类型 %T", value)
	}
	_, err := fmt.Fprint(c.stdout, b.String())
	return err
}
