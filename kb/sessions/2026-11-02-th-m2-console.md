---
created: 2026-11-02
tags:
  - tinyhost
  - m2
  - admin-console
  - bdd
---

## 协调者摘要（th-m2-console）

- 结果：完成 App 列表/详情/全部写操作、回收站、Agent 管理、操作记录及旧 slug 内容跳转，保留 auth 的中间件、路由、字段契约。
- 提交：代码、测试与本文在同一提交；准确 hash 用 `git log -1 --format=%H -- kb/sessions/2026-11-02-th-m2-console.md` 获取，避免内容自引用。
- 验证：`mise exec -- make test` 全绿，全仓 100 个顶层测试、含子测试 136 项，无失败或测试跳过；比 auth 新增 9 个顶层测试。
- 检查：`mise exec -- go vet ./...`、`mise exec -- gofmt -l internal cmd`、`git diff --check` 通过；store/server 的 `go test -race` 通过。
- 行为：四个点名测试均通过；恶意 App 测试已补删除 App、改可见性，两项 admin POST 均返回 403。
- 证据：`tmp/tinyhost-v1/work/th-m2-console/`，含先失败记录、全仓测试、数量统计、race、vet、gofmt、curl 脚本/输出和服务日志。
- curl：登录 → 新建默认私有空 App → 改公开 → 发布 → 改 slug → 旧主入口/版本 301（保留 query）→ 删除 404 → 回收站恢复后主入口/版本 200。
- 决定：改名及预留旧 slug 为一个事务；删除版本在同一事务检查当前版本；审计按稳定 App ID 筛选，purge 后仍保留记录。
- 决定：模板保持无样式 HTML；删除确认统一用外部 admin.js + form[data-confirm]；新增 HomeView.Ready 防止未设置密码时显示业务表单。
- 未验证：真实浏览器、视觉、桌面/手机截图、生产环境，均属后续任务；本次无截图。
- 资源：冒烟前 context 17%；服务已 SIGTERM + wait（exit 0），3471 无监听，临时 cookie 已删；未启动浏览器、模拟器、Docker 或辅助 tab。
- 交给协调者：采用下方页面清单派发 UI；合并 AGENTS.md 建议和共享状态；另留意下文 purge 后 slug 的生命周期边界。

## 实现说明

### 行为与存储

`internal/server/admin.go` 中注册所有业务路由，继续由同一个 `adminMiddleware` 执行 admin 会话鉴权、每个 POST 的 Origin 校验以及 CSP/COOP/Referrer-Policy/no-store。未改 auth 的登录、批准、票据和会话语义，未改 Agent API DTO 或 CLI 输出。

`internal/server/console.go` 包含业务 handler 与具名 view model；`internal/store/console.go` 增加查询和变更方法，无 schema 版本变化：

- 首页排除回收站；待批准列表只含未到期的 pending 申请；创建者/发布者优先显示 Agent 名称，缺失则显示原 ID。
- Owner 新建空 App 时默认 private、CreatedBy=owner、CurrentVersion=nil；slug 冲突包括回收站和历史跳转。
- 改名在一个事务检查当前名及历史名冲突，插入旧名跳转并更新 App；多次改名通过同一 App ID 直接指向最新 slug。`AppBySlug` 语义不变，只有内容路由改用 `SlugOwner`。
- 内容侧旧主入口和 `@vN` 均 301，保留子路径、百分号编码与 query；已删除 App 直接 404。
- 版本删除在事务中检查主入口，当前版本 409，已删除/缺失版本 404；不重用编号。层与 blob 继续由现有 GC 按引用清理。
- 撤覆盖目录复用 CommitDataVersion，以主入口当前版本为基线生成新版本、CreatedBy=owner；保留旧版本，不原地改快照。
- 恢复/立即清理只接受已在回收站的 App ID，活跃 App 和不存在 ID 都返回 404；恢复保留当前版本、全部历史版本和 slug 跳转。
- 回收站剩余天数按配置 trash_days 和 Server.Now 向上取整，最低 0 天。
- 手动签发复用 CreateClient；ttl=1d/30d/forever（默认 30d），只存 SHA-256，明文仅 POST 响应出现。GET /agents 的 view model 不含 token 哈希。状态优先级为撤销 > 过期 > 有效。
- 操作记录按事件 ID 倒序最多 200 条，`?app=` 接受稳定 App ID；LEFT JOIN 让清理后仍可查历史。页面显示动作及 JSON 摘要，html/template 自动转义。
- App 详情/业务变更/回收站变更持有现有 lifecycle 读锁，避免 GC 在恢复、回滚或撤覆盖目录期间清理引用。

