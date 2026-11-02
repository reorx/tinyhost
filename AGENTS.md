# tinyhost

给 Agent 用的自托管静态网站发布服务。Agent 用 CLI 把一个目录发布成 App（`tinyhost.space/<slug>/`），每次发布生成一个不可变版本。Agent 通过设备授权自助接入，Owner 在后台（`th.reorx.com`）批准。apps 用独立的注册域名，是为了不和 reorx.com 下其他用 cookie 的服务同站（spec 原来写的是 `th.reorx.com` / `th-admin.reorx.com`）。Go 编写，无 CGO，存储是 SQLite 加内容寻址的文件块。开发方法论是 BDD：先写行为测试，再实现。

## 现状

- 2026-11-02：M1 核心发布、M2 授权与后台、M3 预览与分发已完成，都经过本地真实浏览器验收（截图在 `tmp/2026-11-02-m1-core-publish/`、`tmp/2026-11-02-m2-*`、`tmp/2026-11-02-m3-ui/`）。M3 的 Linux 容器安装验收并入 M4 验收 2。总 plan 和四期 spec 在 `kb/plans/2026-11-02-tinyhost*.md`。
- M4 上线进行中：发版流水线已完成（`167a9d1`），仓库已公开在 `github.com/reorx/tinyhost`（MIT），`v0.1.0-rc.1` 已发布 Release 和 ghcr 镜像，并已部署到线上（apps `https://tinyhost.space`，后台 `https://th.reorx.com`）。生产部署配置和运维 SOP 在独立的 deploy 仓库（见用户全局 CLAUDE.md 的 DevOps 约定）的 `kb/docs/tinyhost.md`，本仓库不存生产服务器细节、凭据和部署总结。验收发布时要核对线上服务的版本头和 CLI 版本一致。
- **仓库是公开的**：提交、`kb/`、总结里不写密值、token、IP、OSS bucket 名，也不写 reorx.com 上其他服务的安全弱点。运维细节写进 deploy 仓库。

## 关键约束

- 受控操作（发布、删除、批准授权、改设置）只接受两种鉴权：
  - Agent API：`Authorization: Bearer`。
  - admin 域名：`__Host-th_admin` 会话，并且每个 POST 都要校验 Origin。

  apps 域名上的 cookie（`__Host-th_view`）只能用来读私有内容，任何写接口都不能接受它。原因：apps 域名托管的是不可信的 Agent 产出，同域的脚本能带上该域名的所有 cookie。
- 用户内容永远不在 `/` 和 `/_/` 下提供，服务端也不发 `Service-Worker-Allowed`。
- App 内容只用相对路径：主入口、`@vN`、`/_p/` 三种前缀共用同一份文件。
- 新版本的生成方式：拿主入口当前指向的版本，替换其中一层（App 包层或某个覆盖目录层）。
- token、会话 ID、票据只存 SHA-256；明文只出现一次。唯一的例外是预览 token：它是短期的，而且后台要显示链接，所以存明文，它也不能授权任何写操作。数据库文件权限是 0600。
- `internal/api` 的 DTO 和 `--json` 输出从 M1 起冻结：字段只增不改，新增字段必须可选。
- 服务端核心不 import `internal/notify`。事件通过回调传出，由 `cmd/tinyhost-server` 接线。

## 约定

- Go 版本由 mise 管理（`mise.toml`，当前 1.27.3），命令前加 `mise exec --`。Go 代码保持 gofmt 格式、一行一条语句。CLI 用 stdlib `flag` + `register(&command{...})`，SQLite 的打开方式和迁移照 `~/Code/hookploy`。
- 日志一律用 slog 输出 JSON。
- 测试名写成行为句子，函数上方加 `// Behavior:` 注释。`--json` 输出用 golden 快照，更新命令：`go test ./internal/cli -update`。
- 本地手动验证用 `localhost:3471`（apps）和 `admin.localhost:3471`（admin），服务端加 `--insecure-cookies`。

## 代码地图

