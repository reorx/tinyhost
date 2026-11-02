---
created: 2026-11-02
tags:
  - tinyhost
  - publish
  - versioning
  - storage
---

# tinyhost M1：核心发布

这是 [tinyhost 总 plan](2026-11-02-tinyhost.md) 的第一期。核心概念、URL 规则、权限表见总 plan，这里不重复。

## PRD

### 背景

先把主链路做出来：上传一个目录 → 拿到版本链接和主入口 → 能回滚、能删除；同时支持单独推数据。授权流程在 M2，这一期先用服务端命令签发的 token。

### 目标

Agent 用 CLI（或 curl）把目录发布成 App、单独推数据、查看版本、回滚、删除。我在浏览器里按路径打开公开 App 的主入口和任意版本。

### 功能与交互

**准备 token**（临时方式；M2 之后保留，作为"手动签发"的后端）
- 在服务器上运行 `tinyhost-server token create --name dev-agent`，token 明文只输出这一次。
- Agent 侧设置环境变量 `TINYHOST_SERVER` 和 `TINYHOST_TOKEN`。

**发布 App 包：`tinyhost deploy [路径]`**
- 路径可以是目录（根下必须有 `index.html`），也可以是单个 `.html` 文件（当作 `index.html` 发布）。不写路径就用当前目录。
- 新建：`tinyhost deploy ./dist --app q3-report --create [--public] [--title "Q3 销售看板"]`
  - slug 已被占用（包括回收站里的 App）：报错，说明它是谁、何时创建的，提示换个名字，或者去掉 `--create` 改为推送新版本。
  - slug 不合法或是保留字：报错并给出规则。
  - 默认私有；加 `--public` 才公开。
- 更新：`tinyhost deploy ./dist --app q3-report [-m "加上环比"]`
  - App 不存在：报错，提示加 `--create`。
  - App 是别的 Agent 创建的：照常发布，输出里注明创建者。
- 不给 `--app` 时读目录下 `tinyhost.json` 的 `app` 字段；两处都没有就报错，提示用 `--app`。
- 第一次 `--create` 成功后，CLI 把 `app` 写进目录下的 `tinyhost.json`（文件不存在就新建，存在就只改这个字段）。之后在这个目录直接 `tinyhost deploy` 即可。
- `--no-promote`：只生成新版本，主入口不动。
- 发布成功的输出：
  ```
  ✓ q3-report v4 已发布（12 个文件，340 KB）
    版本链接：https://th.reorx.com/q3-report@v4/
    主入口：  https://th.reorx.com/q3-report/
  ```
  用了 `--no-promote` 时，最后一行变成"主入口仍指向 v3"。
- 上传前自动排除：点开头的文件和目录（`.well-known` 除外）、`node_modules`、`tinyhost.json` 本身。排除后为空、根下没有 `index.html`、超过 50MB 或 1000 个文件：直接报错，不上传。
- 新版本沿用主入口当前版本的所有覆盖目录，所以重新发布 App 包不会冲掉别人推的数据。

**推数据：`tinyhost data push <路径> --app <slug> --to <目录>/ [-m 说明]`**
- 路径是目录或单个文件，内容放进 App 的 `<目录>/` 下，整体替换这个目录原有的覆盖内容。
- 覆盖目录不能是根，不能以 `.` 开头，不能和已有的覆盖目录互相嵌套（已有 `data/` 时不能推 `data/sales/`，反过来也不行）。违反时报错，并列出现有的覆盖目录。
- 生成一个新版本（类型为"数据"），在主入口当前版本的基础上替换这一个目录，并立即成为主入口。输出格式同 deploy，额外注明目录。
- 撤掉：`tinyhost data rm --app <slug> --to <目录>/`。同样生成一个新版本，App 包里原来的内容重新露出来。目录不存在时报错。
- App 还没发布过 App 包也能推数据（例如 Owner 先在后台建了空 App），这时主入口只有数据目录有内容。

**`tinyhost.json`（可选，放在 App 包根下，不会被当作网页文件提供）**
```json
{
  "app": "q3-report",
  "title": "Q3 销售看板",
  "data": { "data/": "sales.json：按天的销售额数组，字段见 data/README.md" }
}
```
- `title`：`--create` 时作为 App 标题（`--title` 优先），之后每次 deploy 都更新标题。
- `data`：声明这个 App 需要的数据目录和格式说明，给数据 Agent 看；不强制。