业务成功写操作调用 ownerAppEvent/ownerEvent → AddEvent → OnEvent；App 操作均带 AppID，actor=owner。动作包括 app.created、app.visibility_changed、app.renamed、app.promoted、app.version_deleted、data.removed、app.deleted、app.restored、app.purged、agent.issued、agent.revoked。token 明文不进入审计或日志。

### 页面清单

以下全部在 admin 域名。模板根目录为 `internal/server/views/`。模板使用 `html/template`，数据结构在 `admin.go` / `console.go`；UI 任务只改模板/静态资源，保留现有路由、字段和模型含义。

| 页面 / 路由 | 模板与 view model | 表单 action 与字段 |
|---|---|---|
| 登录 `GET /login?next=`；错误密码时 `POST /login` 返回 401 并重绘 | `login.html`；LoginView：Next、Error、AdminURL | POST `/login`：password、next。成功 302 到安全 next，默认 `/`。 |
| 首页 `GET /` | `home.html`；HomeView：Message、Apps []AppRow、Pending []*store.DeviceRequest、Ready bool | POST `/apps`：slug、title、visibility=private/public，默认 private；另有公共会话表单。待批准项链接 `/device?code=<UserCode>`。 |
| 未设置密码时的任意 admin 请求 | `home.html`；HomeView.Message 为设置提示、Ready=false；不显示业务内容 | 无业务表单。保持 auth 的现有行为。 |
| App 详情 `GET /apps/{slug}` | `app.html`；AppView：App、MainURL、CreatorName、Versions、Overlays、Manifest、Events | POST `/apps/{slug}/visibility`：visibility；`/rename`：slug；`/promote`：version（正整数）；`/versions/{n}/delete`：无字段；`/data/remove`：dir；`/delete`：无字段。后三种删除/撤除使用 data-confirm。另有公共会话表单。 |
| 回收站 `GET /trash` | `trash.html`；TrashView.Apps []TrashRow | POST `/trash/{id}/restore`：无字段；`/trash/{id}/purge`：无字段，data-confirm。另有公共会话表单。 |
| Agent 管理 `GET /agents`；手动签发结果 `POST /agents` | `agents.html`；AgentsView：Agents []AgentRow、Token、IssuedName、ExpiresAt | POST `/agents`：name、ttl=1d/30d/forever；POST `/agents/{id}/revoke`：无字段，data-confirm。POST 签发结果显示一次明文，GET 时 Token 为空。另有公共会话表单。 |
| 操作记录 `GET /events?app=` | `events.html`；EventsView：Events []store.AuditEvent、Apps []*store.App、AppFilter | GET `/events`：app（稳定 App ID，空值为全部）；当前用 input + datalist，支持查已删除/清理的 App。另有公共会话表单。 |
| 设备批准 `GET /device?code=`；批准/拒绝的结果页 | `device.html`；DeviceView：Request *store.DeviceRequest、Message | POST `/device/{id}/approve`：name、ttl=1d/30d/forever；POST `/device/{id}/deny`：无字段。处理后 Request=nil，Message=approved/denied；不存在、过期、已处理也显示 Message。保持 auth 的字段和含义。 |
| 私有查看跳转 `GET /view-auth?next=` | 无模板 / view model；需要 admin 会话，无会话先跳登录 | 无表单；签票后 302 到 apps 域名 `/_/auth?ticket=…&next=…`，后者兑换 viewer 会话并跳原路径。UI 无需新增密码页。 |
| 退出 `POST /logout`、全部退出 `POST /logout-all` | 无独立模板，操作后 302 `/login` | 无字段，已在公共导航提供表单。 |
| 静态资源 `GET /static/*` | embed 的 `views/static/*`，无 view model | `admin.js` 只监听 submit，读取 form 的 data-confirm 文案并调用 window.confirm；取消则 preventDefault。不使用内联脚本。 |

