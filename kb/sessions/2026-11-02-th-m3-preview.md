---
created: 2026-11-02
tags:
  - tinyhost
  - preview
  - cli
  - bdd
---

## 协调者摘要（th-m3-preview）

- 结果：完成 M3 预览存储/GC、Agent API、公开内容托管、后台列表与删除、CLI 三个命令及上传前路径警告；没有提前做 dist/UI 任务。
- 提交：代码、测试、契约文档与本文同一提交；准确 hash 用 `git log -1 --format=%H -- kb/sessions/2026-11-02-th-m3-preview.md` 获取，避免内容自引用。
- 验证：`mise exec -- make test` 全绿，120 个顶层测试、71 个子测试，共 191 项；本任务增加 16 个顶层测试，八个点名测试全部通过。
- 检查：`mise exec -- go vet ./...`、`mise exec -- gofmt -l internal cmd`、`git diff --check` 通过；server/store/cli 的预览及警告测试 race 检查通过。
- 快照：新增七份 CLI golden，更新 help；覆盖 preview/previews/rm、TTL 错误、deploy/data push/preview 的可选 warnings。
- 证据：`tmp/tinyhost-v1/work/th-m3-preview/` 有红灯日志、最终测试/计数、race/vet/格式记录、本地真实 CLI/curl/browser 冒烟脚本、输出和服务日志。
- 截图：`tmp/2026-11-02-m3-preview/public-preview.png`、`admin-previews.png`；未登录浏览器确认 CSS 背景和 JS 文本，后台能列出全部预览。
- 冒烟：链接复用且内容更新、--new 独立链接、--ttl 30d 拒绝、deploy/preview 带文件与行号警告但成功、后台删除后 404，均通过。
- 决定：CLI --new 轮换并保存本地 key，后续普通 preview 更新新链接；DELETE 额外接受链接 token 并仍校验创建者，支持过期链接删除。
- 决定：遵循 M3 明文存短期预览 token 的例外，将新建与已存在数据库权限收紧至 0600；到期即时 410，GC 释放层、保留七天墓碑。
- 未验证：生产环境、真正等待七天（用注入时钟覆盖）、手机视觉设计；后两页仅接入已有样式，视觉设计交 th-m3-ui。
- 资源：验收前 context 低于 40%；本地服务和独立浏览器会话已关闭，3471 无监听；mac-dev-cleanup 的 browser 检查无遗留。
- 交给协调者：合并下方 AGENTS.md 建议和共享状态；dist 补齐 skill 的 preview/路径警告说明；UI 以「页面清单增量」为契约。

## 实现说明

预览在 `user_version=3` 新表中按 `(client_id, local_key)` 唯一，内容是一个普通层，不归属 App，也不生成版本。一次事务插入新层并替换预览引用，失败保留旧内容和到期时间；并发同 key 更新只产生一个稳定链接。预览上传使用现有解包限制和上传/GC 生命周期锁。20 位小写 base32 token 使用 100 bit 随机数，不是 Agent token，也不能用作任何写操作的凭证。

到期判断用 `Server.Now`，不等待 GC。GC 先将过期行的 layer 置 NULL，再按全部引用判断孤儿层；墓碑超过七天删除。`OrphanLayers` 与 `DeleteOrphanLayer` 都检查 previews.layer，后者仍原子重查引用。重新上传到尚未删除的墓碑会恢复原 id/链接，SQL 引用及 expiry 一起更新。

内容托管抽出原 M1 的 `resolveContent`，预览用仅含 BaseLayer 的版本视图复用它。目录跳转、补 `.html`、自定义 404、隐藏路径与 traversal 拒绝、HEAD、ETag、Range 保持统一；预览始终 no-cache/noindex，无可见性检查。过期页来自独立 embed 模板，不调用后台 head，也不依赖 admin 域名资源。