- `internal/version`：`Version` 变量，用 `-ldflags -X` 注入。
- `internal/token`、`internal/slug`：凭证的生成和哈希；slug 与 `@vN` 的校验。
- `internal/blob`：内容寻址存储。Put 写完才原子发布；复用同一块时刷新 mtime。临时目录和 blobs 目录必须在同一个文件系统上（发布用硬链接）。
- `internal/bundle`：目录或单文件打包成 tar.gz，服务端流式安全解包。根目录的 `tinyhost.json` 只当元数据，不作为网页文件提供。Pack 生成的临时文件由调用者清理。`CheckAbsolutePaths` 只在 CLI 侧扫描 HTML/CSS/JS 等文本里的绝对路径，结果放进 `Summary.PathWarnings`（File/Line/Text），不阻止上传；单个文件最多扫 1MiB，每个文件最多 5 条、总共最多 20 条。
- `internal/store`：内嵌 `schema.sql`，用 `user_version` 迁移。`CommitAppVersion` / `CommitDataVersion` 在一个事务里写层、版本、主入口；参数里的 app 用 ID；数据层的路径要预先加上规范化的覆盖目录前缀。`LookupVersionFile` 在覆盖层里找不到文件时不回落到 App 包层；补 `.html`、404 等 HTTP 策略由 server 负责。
  - M2（`user_version=2`）新增授权申请、会话、查看票据、settings、`slug_redirects`。`PollDevice` 在一个事务里创建并消费 client。`SlugOwner` 同时查现名和历史跳转；`CreateApp` 上的插入触发器阻止复用旧 slug。
  - M3（`user_version=3`）新增 `previews`，在 `previews.go`：按 (client, local_key) 唯一，`CommitPreview` 原子替换层和到期时间。GC 先把过期预览的 layer 置 NULL，七天后删除整行；孤儿层的两处判断（`OrphanLayers` / `DeleteOrphanLayer`）都要把 `previews.layer` 算作引用。`Store.Open` 会把新建和已有的数据库设为 0600。
