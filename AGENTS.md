# tinyhost

给 Agent 用的自托管静态网站发布服务。Agent 用 CLI 把一个目录发布成 App（`th.reorx.com/<slug>/`），每次发布生成一个不可变版本。Agent 通过设备授权自助接入，Owner 在后台（`th-admin.reorx.com`）批准。Go 编写，无 CGO，存储是 SQLite 加内容寻址的文件块。开发方法论是 BDD：先写行为测试，再实现。

## 现状

- 2026-11-02：M1 核心发布已完成（基础库、服务端、CLI、本地手动验收）；token 暂时只能用 `tinyhost-server token create` 签发后放进 `TINYHOST_TOKEN`。下一步是 M2 授权与后台。总 plan 和四期 spec 在 `kb/plans/2026-11-02-tinyhost*.md`。
- 生产部署在 deploy 目录管理（见用户全局 CLAUDE.md 的 DevOps 约定），M4 才上线。

## 关键约束

- 受控操作（发布、删除、批准授权、改设置）只接受两种鉴权：
  - Agent API：`Authorization: Bearer`。
  - admin 域名：`__Host-th_admin` 会话，并且每个 POST 都要校验 Origin。

  apps 域名上的 cookie（`__Host-th_view`）只能用来读私有内容，任何写接口都不能接受它。原因：apps 域名托管的是不可信的 Agent 产出，同域的脚本能带上该域名的所有 cookie。
- 用户内容永远不在 `/` 和 `/_/` 下提供，服务端也不发 `Service-Worker-Allowed`。
- App 内容只用相对路径：主入口、`@vN`、`/_p/` 三种前缀共用同一份文件。
- 新版本的生成方式：拿主入口当前指向的版本，替换其中一层（App 包层或某个覆盖目录层）。
- token、会话 ID、票据只存 SHA-256；明文只出现一次。
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
- `internal/bundle`：目录或单文件打包成 tar.gz，服务端流式安全解包。根目录的 `tinyhost.json` 只当元数据，不作为网页文件提供。Pack 生成的临时文件由调用者清理。
- `internal/store`：内嵌 `schema.sql`，用 `user_version` 迁移。`CommitAppVersion` / `CommitDataVersion` 在一个事务里写层、版本、主入口；参数里的 app 用 ID；数据层的路径要预先加上规范化的覆盖目录前缀。`LookupVersionFile` 在覆盖层里找不到文件时不回落到 App 包层；补 `.html`、404 等 HTTP 策略由 server 负责。
- `internal/api`：HTTP API 和 CLI `--json` 共用的 DTO，接口契约见 `docs/json-output.md`。CLI 直接输出这些 DTO，不输出 store 的模型。
- `internal/server`：按 Host 分流、只认 Bearer 的 Agent API、内容解析、GC。`Server.Now` 可注入时钟；`OnEvent` 在审计记录落库后通知上层。上传和 GC 之间有一把生命周期锁，保护「blob 写完到 SQL 提交」这段窗口，上传之间仍然并发。
- `internal/cli`：Agent 用的 CLI，stdlib `flag` + `register` 分发。远端命令直接输出 `internal/api` 的 DTO；JSON 的成功和错误都走 stdout，警告走 stderr。服务器地址的优先级：`--server` > `TINYHOST_SERVER` > `os.UserConfigDir()/tinyhost/config.json`。M1 的 token 只从 `TINYHOST_TOKEN` 读。
- `cmd/tinyhost`：只调 CLI 的 Run 并返回退出码，不引入服务端和 SQLite 的依赖。
- `cmd/tinyhost-server`：`serve` / `token create` / `gc` / `version`。数据库在 `<data-dir>/tinyhost.db`；相对的数据目录在入口处先转成绝对路径再传给 `store.Open`。

## 文档

- `kb/plans/2026-11-02-tinyhost.md` — 总 plan：概念、URL、权限、安全决策、分期。实现任何一期之前，先读它和那一期的 spec。
- `docs/json-output.md` — HTTP API 与 `--json` 的输出契约，字段只增不改。