**查询**
- `tinyhost apps`：列出所有 App 的 slug、标题、可见性、当前版本、创建者、最近更新时间。
- `tinyhost info <slug>`：链接、可见性、创建者、当前版本，App 包声明的数据目录及说明，现有覆盖目录（谁、何时更新）。
- `tinyhost versions <slug>`：版本号、类型（App 包，或"数据 + 目录"）、发布者、说明、时间，并标出主入口所在的版本。
- `tinyhost whoami`：当前 token 对应的 Agent 名称和服务器地址。
- 所有命令都支持 `--json`。

**回滚：`tinyhost promote <slug> <vN>`**
- 把主入口切到这个版本。版本不存在或已被清理时报错。

**删除：`tinyhost delete <slug>`**
- 只能删自己创建的 App，删别人的会报错并说明创建者。
- 删除后链接立即 404；App 进回收站，7 天后彻底清理，这期间 slug 不能被重新使用。输出里提示"7 天内可以请 Owner 在后台恢复"。

**版本保留**
- App 包版本全部保留。只改了数据的版本，每个 App 保留最近 20 个；主入口所在的版本永远不清理。被清理的版本链接显示 404。

**浏览**
- 公开 App：`/slug/` 打开主入口，`/slug@vN/` 打开指定版本。
- 路径解析：`/slug/about` 依次找 `about`、`about.html`、`about/index.html`（找到最后一种时，跳转到 `/slug/about/`）。都找不到就用 App 根下的 `404.html`，再没有就显示 tinyhost 自己的 404 页。
- 访问覆盖目录下的路径时只看覆盖内容；覆盖内容里没有的文件直接 404，不会回落到 App 包。
- 私有 App：这一期一律显示"需要登录"页，不泄露任何内容；登录查看在 M2 做。
- 从未发布过的 App 显示"尚未发布"页。已删除的 App 和不存在的 slug 显示 404。
- 根路径 `/` 显示一段说明："这是一个 tinyhost 服务，Agent 请阅读 /skill.md"（skill 正文在 M3 补全）。

**没有 CLI 的环境**
- 可以用 tar + curl 直接上传，效果和 deploy、data push 完全相同。写法放进 skill。

### 不做的事

- 授权申请和后台（M2）；预览、skill 正文、安装脚本、上传前的路径检查（M3）。
- 私有 App 的登录查看；改可见性、改 slug、恢复已删除的 App（都在 M2 后台）。
- 断点续传、增量上传。

### 验收标准

1. 用 `--create` 发布一个目录，得到 v1 的版本链接和主入口，两个都能在浏览器打开，页面里的相对路径资源都能加载。
2. 再发布一次得到 v2：主入口显示 v2，`@v1` 仍是 v1 的原样内容。
3. 用 `--no-promote` 发布 v3 后主入口仍是 v2；`promote q3-report v3` 之后主入口变为 v3。
4. 对不存在的 App 不带 `--create` 发布会失败；对已存在的 slug 带 `--create` 也会失败，并说明创建者。
5. 另一个 Agent 往 `data/` 推数据，得到新版本，主入口 `data/` 下是新数据。之后写 App 的 Agent 重新 deploy（包里自带旧的 `data/`），主入口 `data/` 下仍是推送的数据。
6. 推数据的目录和已有覆盖目录嵌套时失败；`data rm` 之后，App 包里的 `data/` 重新出现。
7. Agent A 删不掉 Agent B 创建的 App。A 删掉自己的 App 后链接 404，7 天内同名 slug 无法再创建。
8. 目录里的 `.env`、`.git/` 发布后访问不到。
9. 用 curl 上传含 `../` 路径或符号链接的 tar 包会被拒绝。
10. 私有 App 的链接显示"需要登录"，内容不泄露。
11. 连续推 25 次数据后，版本列表里只剩最近 20 个数据版本，App 包版本一个不少。
12. 在旧版本上 `promote` 回滚后再推数据，新版本的 App 包内容来自回滚到的那个版本。

## Tech Design

### 现状

空仓库。参照总 plan 的"仓库结构"和 hookploy 的约定（见总 plan"现状"）。本期搭起 go module、两个入口、`token`、`slug`、`blob`、`store`、`bundle`、`server`（apps 域名部分）、`cli`。

### 方案