CLI 的 `previews.json` 复用 `saveAuthFile` 的 0600 同目录原子替换。索引使用归一后的服务器 origin 与绝对路径，source 仅上报 basename。首次/--new 时先保存 key 再上传，网络失败后可用同 key 重试。--new 通过随机新 key 实现，不使用 API 的 new=1：后者会忽略传入 key，CLI 无法把服务端随机 key 记回 spec 规定的本地映射；API new=1 本身按 spec 实现并有独立测试。旧链接继续保留到期。

API 的 DELETE 保留 `/previews/{id}` 路由，附加支持将链接中的 token 解析成真正 ID，再检查请求 client。这样 CLI 删除过期链接时无需依赖只列有效预览的 GET，也不需要新增本地缓存或暴露他人的预览。成功 DTO 始终返回实际预览 ID；猜中他人的 ID 或 token 都返回相同 404。

`CheckAbsolutePaths(map[string][]byte)` 确定性地逐行扫描指定文本扩展名，单文件上限 1MiB，每行一条、每文件五条、总共二十条。Pack 只检查实际上传的文件，沿用排除与符号链接策略；不把被跳过文件读进检查器。检查结果放入新增 `Summary.PathWarnings`，保留已有 `Warnings []string` 的符号链接提示语义。CLI 上传前打印 stderr 警告，并在成功 DTO 加可选 warnings；curl/HTTP 直传不检查。

M3 的明文 capability 使数据库权限要求变得明确：`Store.Open` 在 SQLite 打开前以 0600 创建文件，并收紧已有文件权限。已有 schema 检查测试随迁移升级到 version=3，未来 schema 拒绝用例改为 version=4，没有取消其断言。

## 页面清单增量

所有 view model 都是具名 struct，定义在 `internal/server/previews.go`。下表就是 th-m3-ui 的页面契约；路由、字段名与含义只增不改。

| 页面 / 路由 | 模板、view model | 表单 action 与字段 |
|---|---|---|
| admin `GET /previews` | `internal/server/views/previews.html`；`PreviewsView.Previews []PreviewRow`。列出全部 Agent 的预览，包含未清理的过期墓碑；空列表有空态。 | `POST /previews/{{.ID}}/delete`，无业务字段。沿用公共导航的 logout/logout-all 表单。删除表单有 `data-confirm`。成功 302 `/previews`，不存在 404。 |
| apps `GET/HEAD /_p/{token}/...` 的过期状态 | `internal/server/views/preview-expired.html`；`ExpiredPreviewView.ExpiresAt int64`（Unix 秒，当前朴素页未展示）。HTTP 410，标题「这个预览已过期」，提示联系 Agent 重新 preview。 | 无表单。HEAD 无 body。不加载 admin head/导航或 admin 静态资源；目前没有内联样式/脚本。 |
| admin 公共导航 | `internal/server/views/shared.html` 的 `nav` 增加 key `previews` 和链接 `/previews`，当前位置设置 aria-current。其他 key 与公共模板接口保持原状。 | 现有公共账户表单不变。 |

`PreviewRow` 嵌入 `*store.Preview`，字段：ID、Token、ClientID、LocalKey、Source string；Layer *string（GC 后可为 nil）；CreatedAt、UpdatedAt、ExpiresAt int64。另加 URL、CreatorName string 和 Expired bool。UI 应使用 URL、ID、Source、CreatorName、Expired 和时间字段；Token/LocalKey/Layer 属于内部存储信息，无需展示。Source 可为空，当前链接以 ID 回退。表格字段现在是来源与链接、Agent、到期时间、状态、删除操作。

后台继承现有 adminMiddleware 的会话、每个 POST 的 Origin 检查、no-store 和 CSP；没有新增内联脚本、style 属性或 style 标签。删除用 `ownerEvent("preview.deleted", {"preview_id": id})` 审计，AppID 为空；操作记录页已经能显示该动作的字面码。UI 可给 shared.html 的 `action` 增加中文名称。预览页沿用现有 `.page`、`.page-head`、`.tbl`、`.btn`、`.btn-danger` 等类，没有新 CSS 或视觉体系。