- `internal/distribution`：`/_/dl/` 下载代理，只提供当前版本 `checksums.txt` 里列出的单段文件名；从远端下载、命中缓存、读本地 `--dl-dir` 时都校验 SHA-256，临时文件校验通过后才 rename。远端缓存在 `<data-dir>/dl/<version>/`。`scripts/dist.sh`（`make dist VERSION=…`）生成六个平台的 CLI 包（Windows 是 zip，其余 tar.gz）、linux amd64/arm64 与 darwin arm64 的服务端包和统一的 checksums；文件名里的版本不带 `v`，macOS 上打包用 `COPYFILE_DISABLE=1` 避免 AppleDouble 混进包里。`scripts/accept-install.sh` 在全新的 HOME 里验证安装、skill 和 login（验收 6）。
- `internal/notify`：容量 100 的异步队列，默认事件 `device.requested` / `app.created` / `app.deployed`；ntfy 和 webhook 每个请求 10 秒超时、不重试，队列满或发送失败只记日志。事件消息的组装和回调接线在 `cmd/tinyhost-server/notify.go`。`device.requested` 的 detail 里不能放 device_code（它是秘密）。
- `internal/api`：HTTP API 和 CLI `--json` 共用的 DTO，接口契约见 `docs/json-output.md`。CLI 直接输出这些 DTO，不输出 store 的模型。
- `internal/server`：按 Host 分流、只认 Bearer 的 Agent API、内容解析、GC。`Server.Now` 可注入时钟；`OnEvent` 在审计记录落库后通知上层。上传和 GC 之间有一把生命周期锁，保护「blob 写完到 SQL 提交」这段窗口，上传之间仍然并发。
- `internal/server/previews.go`：预览 API 只认 Bearer，列表和删除按 client 隔离；DELETE 除了 ID 也接受链接里的 token，但仍检查创建者。`/_p/{token}/` 不需要登录，公开读取，内容解析复用 `content.go` 的 `resolveContent`；按 `Server.Now` 判断到期，过期就返回 410。
- `internal/server/distribution.go`、`errors.go`：apps 根路径说明页、`/skill.md`、`/install.sh`、apps 域名的 `/_/static/`（服务端自己的可信资源，不是用户内容），以及后台的通用错误页。admin 的业务错误输出 HTML 错误页，Agent API 仍是 JSON，状态码都不变。这两页的路由和 view model 见 `kb/sessions/2026-11-02-th-m3-dist.md`「页面清单增量」。所有 Agent API 响应（包括错误）都带 `X-Tinyhost-Version`。
- `internal/server/admin.go`：admin 域名的路由都加在 `adminHandler` 里，`adminMiddleware` 统一负责会话检查和所有 POST 的 Origin 校验。模板和静态文件在 `views/`，用 embed 打包；handler 传给模板的是具名 view model，路由、表单字段名、view model 字段只增不改。`Server.Now` 同时控制 token、会话、设备授权和限速的时钟。admin 页面的 CSP 统一是 `form-action 'self'`，只有 `/login` 的响应额外允许 apps 源：登录后的 302 链最后一跳是 apps 域名的 `/_/auth`，Chrome 把整条链都算作表单提交，不放行就回不到私有 App（见 `kb/sessions/2026-11-02-th-m2-fix2.md`）。
- `internal/server/console.go`：后台业务页面的 handler 和 view model。所有页面的路由、表单字段、view model 字段见 `kb/sessions/2026-11-02-th-m2-console.md` 的「页面清单」和 `kb/sessions/2026-11-02-th-m3-preview.md` 的「页面清单增量」（后台预览页、apps 域名的过期页），改界面前先读。新增的 App 写操作用 `ownerAppEvent` 记审计，保留 AppID。
- `internal/server/views/`：`shared.html` 定义 `meta`、`assets`（参数是静态前缀：admin 用 `"/static"`，apps 域名用 `"/_/static"`，都是绝对路径，所以在 `/_p/<token>/` 下任意深度都能加载）、`head`（参数是页面标题，等于 `meta` + 标题 + `assets "/static"`）、`nav`（参数是当前页 key：`apps` / `previews` / `trash` / `agents` / `events`）、`brand`（后台）、`site-brand`（apps 域名，不带「后台」）、`audit`、`action`（动作码 → 中文）、`visibility`、`code`（验证码加连字符）和 `i-*` 图标。apps 域名的 `root.html`、`preview-expired.html` 不放后台导航，也不能出现表单或密码框（有测试检查）。错误页 `error.html` 用 solo 布局，要保留 `data-error-page` 标记。样式全部在 `static/admin.css`，浅色和深色的令牌在 `:root`；全局 `[hidden]` 带 `!important`，因为 `.btn` 的 display 会盖过它。`static/admin.js` 负责 `data-confirm`、`<time data-ts>` 按本地时区显示、`data-copy`、`data-back`（有历史记录时才显示）和账户菜单。CSP 不允许 `style` 属性、`<style>` 和内联 `<script>`，样式只能写进 CSS 文件。操作记录页的审计表要保持字面量 `<table>`，筛选放在表格前面（测试按它切分）。模板和静态文件是 embed 进去的，改了 `views/` 之后要重新 build `tinyhost-server` 再做本地验证。
- `internal/store/console.go`：`RenameApp` 在一个事务里改名并预留旧名；`DeleteVersion` 在同一事务里拒绝删除当前版本；恢复和立即清理只作用于回收站里的 App。`Events` 返回最近 200 条，按稳定的 App ID 筛选，App 清理后审计记录仍在。旧 slug 的预留在 App 被清理（purge）后释放。
- `internal/cli`：Agent 用的 CLI，stdlib `flag` + `register` 分发。远端命令直接输出 `internal/api` 的 DTO；JSON 的成功和错误都走 stdout，警告走 stderr。服务器地址的优先级：`--server` > `TINYHOST_SERVER` > `os.UserConfigDir()/tinyhost/config.json`。凭证按服务器分别保存，环境变量 `TINYHOST_TOKEN` 优先；凭证文件和待完成的 login 申请都以 0600 原子写入。`login` 等待超时退出码 4，再运行会继续同一个申请。`skill` 从根目录 `skill/` 包嵌入并渲染。CLI 只在能解析成语义版本、且比服务器旧时，在 stderr 末尾提示升级，dev 版本不提示，不影响 stdout 和退出码。`preview` / `previews` / `previews rm`：本地 `previews.json` 把「服务器|绝对路径」映射到 local_key，也以 0600 原子写入；`--new` 会换一个新 key 并保存，之后不带 `--new` 的 preview 更新的是新链接。deploy、data push、preview 的路径警告走 stderr，`--json` 输出里多一个可选的 `warnings`。
- `skill/`：`SKILL.md` 和 `install.sh` 都 embed 进来。`Render(server)` 填入服务器地址和构建版本，`/skill.md` 和 `tinyhost skill` 共用它，所以服务器和 CLI 要用同一个 VERSION 构建。`RenderInstall` 对 shell 和 JSON 做安全引用。安装脚本写 `config.json`（0600）的目录必须和 Go 的 `os.UserConfigDir` 一致：macOS 是 `~/Library/Application Support/tinyhost`，Linux 遵守 `XDG_CONFIG_HOME`。
- `cmd/tinyhost`：只调 CLI 的 Run 并返回退出码，不引入服务端和 SQLite 的依赖。
- `cmd/tinyhost-server`：`serve` / `passwd` / `token create` / `gc` / `version`。`serve` 必须给出和 apps 不同 Host 的 `--admin-url`；`--insecure-cookies` 只用于本地。数据库在 `<data-dir>/tinyhost.db`；相对的数据目录在入口处先转成绝对路径再传给 `store.Open`。
- `.github/workflows/`：CI 在 push 和 PR 时跑 gofmt 检查、测试和 vet，Go 版本和 mise 一样锁在 1.27.3。`v*` tag 触发 Release 和 ghcr 的双架构镜像；版本号带连字符的是 prerelease，镜像不推 `latest`。已推的 tag 不移动、不删除，修好后递增 rc 号。
- `Dockerfile`：直接用同版本 `make dist` 产出的服务端包，不重新编译；基于 distroless/static，只含服务端，数据在 `/data`，默认监听 `:3471`。本机不跑 Docker，镜像的构建和双架构验证都在 CI 里。

## 文档

- `kb/plans/2026-11-02-tinyhost.md` — 总 plan：概念、URL、权限、安全决策、分期。实现任何一期之前，先读它和那一期的 spec。
- `docs/json-output.md` — HTTP API 与 `--json` 的输出契约，字段只增不改。