- 上传请求的处理顺序：Bearer 鉴权 → 流式解包、校验，同时把文件写进 blob 存储（边写边算 sha256）→ 在一个 SQLite 事务里写入层、版本、覆盖目录，并移动主入口 → 记录事件。
- 内容请求的处理顺序：解析第一段路径（`slug` 或 `slug@vN`）→ 找 App 和版本 → 可见性检查 → 按覆盖目录优先的规则找文件 → `http.ServeContent` 输出。
- 后台 GC 每小时运行一次：清理数据版本、清空回收站，删除不再被引用的层和文件块。

### 数据变动

SQLite schema 写在 `internal/store`，用 `PRAGMA user_version` 做迁移（照 hookploy `store.go`）。时间统一存 unix 秒。

```sql
CREATE TABLE clients (
  id            TEXT PRIMARY KEY,           -- "c_" + 随机
  name          TEXT NOT NULL,
  token_hash    TEXT NOT NULL UNIQUE,       -- sha256 hex
  meta          TEXT NOT NULL DEFAULT '{}', -- json：hostname/os/arch/cli_version/request_ip（M2 填写）
  created_at    INTEGER NOT NULL,
  expires_at    INTEGER,                    -- NULL = 永不过期
  revoked_at    INTEGER,
  last_used_at  INTEGER,
  last_used_ip  TEXT
);

CREATE TABLE apps (
  id               TEXT PRIMARY KEY,        -- "a_" + 随机
  slug             TEXT NOT NULL UNIQUE,    -- 含回收站里的；彻底清理时删除整行
  title            TEXT NOT NULL DEFAULT '',
  visibility       TEXT NOT NULL CHECK (visibility IN ('public','private')),
  created_by       TEXT NOT NULL,           -- client id 或 'owner'
  current_version  INTEGER,                 -- NULL = 从未发布
  next_version     INTEGER NOT NULL DEFAULT 1,
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  deleted_at       INTEGER
);

CREATE TABLE layers (
  id          TEXT PRIMARY KEY,             -- "l_" + 随机
  file_count  INTEGER NOT NULL,
  total_size  INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE TABLE layer_files (
  layer_id  TEXT NOT NULL REFERENCES layers(id) ON DELETE CASCADE,
  path      TEXT NOT NULL,                  -- 相对 App 根的完整路径，覆盖层也存完整路径（data/sales.json）
  blob      TEXT NOT NULL,                  -- sha256 hex
  size      INTEGER NOT NULL,
  PRIMARY KEY (layer_id, path)
);
CREATE INDEX layer_files_blob ON layer_files(blob);

CREATE TABLE versions (
  app_id      TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('app','data')),
  base_layer  TEXT NOT NULL REFERENCES layers(id),
  manifest    TEXT NOT NULL DEFAULT '{}',   -- App 包里 tinyhost.json 解析后的 title / data
  data_dir    TEXT,                         -- kind=data 时本次推送或撤掉的目录
  message     TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (app_id, number)
);
CREATE TABLE version_overlays (
  app_id      TEXT NOT NULL,
  number      INTEGER NOT NULL,
  dir         TEXT NOT NULL,                -- 以 / 结尾，如 data/
  layer       TEXT NOT NULL REFERENCES layers(id),
  pushed_by   TEXT NOT NULL,                -- 这一层最初由谁推送，沿用到后续版本
  pushed_at   INTEGER NOT NULL,
  PRIMARY KEY (app_id, number, dir),
  FOREIGN KEY (app_id, number) REFERENCES versions(app_id, number) ON DELETE CASCADE
);

CREATE TABLE events (
  id      INTEGER PRIMARY KEY,
  at      INTEGER NOT NULL,
  actor   TEXT NOT NULL,                    -- client id 或 'owner'
  action  TEXT NOT NULL,                    -- app.created / app.deployed / data.pushed / data.removed / app.promoted / app.deleted
  app_id  TEXT,
  detail  TEXT NOT NULL DEFAULT '{}'
);
```

- 空 App 第一次推数据时，base_layer 指向一个 0 文件的空层（每次新建一个空层即可）。
- 文件块：`<data-dir>/blobs/<hash前两位>/<hash>`，只写一次、永不修改。上传先写 `<data-dir>/tmp/<随机名>`，边写边算 sha256，然后 rename 到目标路径。目标已存在时删掉临时文件，并用 `os.Chtimes` 刷新目标的 mtime（供 GC 判断，见下）。

### 逻辑变动

