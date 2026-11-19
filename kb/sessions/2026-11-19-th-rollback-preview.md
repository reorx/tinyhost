---
created: 2026-11-19
tags:
  - publishing
  - previews
  - rollback
---

# 回滚 App 包，以及审核预览后发布

## 概要

`tinyhost rollback foo v1` 将 v1 的 App 包与主入口当前的数据组合成新版本。`tinyhost preview ./site --app foo` 生成按内容寻址的临时链接，审核期间始终显示主入口的实时数据。`publish` 复用已审核的包层，在事务中选取发布时的数据并切换主入口。精确恢复历史快照仍用 `promote`。普通 `preview` 继续生成独立的公开链接。

## 页面清单增量

| 页面或操作 | 页面契约 |
|---|---|
| admin `GET /apps/{slug}` | `app.html` 的版本表从 `VersionRow` 读取 `RollbackOf`、`Preview`，在包类型下显示「回滚自 vN」或「来自预览 HASH」。非当前版本行提供「回滚到此版本」和「设为主入口」。 |
| admin `POST /apps/{slug}/rollback` | 接受表单字段 `version`。沿用 admin 会话、POST Origin 校验和 `data-confirm` 确认框。成功后 302 回详情页，版本缺失返回 404，主入口已经使用同一包层返回 409。审计记录携带 App ID、源版本和新版本号。 |
| 详情页「App 预览」 | `AppView.Previews []AppPreviewRow` 展示有效预览。每行含可打开的 hash 链接、预览说明、创建者名称、到期时间和已发布版本或待发布状态。 |
| apps `GET/HEAD /{slug}@{hash}/…` | 使用 App 可见性和查看会话。过期展示 `preview-expired.html`，状态 410。`ExpiredPreviewView.App` 用于显示可重新生成该预览的命令。改名跳转保留 hash、文件路径及 query。 |
| admin 操作记录 | `shared.html` 增加「创建 App 预览」「回滚 App 包」「发布预览」。默认通知后两项，消息给出新版本、来源和操作者。 |

## 修改的文件

- `internal/store/{store.go,app_preview_schema.sql,apppreviews.go,versions.go,gc.go}` 实现 v4 迁移、层复用、续期、版本来源与回收。新增和更新的 store 行为测试覆盖 v3 数据、并发发布和 GC。
- `internal/slug/slug.go` 解析三种 App 引用。`internal/api/api.go` 定义预览 DTO 和可选的版本来源。
- `internal/server/{apppreviews,api,previews,content,server,gc}.go` 接入四个 Agent 端点和预览内容路由。`admin.go`、`console.go`、`views/` 更新详情页和表单。HTTP 行为测试覆盖 Bearer、viewer、Origin、缓存、改名、过期、非法请求体和来源信息。
- `internal/cli/{releases,previews,commands,output}.go` 提供新命令、帮助和文本输出。请求测试及 `testdata/golden/` 覆盖人类可读和 JSON 输出。离线 skill 快照测试隔离本机配置，保证所有环境使用同一份快照。
- `internal/notify/hub.go`、`cmd/tinyhost-server/notify.go` 及其测试更新默认事件和文案。
- `skill/SKILL.md`、`docs/json-output.md`、`README.md`、`SECURITY.md`、`AGENTS.md` 同步 CLI 用法、数据契约、访问权限和代码地图。

## 注意事项

同一 App 内，文件路径、文件内容和规范 manifest 决定 12 位哈希。库中额外保存完整 SHA-256；短哈希碰撞返回 409，保留已审核的原内容。续期保留原说明和创建者，较短的 TTL 也不会缩短原有有效期。

回滚或发布之后，新的 data 版本仅继承包和覆盖层。版本来源只属于实际执行该操作的版本。普通预览与 App 预览分别回收，App 预览到期清理后，正式版本仍引用的层继续保留。

## 验收

使用本地编译的 CLI 和服务端，在两个 localhost 域名上以私有 App 验收。原始记录和截图存放在 `tmp/2026-11-19-rollback-preview/`。

| 操作 | 结果 |
|---|---|
| v4 回滚到 v1 | 生成 v5。浏览器显示 v1 的界面与 v4 的数据。再次回滚返回 already_current。 |
| 私有预览与发布 | 未登录时跳转 Owner 登录，登录后返回原预览。同样的包续期得到同一链接；推数据后刷新出现 v6 数据。按 URL 发布成 v7，主入口显示审核过的界面和 v6 数据。重复发布返回 already_current。 |
| 过期和兼容命令 | 调整验收库的到期时间后，浏览器读取及 CLI 发布均返回 410。`deploy --no-promote` 创建 v8 时主入口仍是 v7。`promote foo v2` 恢复了 v2 界面及当时的数据。 |
| 后台 | 已归档浅色、深色、390px 页面、审计和过期页截图。390px 视口的页面宽度为 390px。原生确认框已验证取消、确认及回滚结果。浏览器对话框事件保存在 `confirm.json`，系统屏幕录制权限限制了原生窗口截图。 |
| 自动化 | 全量 Go 测试、vet、gofmt 检查通过。store、server、CLI 新流程也通过了 race 检查。 |

## 遗留事项

建议版本号 `v0.2.0`。上线前需由用户确认发版，再检查 deploy 仓库是否显式覆盖 `notify-events`；覆盖默认列表的实例需按需加入两项新通知。

## 相关文档

- 既有 App 详情页和 Owner 写操作契约见[后台页面清单](2026-11-02-th-m2-console.md)。
- 普通公开预览的生命周期与过期页见[预览实现记录](2026-11-02-th-m3-preview.md)。
- 后台上传与版本结果见[手动上传记录](2026-11-03-th-manual-upload.md)。
