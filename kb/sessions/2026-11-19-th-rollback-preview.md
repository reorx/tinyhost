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

## 发版与上线（2026-11-19）

- 用户确认后发版：master CI 绿，`v0.2.0` 附注 tag（指向 `8064bc5`）的 [Release 流水线](https://github.com/reorx/tinyhost/actions/runs/44538373529)一次绿。Release 是正式版，有 10 个资产；流水线里的双架构版本、nonroot、healthy 检查都通过。
- 线上升级（deploy 仓库 `d83b2c2`）：
  - 升级前手动备份成功。升级后 `user_version=4`，`integrity_check=ok`。App、版本、client、layer 的计数和密码哈希前后一致，`app_previews` 是空表。
  - 两个域名的 `X-Tinyhost-Version` 都是 `0.2.0`，`/_/health` 返回 `ok`，容器 healthy。未带 token 请求新接口返回 401。
  - 线上 `.env` 没有设置 `TINYHOST_NOTIFY_EVENTS`，用服务端默认值，`app.rolled_back` 和 `app.preview_published` 自动推送，不需要改 env。
  - [accept-install](https://github.com/reorx/tinyhost/actions/runs/44540357293) 三平台绿，产生的 3 条授权申请没有批准，会自动过期。
- 回退：0.1.2 镜像读不了 v4 的库，要回退必须恢复升级前的快照，不能只改版本号。
- 证据在 `tmp/2026-11-19-v0.2.0-release/`。

## 遗留事项

- 线上没有用真实 Owner 会话在 `th.reorx.com` 验过新的后台界面（版本来源标注、「回滚到此版本」、App 预览区块）和 Agent 端的 `rollback`、`preview --app`、`publish`；本地验收已覆盖，留给用户在真实数据上试。试的时候会产生真实的 `app.rolled_back` / `app.preview_published` 通知。

## 相关文档

- 既有 App 详情页和 Owner 写操作契约见[后台页面清单](2026-11-02-th-m2-console.md)。
- 普通公开预览的生命周期与过期页见[预览实现记录](2026-11-02-th-m3-preview.md)。
- 后台上传与版本结果见[手动上传记录](2026-11-03-th-manual-upload.md)。