**`internal/token`**：照搬 hookploy 的写法。`New()` 生成 `th_` + 43 位 base64url；`Hash()` 返回 sha256 hex；`Equal()` 做常量时间比较。会话 ID 和票据也用它生成（M2）。

**`internal/slug`**
- `Valid(s)`：长度 3–40，只含 `[a-z0-9-]`，首尾必须是字母或数字，不能有 `--`。
- 保留字：`api admin www static assets login logout auth`。
- `ParseSegment(seg)`：返回 `(slug, version int)`；`q3-report@v12` → `("q3-report", 12)`，没有 `@v` 时 version 为 0。

**`internal/bundle`**
- CLI 侧 `Pack(path) (tmpfile, Summary, error)`：
  - 遍历目录，不跟随符号链接（跳过并给出警告）。
  - 应用排除规则（点开头且不是 `.well-known` 的任意一段、`node_modules`、根下的 `tinyhost.json`），但根下的 `tinyhost.json` 仍会单独打进包里给服务端解析。
  - 单个 `.html` 文件打包成 `index.html`。
  - 写 tar.gz 到临时文件，同时检查 50MB / 1000 文件 / 必须有 `index.html`（只对 deploy 检查）。
- 服务端 `Unpack(r io.Reader, limits, sink)`：
  - gzip + tar 流式读取；用累计计数限制解压后的总大小，防止解压炸弹。
  - 只接受普通文件，目录条目忽略；符号链接、硬链接和其他类型一律拒绝整个上传，并指出路径。
  - 路径规范化：去掉开头的 `./`；拒绝绝对路径、`..`、反斜杠、NUL、空段、长度超过 512 字节、重复路径。点开头的段（`.well-known` 除外）静默丢弃，记入 `skipped`。
  - 根下的 `tinyhost.json`（不超过 64KB）解析成 manifest，不入层。
  - 每个文件交给 `sink`，由它写入 blob。
- 服务端校验：限制项可配置，默认 50MB、1000 个文件。

**`internal/blob`**：`Put(r) (hash, size, err)`、`Open(hash) (*os.File, err)`、`Remove(hash)`、`Walk(fn)`（GC 用）。

**`internal/store`**
- 打开方式同 hookploy（WAL、`busy_timeout`、`foreign_keys`、`SetMaxOpenConns(1)`）。
- 写操作每个一个事务，版本号在事务里取 `next_version` 再自增，由 SQLite 单写者保证顺序。
- 主要函数：
  - `CreateClient`、`ClientByTokenHash`、`TouchClient`（最多每分钟写一次）
  - `CreateApp`、`AppBySlug`、`ListApps`、`SoftDeleteApp`
  - `CommitAppVersion(app, layer, manifest, msg, by, promote)`
  - `CommitDataVersion(app, dir, layer|nil, msg, by)`：layer 为 nil 表示撤掉目录
  - `Promote`、`Versions`、`ResolveVersion(app, n)`（返回 base layer + 覆盖层列表）
  - `LookupFile(layer, path)`、`AddEvent`
  - GC 相关：`PruneDataVersions(keep)`、`PurgeTrash(before)`、`OrphanLayers()`、`ReferencedBlobs()`

**`internal/server`（本期只有 apps 域名）**
- 路由入口按 Host 分流：Host 等于 `--apps-url` 的 host → apps；等于 `--admin-url` 的 host → admin（M2）；其他 → 421。
- apps 域名的路由：

| 路径 | 处理 |
|---|---|
| `/` | 说明页 |
| `/robots.txt` | `User-agent: *` / `Disallow: /` |
| `/_/api/v1/...` | Agent API（见下表） |
| `/_/...` 其余、`/_p/...` | 404（M2 / M3 再填） |
| `/{seg}` | 第一段合法时 301 到 `/{seg}/` |
| `/{seg}/{rest...}` | 内容托管 |

- Agent API：鉴权中间件读 `Authorization: Bearer`，查 `token_hash`，检查未撤销、未过期，把 client 放进 context。错误统一返回 `{"error":{"code","message","hint"}}`。

