# tinyhost

给 Agent 用的自托管静态网站发布服务。Agent 用 CLI 把一个目录发布成 App（`th.reorx.com/<slug>/`），每次发布生成一个不可变版本。Agent 通过设备授权自助接入，Owner 在后台（`th-admin.reorx.com`）批准。Go 编写，无 CGO，存储是 SQLite 加内容寻址的文件块。开发方法论是 BDD：先写行为测试，再实现。

## 现状

- 2026-11-02：只有方案，还没有代码。总 plan 和四期 spec 在 `kb/plans/2026-11-02-tinyhost*.md`，从 M1 开始实现。
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

- Go 版本由 mise 管理（`mise.toml`）。CLI 用 stdlib `flag` + `register(&command{...})`，SQLite 的打开方式和迁移照 `~/Code/hookploy`。
- 日志一律用 slog 输出 JSON。
- 测试名写成行为句子，函数上方加 `// Behavior:` 注释。`--json` 输出用 golden 快照，更新命令：`go test ./internal/cli -update`。
- 本地手动验证用 `localhost:3471`（apps）和 `admin.localhost:3471`（admin），服务端加 `--insecure-cookies`。

## 文档

- `kb/plans/2026-11-02-tinyhost.md` — 总 plan：概念、URL、权限、安全决策、分期。实现任何一期之前，先读它和那一期的 spec。