## 测试与证据

- 红灯：`red-server.log`（七个预览测试原先全部 404）、`red-bundle.log`（检查器尚未定义）、`red-cli.log`（未知命令/缺少警告）、`red-store.log`（数据库 0644）、`red-expired-link-delete.log`（过期 token 无法删除）。
- 最终全量：`final-test.log`；结构化输出 `tests.jsonl`，统计脚本 `report_tests.py` 与 `test-count.txt`。新增行为测试覆盖迁移、并发、事务回滚、GC 引用与权限。
- `race.log`：`mise exec -- go test -race ./internal/server ./internal/store ./internal/cli -run 'TestPreview|TestExpiredPreview|TestPreviews|TestAdminCanDeleteAnyPreview|TestConcurrentPreview|TestUploadsWarn'`。
- `vet.log`、`gofmt.log` 应为空；另有 `cleanup.log`、`context-inspect.txt`。
- `smoke.py` 运行构建后的真实两个二进制，数据在任务自己的 data 目录；启动/关闭都由同一脚本托管。`smoke.log` 保存命令与输出，token 创建输出不落日志，测试 cookie 仅在本地临时文件。
- 浏览器在预览截图前从未登录，DOM 检查 body 背景为 `rgb(237, 247, 239)`、JS 将 #status 改为「JS 已加载，CSS 已生效」；刷新后包含第二版文案。
- 同一脚本登录后台截图，再以真实 admin 表单 POST 删除，`delete-headers.txt` 的 Location 是 `/previews`，随后链接 404。CLI 还验证按链接删除和空列表。
- 首轮冒烟脚本用了页面不存在的 `button[type=submit]` 显式属性选择器，已改为实际的「登录」按钮语义定位并完整重跑；`smoke-first-attempt.log` 保留这次脚本定位失败。

截图目录：`tmp/2026-11-02-m3-preview/`。服务、命名浏览器会话均已关闭，没有启动模拟器、Docker、额外 tab 或生产操作。

## AGENTS.md 建议

由协调者合并，执行者未改根 AGENTS.md、共享清单或 spec 勾选框。可粘贴内容：

- `internal/store/previews.go`：M3 迁移到 `user_version=3`，预览按 client/local_key 唯一；`CommitPreview` 原子替换层和到期时间。GC 先将过期 layer 置 NULL，七天后删除墓碑；孤儿层的两处判断都必须检查 previews.layer。`Store.Open` 将新建和已有数据库权限设为 0600。
- `internal/server/previews.go`：预览 API 只认 Bearer，列表/删除按 client 隔离；DELETE 除 ID 外也支持 URL token，但仍检查创建者。`/_p/` 是无登录公开读取，20 位 base32 token 含 100 bit 随机性；**短期预览 token 是只存 hash 规则的例外**，明文保存用于后台显示链接，不授权写操作。到期用 Server.Now 即时返回 410；内容复用 content.go 的 resolveContent。
- 后台导航增加 key `previews`。新增后台预览页和 apps 过期页的路由、模板、view model、表单契约见 `kb/sessions/2026-11-02-th-m3-preview.md`「页面清单增量」；重做这两页前先读。
- `internal/bundle`：`CheckAbsolutePaths` 返回 File/Line/Text；Pack 将结果放进 Summary.PathWarnings，原有 Warnings 仍是符号链接等文本提示。只扫描上传文件的 HTML/CSS/JS 等文本，每文件 ≤1MiB，每文件五条、总共二十条，不阻止上传。
- `internal/cli`：preview/previews/previews rm；previews.json 是 server/绝对路径到 local_key 的 0600 原子映射。--new 轮换 key，后续普通 preview 更新新链接；deploy/data push/preview 的路径警告走 stderr，并在 JSON 增加可选 warnings。