| 方法与路径 | 说明 |
|---|---|
| `GET /_/api/v1/whoami` | `{client, server_version}` |
| `GET /_/api/v1/apps` | App 列表 |
| `GET /_/api/v1/apps/{slug}` | App 详情：可见性、创建者、当前版本、manifest.data、覆盖目录 |
| `POST /_/api/v1/apps/{slug}/deploys` | body 为 tar.gz；query：`create=1`、`visibility=public\|private`、`title=`、`message=`、`promote=0` |
| `POST /_/api/v1/apps/{slug}/data?dir=data/&message=` | body 为 tar.gz，文件路径相对于 dir |
| `DELETE /_/api/v1/apps/{slug}/data?dir=data/&message=` | 撤掉覆盖目录 |
| `GET /_/api/v1/apps/{slug}/versions` | 版本列表 |
| `POST /_/api/v1/apps/{slug}/promote` | JSON `{"version": 3}` |
| `DELETE /_/api/v1/apps/{slug}` | 软删除，仅创建者可用 |

  发布类接口统一返回 `DeployResult{app, version, kind, data_dir, files, size, version_url, main_url, promoted, current_version, app_created_by, skipped}`。
- 错误码：`unauthorized`、`app_not_found`、`slug_taken`（附创建者、创建时间、是否在回收站）、`slug_invalid`、`not_creator`、`bad_bundle`、`too_large`、`overlay_conflict`（附现有目录）、`overlay_not_found`、`version_not_found`。
- 覆盖目录校验：规范化成以 `/` 结尾的相对路径；不能是根，任何一段都不能以 `.` 开头；和现有目录互为前缀时冲突。
- 内容托管：
  1. 解析第一段。App 不存在或已删除 → 404。版本号为 0 时取 `current_version`；为 NULL → "尚未发布"页（404）。指定的版本不存在 → 404。
  2. 私有 App → 401 "需要登录"页（M2 改成跳转登录）。
  3. `rest` 先 URL 解码再 `path.Clean`。为空或以 `/` 结尾时补 `index.html`。
  4. 查找文件：如果路径落在某个覆盖目录下，只查那一层；否则只查 base layer。顺序是精确路径 → `+.html` → `+/index.html`（命中时 301 到带斜杠的地址）。都不中时查根下的 `404.html`（返回 404 状态码），再没有就用内置 404 页。
  5. 响应头：
     - `Content-Type` 按扩展名，用 `mime.TypeByExtension` 并补充 `.js .mjs .json .wasm .webmanifest .md .csv`，文本类型加 `charset=utf-8`。
     - `ETag: "<sha256>"`。
     - `Cache-Control`：主入口 `no-cache`，版本链接 `public, max-age=31536000, immutable`。
     - 固定加 `X-Content-Type-Options: nosniff`、`Referrer-Policy: strict-origin-when-cross-origin`、`X-Robots-Tag: noindex`。
     - 绝不发 `Service-Worker-Allowed`。
     - 用 `http.ServeContent` 处理 If-None-Match、Range、HEAD。
- GC（`gc.go`）：
  - 每小时运行，也可以通过 `tinyhost-server gc` 运行一次。
  - 步骤：清理数据版本，每个 App 保留最近 20 个，跳过主入口所在版本 → 清空 `deleted_at` 早于 7 天的 App（删整行，级联删除版本）→ 删除没有被任何版本或预览引用的层 → 删除不在 `layer_files` 里、且 mtime 早于 1 小时的文件块。
  - mtime 的条件防止误删：上传过程中刚复用、还没提交事务的文件块会被刷新 mtime，从而躲过这一轮。
- 请求日志：slog JSON，每个请求一行，字段为 method、host、path、status、bytes、duration_ms、client。
- 客户端 IP：配置了 `--trusted-proxy-header`（如 `CF-Connecting-IP`）时取该头，否则取 `RemoteAddr`。

**`cmd/tinyhost-server`**
- 子命令用和 CLI 相同的 `register` 分发方式：
  - `serve`
  - `token create --name N [--ttl 30d]`：只在本机直接写 DB，明文只打印一次
  - `gc`
  - `version`
- `serve` 的参数（都可以用环境变量 `TINYHOST_*` 代替）：

| 参数 | 默认值 / 说明 |
|---|---|
| `--apps-url` | 必填 |
| `--admin-url` | M2 起必填，且 host 必须和 apps 不同，否则拒绝启动 |
| `--data-dir` | `./data` |
| `--listen` | `127.0.0.1:3471` |
| `--max-upload-mb` | 50 |
| `--max-files` | 1000 |
| `--data-versions-keep` | 20 |
| `--trash-days` | 7 |
| `--trusted-proxy-header` | 无 |
| `--insecure-cookies` | M2 使用 |