`shared.html` 定义两个可复用模板：`nav`（App/回收站/Agent/操作记录链接和退出表单）与 `audit`（直接接收 []store.AuditEvent）。五个业务页面都调用 nav，并加载 `/static/admin.js`；已有 login/device 模板保持 auth 原状。所有路径跳转、表单字段都无需前端构建工具。

#### 嵌套字段（供 UI 直接使用）

时间均为 Unix 秒；指针字段可能为 nil，模板用 `if` / `with` 判断。`html/template` 会解引用已判断存在的指针。

| 类型 | 字段 |
|---|---|
| store.App / AppView.App | ID、Slug、Title、Visibility、CreatedBy；CurrentVersion *int；NextVersion int；CreatedAt、UpdatedAt int64；DeletedAt *int64。 |
| AppRow | 嵌入 *store.App，另加 CreatorName。 |
| VersionRow | 嵌入 *store.Version：AppID、Number、Kind、BaseLayer、Manifest、DataDir *string、Message、CreatedBy、CreatedAt、Overlays；另加 PublisherName、URL、Current bool。UI 展示 Number/Kind/DataDir/Message/PublisherName/CreatedAt，URL 为打开链接。 |
| OverlayRow | 嵌入 store.Overlay：Dir、Layer、PushedBy、PushedAt；另加 PublisherName。撤除表单的 dir 使用 Dir。 |
| bundle.Manifest / AppView.Manifest | App、Title、Data map[string]string（目录 → 描述）；当前主入口版本的 App 包声明。 |
| TrashRow | 嵌入 *store.App，另加 DaysRemaining int；恢复/清理 action 使用 ID。 |
| AgentRow | ID、Name、Status、LastUsedIP；CreatedAt int64；ExpiresAt、RevokedAt、LastUsedAt *int64。Status 已计算为「有效 / 已过期 / 已撤销」。 |
| AgentsView 的签发字段 | Token、IssuedName string；ExpiresAt *int64（nil 为永久）。Token 为空表示普通列表；非空仅为本次签发响应。 |
| store.AuditEvent | ID、At int64；Actor、ActorName、Action、AppID、AppSlug、Detail string。AppSlug 在 App 已清理时为空，此时仍可展示/筛选 AppID。 |
| store.DeviceRequest（首页 Pending 与批准页 Request） | ID、UserCode、Name、Meta（JSON 文本：hostname/os/arch/cli_version/request_ip）、Status；ApprovedName *string、TokenTTL *int64、ClientID *string；CreatedAt、ExpiresAt int64；LastPolledAt *int64。DeviceCodeHash 是内部字段，模板不得输出。 |

表单失败通常返回 400（无效输入）、404（不存在/已删资源）、409（slug 冲突/删除当前版本），保持朴素错误响应；没有额外错误模板。新建、改名和恢复成功跳详情；删除/清理成功跳回收站；其他 App 操作跳详情；撤销跳 Agent 列表。

## 验证与证据

证据根目录：`tmp/tinyhost-v1/work/th-m2-console/`。

- `bdd-red.log`：四项点名验收及两个补充服务端测试先全部因新路由 404 失败；`store-red.log`：存储行为测试先因未实现方法失败。
- `test.log`：`mise exec -- make test` 全仓通过；`test.json`：`mise exec -- go test -json ./...` 结构化输出；`count-tests.py` / `test-counts.log`：100 个顶层、含子测试 136 项通过，无失败或测试跳过（无测试包的 package skip 不算跳过用例）。
- 顶层分布：server 44、store 20、CLI 19、server 入口 5，其余包 12。新增 server 6、store 3，auth 原有测试不减。
- `race.log`：`mise exec -- go test -race ./internal/store ./internal/server` 通过；`vet.log`、`gofmt.log` 均为空，未运行额外风格 linter。
- `smoke.py` / `smoke.log` / `serve.log`：真实进程由脚本持有和回收，以 curl 完成登录、新建、改可见性、手动签发、发布、改名及旧主入口/版本跳转、删除和恢复。日志列出 curl 命令与状态/Location，Bearer 和签发响应明文脱敏；结束时删除 cookie jar 和临时响应头。
- 冒烟命令：`uv run --no-project tmp/tinyhost-v1/work/th-m2-console/smoke.py`。数据位于该目录的 `data/`，服务参数为 `--listen 127.0.0.1:3471 --apps-url http://localhost:3471 --admin-url http://admin.localhost:3471 --insecure-cookies`；curl 用 --resolve 解析 admin.localhost。
- 冒烟前执行 `agent-inspect-session.sh`：context 17%；服务最终 exit 0，`lsof -nP -iTCP:3471 -sTCP:LISTEN` 无监听。