## 交给协调者

- th-m3-preview 完成，可继续派 th-m3-dist；其中 skill 正文必须补预览命令、--ttl 边界、--new 及路径警告。
- th-m3-ui 只需依据本总结「页面清单增量」接续设计后台预览页与过期页；过期页的资源路径必须考虑 apps/admin 两个 Host，当前不依赖 admin 静态文件。
- 接受两处实施细化：CLI --new 用轮换 key 保持后续更新语义；DELETE 接受 token 以覆盖过期链接删除。文档与测试已同步，不影响 M1/M2 契约。
- 共享文档的关键约束须注明短期预览 token 明文存储的例外，避免与根文件原先「token 只存 SHA-256」的概括冲突。

## 相关文档

- [总 plan](../plans/2026-11-02-tinyhost.md) — 依据其中概念、URL、权限与技术约束。
- [M3 spec](../plans/2026-11-02-tinyhost-preview-distribution-spec.md) — 实现其中预览与路径检查范围。
- [M1 基础库](2026-11-02-th-m1-libs.md) — 参考 GC 预览引用的交接。
- [M1 CLI](2026-11-02-th-m1-cli.md) — 参考 CLI 与 golden 约定。
- [M2 后台](2026-11-02-th-m2-console.md) — 参考页面/字段契约与审计接线。
- [M2 界面](2026-11-02-th-m2-ui.md) — 沿用公共模板、样式、CSP 与时间呈现方式。

## 修改的文件

- store：`store.go`、`gc.go`、`store_test.go`；新增 `preview_schema.sql`、`previews.go`、`previews_test.go`。
- server：`server.go`、`content.go`、`gc.go`、`admin.go`；新增 `previews.go`、`preview_test.go`。
- views：新增 `previews.html`、`preview-expired.html`；`shared.html` 增加导航入口。
- bundle：`bundle.go`、`pack.go`；新增 `check.go`、`check_test.go`。
- cli：`commands.go`、`output.go`；新增 `previews.go`、`preview_test.go`、七份 golden，更新 help golden。
- 契约与总结：`internal/api/api.go`、`docs/json-output.md`、本文。

## 返工 r1：过期页 CSP

为 apps 域名服务端生成的预览过期页补上 CSP：`default-src 'self'; frame-ancestors 'none'; form-action 'none'`。`internal/server/distribution.go` 定义 `trustedAppsPageCSP`，根路径页和 `previewContent` 的 410 分支共同引用它，且在写出状态码前设置响应头。正常预览内容和 404 分支未改。

先新增 `TestExpiredPreviewUsesTrustedPageCSP`：断言根路径页的准确策略，GET/HEAD 过期预览返回同样的 CSP，HEAD 无 body；同时断言正常预览和不存在预览仍不带 CSP。实现前运行 `mise exec -- go test ./internal/server -run '^TestExpiredPreviewUsesTrustedPageCSP$' -count=1`，GET、HEAD 两项均因 410 的 CSP 为空失败；实现后同命令加 `-v`，一个顶层测试及两个子测试全部通过。

证据保存在 `tmp/tinyhost-v1/work/th-m3-preview/`：`r1-red.log`、`r1-green.log`、`r1-test.log`、`r1-tests.jsonl`、`r1-test-count.txt`、`r1-vet.log`、`r1-gofmt.log`。`mise exec -- make test` 全绿，共 139 个顶层测试、75 个子测试，合计 214 项；`mise exec -- go vet ./...`、`mise exec -- gofmt -l internal cmd` 输出为空，`git diff --check` 通过。本次未修改页面模板，没有启动浏览器或本地服务。

代码、测试和本节在同一提交。协调者在工作区中的 `AGENTS.md` 和 M3 spec 改动未修改、未暂存；本次提交仅含 `distribution.go`、`previews.go`、`preview_test.go` 与本文。