**`internal/cli`**
- 命令分发照 hookploy 的 `register(&command{...})`。本期命令：`deploy`、`data push`、`data rm`、`apps`、`info`、`versions`、`promote`、`delete`、`whoami`、`version`、`help`。`skill` 先输出占位文本（M3 补全）。
- 不带参数运行时打印帮助，第一行是 `AGENTS: run "tinyhost skill" and read it fully before first use.`
- 全局参数和环境变量：
  - `--json`
  - 服务器地址：`--server` > `TINYHOST_SERVER` > 配置文件 `config.json`（M3 的安装脚本写入）
  - token：`TINYHOST_TOKEN` > 凭证文件（M2 起有）
- 退出码：0 成功；1 一般错误；2 用法错误；3 未授权（没有 token、token 无效或已撤销）；4 授权等待中（M2 使用）。
- 人类可读输出照 PRD 的格式；错误输出 `错误：<message>`，下一行 `提示：<hint>`。`--json` 时直接输出 `internal/api` 的结构，错误也输出 JSON。
- `tinyhost.json` 写回：读成 `map[string]any`，只设置 `app`，再以 2 空格缩进写回（字段顺序可能改变，可以接受）。

**文档**
- 新建 `docs/json-output.md`，写 API 与 `--json` 的结构。
- 更新 `AGENTS.md` 的代码地图。

### 实施步骤

- [x] 初始化：`mise use go@1.27`（生成 `mise.toml`）、`go mod init github.com/reorx/tinyhost`、`Makefile`（test / build）、`.gitignore`（`tmp/ dist/ data/`）
- [x] `internal/token`、`internal/slug`，先写测试
- [x] `internal/blob`，先写测试（去重、原子写、mtime 刷新）
- [x] `internal/store`：schema、迁移、查询，先写测试
- [x] `internal/bundle`：Pack / Unpack，先写测试（排除规则、路径安全、限制、单文件模式、tinyhost.json）
- [x] 把验收标准 1–12 写成 `internal/server` 的行为测试（此时全部失败）
- [x] 实现 Agent API 与内容托管，直到测试通过
- [x] GC 及测试（验收 7、11 的清理部分）
- [x] `cmd/tinyhost-server`：serve / token create / gc / version
- [x] `internal/cli` 命令及测试（golden 快照）
- [x] `docs/json-output.md`、`AGENTS.md`
- [x] 本地手动验收：起服务，用一个带 `data/` 的示例看板走完验收 1–6，截图存到 `tmp/<日期>-m1-core-publish/`

### 测试与验证

| 验收 | 测试 |
|---|---|
| 1 | `TestDeployCreateReturnsVersionAndMainURLs`：两个 URL 都返回 index.html，相对路径资源返回 200 |
| 2 | `TestRedeployMovesMainButOldVersionIsImmutable` |
| 3 | `TestNoPromoteKeepsMainAndPromoteSwitchesIt` |
| 4 | `TestDeployToMissingAppRequiresCreate`、`TestCreateOnTakenSlugReportsCreator` |
| 5 | `TestDataPushOverridesBundleDirAndSurvivesRedeploy` |
| 6 | `TestNestedOverlayIsRejected`、`TestDataRmRevealsBundleContent` |
| 7 | `TestOnlyCreatorCanDelete`、`TestDeletedSlugIsReservedUntilPurge`（GC 用注入的时钟） |
| 8 | `TestDotfilesAreNeverServed`（CLI 打包测试和服务端解包测试各一条） |
| 9 | `TestUnpackRejectsTraversalAndSymlinks` |
| 10 | `TestPrivateAppShowsLoginRequired` |
| 11 | `TestGCKeepsLatestDataVersionsAndAllAppVersions` |
| 12 | `TestDataPushBuildsOnCurrentVersionAfterRollback` |

补充：`TestPathResolution`（表驱动：`.html` 补全、目录跳转、自定义 404、覆盖目录不回落）、`TestRevokedOrExpiredTokenIsRejected`、`TestGCDoesNotDeleteFreshlyReusedBlob`。CLI 的 `--json` 输出做 golden 快照。

### 风险与待定

- `modernc.org/sqlite` 单写连接下，大文件上传只在提交时占用写锁，解包和写 blob 都在事务之外完成。需要用 2 个并发的 50MB 上传实测一次，确认不会因为 `busy_timeout` 而失败。
- `tinyhost.json` 写回时字段顺序会变，Agent 一般不在意；如果用户在意，再换成保序的写法。