点名验收：TestRenameRedirectsOldPathsAndReservesOldSlug、TestDeleteVersionAndCurrentVersionIsProtected、TestRestoreFromTrash、TestEventsRecordedForOwnerAndAgentActions 均存在并通过。TestAppPageCannotPerformControlledActions 同时覆盖 admin 批准、删除 App、改可见性的恶意 Origin（全部 403），以及原有六条无 Bearer 的 Agent API 写请求（401）。审计测试同时覆盖 Agent 的发布/推数据/回滚/删除和 Owner 的批准/撤销/可见性等动作，并验证 AppID 不丢失。

## 修改的文件

- `internal/server/admin.go`：接入业务路由、扩充 HomeView、增加可关联 App 的 Owner 审计 helper。
- `internal/server/console.go`：业务 handler、具名 view model、输入检查与错误映射。
- `internal/server/content.go`：历史 slug 301 跳转，保留路径与 query。
- `internal/server/console_test.go`、`internal/server/auth_test.go`：业务行为和跨源拒绝用例。
- `internal/store/console.go`、`internal/store/console_test.go`：查询/事务操作和存储行为测试。
- `internal/server/views/{home,app,trash,agents,events,shared}.html`、`views/static/admin.js`：朴素页面和外部删除确认。
- 本总结：页面和模型契约、证据、后续接手说明。

## AGENTS.md 建议（交给协调者）

可将现有 admin 代码地图一条补为：

- `internal/server/console.go`：M2 后台业务 handler 和具名 view model；页面/表单/嵌套字段契约见 `kb/sessions/2026-11-02-th-m2-console.md`「页面清单」，改界面前先读。所有路由在 adminHandler 注册并继承统一安全中间件；新增 App 写操作用 ownerAppEvent 保留 AppID。
- `internal/store/console.go`：RenameApp 以事务同时改名并预留旧名，DeleteVersion 以事务保护当前版本；恢复/清理只处理回收站。Events 返回最近 200 条并按稳定 App ID 筛选，App 清理后仍保留审计。

## 交给协调者

- 按 brief，未编辑 AGENTS.md、next-up、known-issues、plan/spec 勾选框及协调记录；开工时已有的 AGENTS.md 工作区修改原样保留，不纳入本提交。
- 现有已签收 schema 的 slug_redirects 使用 ON DELETE CASCADE，因此回收站阶段仍预留所有旧名，purge/GC 删除 App 行后会释放现名及历史名。本任务沿用该生命周期，没有改迁移；总 plan 的「旧路径永久跳转」应按 App 尚存在来理解。若要求清理后也永不复用，需要另设计独立墓碑保留，不能只改 handler。
- UI 可直接接手上面的页面清单；真实浏览器恶意脚本、手机/桌面截图与整体 M2 手验仍由后续任务进行。

## 交接

本任务完成标准全部满足，无范围内未完成事项。工作保持在当前 master，未 push、merge、打 tag，未提前实现 M3 或生产部署。

## 相关文档

- [总 plan](../plans/2026-11-02-tinyhost.md) — 权限、URL 和事务设计依据。
- [M2 spec](../plans/2026-11-02-tinyhost-auth-console-spec.md) — 后台业务、验收与路由依据。
- [auth 总结](2026-11-02-th-m2-auth.md) — 本任务沿用的 admin 骨架和字段契约。
