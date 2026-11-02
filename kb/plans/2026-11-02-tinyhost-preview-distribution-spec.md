---
created: 2026-11-02
tags:
  - tinyhost
  - preview
  - skill
  - install
  - ntfy
---

# tinyhost M3：预览与分发

这是 [tinyhost 总 plan](2026-11-02-tinyhost.md) 的第三期，依赖 [M1 核心发布](2026-11-02-tinyhost-core-publish-spec.md) 和 [M2 授权与后台](2026-11-02-tinyhost-auth-console-spec.md)。

## PRD

### 背景

M1、M2 让"发布"和"接入"成立，但还缺三样东西：

1. Agent 开发过程中想快速看看效果，不该每次都正式发一个 App 版本。
2. 一个陌生的 Agent 要能只靠服务器地址自己学会怎么用：装 CLI、读说明。
3. 有 Agent 申请授权、有新东西发布时，我希望手机能直接收到通知，不用去翻 Agent 的输出。

另外，Agent 写的页面常用 `/` 开头的绝对路径，在 tinyhost 的路径前缀下会加载失败，需要在上传时就提醒它。

### 目标

- Agent 一条命令拿到临时预览链接，反复修改时链接不变。
- 我对任何 Agent 只说一句话，它就能自己装好、学会、用起来。
- 授权申请和发布会推送到我的手机。

### 功能与交互

**预览：`tinyhost preview [路径] [--ttl 24h] [--new]`**
- 路径规则和 deploy 一样（目录或单个 HTML 文件），大小限制也一样。
- 输出：
  ```
  ✓ 预览已更新，2026-11-03 14:20 过期
    https://th.reorx.com/_p/k7f2m9xq4c8d3h5z/
  ```
- 同一个 Agent 对同一个目录再次运行 preview：沿用原来的链接，内容替换为最新的，过期时间重新计算。我开着的页面刷新一下就能看到新内容。
- `--new`：生成一个新链接，旧链接保留到它自己过期。
- `--ttl`：1 小时到 7 天，默认 24 小时，超出范围时报错。
- 预览链接谁拿到都能看，不需要登录；链接里的随机串无法猜到。
- 过期后打开链接显示"这个预览已过期"。过期 7 天后链接变为 404。
- 预览不出现在 App 列表里，不产生版本，也不能覆盖目录。
- `tinyhost previews`：列出自己还没过期的预览（链接、来源目录、过期时间）。
- `tinyhost previews rm <链接或 ID>`：提前删除自己的预览。
- 后台新增"预览"页：列出所有 Agent 的预览，可以删除。

**上传前的路径检查（deploy、data push、preview 都做）**
- 扫描 HTML、CSS、JS 文件，发现以 `/` 开头的引用（`src="/…"`、`href="/…"`、`url(/…)`、`fetch("/…")`、`import … from "/…"`）就给出警告。
- 警告不阻止上传，列出文件、行号、原文，并提示"改成相对路径，例如 ./app.js"。`//` 开头的协议相对地址和完整的 `https://` 地址不算。
- 例：
  ```
  ⚠ 发现 2 处以 / 开头的路径，在 tinyhost 上会加载失败：
    index.html:12  <script src="/assets/app.js">
    app.js:40      fetch("/data/sales.json")
    改成相对路径（./assets/app.js、data/sales.json）
  ✓ 预览已更新 …
  ```

**Skill**
- `tinyhost skill`：打印完整的使用说明（Markdown，带 skill 的 frontmatter，可以直接存成 Agent 的 skill 文件）。
- 同样的内容也在 `https://th.reorx.com/skill.md`，链接已经替换成这台服务器的地址。没装 CLI 的 Agent 也能先读。
- 说明里包括：
  - tinyhost 是什么；什么时候用 preview，什么时候用 deploy，什么时候用 data push。
  - 安装、授权：把链接原样发给用户，等用户批准；超时就再运行一次 login。
  - 云端环境用 `TINYHOST_TOKEN`。
  - App 目录规范：`index.html`、只用相对路径、单页应用用 hash 路由、数据放 JSON 文件、`tinyhost.json` 的用法。
  - 数据目录协作：先 `tinyhost info` 看 App 声明的数据目录和格式。
  - 新建要加 `--create`；不要随便往别人的 App 发布。
  - 发布后必须把版本链接和主入口原样告诉用户。
  - 不要把密钥、令牌、隐私数据放进公开 App。
  - curl 直接上传的写法。
  - 常见错误和对应处理。

**一行安装**
- `curl -fsSL https://th.reorx.com/install.sh | sh`：
  - 识别系统和架构（linux / macOS × amd64 / arm64），下载与服务器同版本的 CLI 并校验，装到 `~/.local/bin`（可以用 `TINYHOST_INSTALL_DIR` 改）。
  - 写好服务器地址，之后不需要再设置 `TINYHOST_SERVER`。
  - 最后提示下一步：`tinyhost skill`。
  - `~/.local/bin` 不在 PATH 里时，提示怎么加。
  - 重复运行就是升级。
- 下载只经过 `th.reorx.com` 这一个域名。
- CLI 发现自己比服务器旧时，在输出末尾提示重新运行安装命令升级。

**根路径说明页**
- `https://th.reorx.com/` 显示：这是一个 tinyhost 服务；Agent 请先运行安装命令，再读 `tinyhost skill`（或直接读 `/skill.md`）。

**通知（ntfy）**
- 默认推送三类事件：
  - **Agent 申请授权**：标题"<名称> 申请使用 tinyhost"，内容是验证码、主机名、IP，点击打开批准页。
  - **新建 App**：标题"新 App：<slug>"，内容是创建者和标题，点击打开主入口。
  - **App 包发布**：标题"<slug> v4 已发布"，内容是发布者和说明，点击打开主入口。
- 推数据、回滚、删除默认不推，可以通过配置打开。
- 也可以配置一个通用 webhook，收到同样的事件（JSON）。
- 通知发送失败不影响任何操作，只记日志。

### 不做的事

- 预览自动刷新、监听文件变化（watch）。
- 把预览"转正"成 App（直接对同一目录 `deploy --create` 即可）。
- Windows 的安装脚本、`tinyhost skill --install` 自动写入各种 Agent 的 skills 目录。
- Telegram 等其他通知渠道（webhook 可以自己接）。

### 验收标准

1. `preview` 一个目录得到链接，在没登录的浏览器里能打开，相对路径资源能加载。
2. 修改目录后再次 `preview`，链接不变，刷新后看到新内容；加 `--new` 得到不同的链接。
3. `--ttl 1h` 的预览一小时后显示"已过期"；`--ttl 30d` 报错。
4. Agent A 在 `previews` 里看不到 Agent B 的预览，也删不掉；后台能看到全部，也能删除。
5. 目录里有 `src="/app.js"` 时，deploy 和 preview 都给出带文件名和行号的警告，并且照常完成。
6. 在一个干净的 Linux 容器里运行安装命令，装好的 `tinyhost` 版本与服务器一致，不设任何环境变量就能 `login`。
7. `/skill.md` 与 `tinyhost skill` 的内容一致，里面的地址是这台服务器的地址。
8. Agent 申请授权后，我的手机在几秒内收到通知，点开就是批准页。
9. 发布 App 包、新建 App 时手机收到通知；推数据默认不收到。
10. ntfy 服务不可用时，发布照常成功。

## Tech Design

### 现状

M2 完成后：设备授权、后台、私有查看都已就绪。层和文件块、GC、`bundle.Pack/Unpack` 都来自 M1。`events` 记录了所有动作，但没有对外通知。CLI 的 `skill` 命令还是占位文本。

### 方案

- **预览**：一个预览 = 一个层 + 一个随机路径令牌，按 `(client_id, local_key)` 去重。`local_key` 由 CLI 为每个绝对路径生成并保存在本地，所以"同一个 Agent、同一个目录"会落到同一个预览上。
- **路径检查**：放在 CLI 侧的 `bundle.Pack` 里做，服务端不做。curl 用户不会收到警告，可以接受。
- **Skill 与安装脚本**：都是 embed 进二进制的模板，渲染时填入服务器地址和版本。
- **CLI 下载**：服务端 `/_/dl/` 代理 GitHub Release 并缓存到本地，校验 checksums。
- **通知**：服务端核心发出事件回调，`internal/notify` 订阅后异步发送；核心代码不 import notify。

### 数据变动

```sql
CREATE TABLE previews (
  id          TEXT PRIMARY KEY,               -- "p_" + 随机
  token       TEXT NOT NULL UNIQUE,           -- 20 位小写 base32（100 bit），即 URL 中的 /_p/<token>/
  client_id   TEXT NOT NULL,                  -- Owner 不创建预览
  local_key   TEXT NOT NULL,
  source      TEXT NOT NULL DEFAULT '',       -- CLI 上报的目录名（只用于显示）
  layer       TEXT REFERENCES layers(id),     -- 过期后由 GC 置 NULL
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL,
  UNIQUE (client_id, local_key)
);
```

- token 存明文：后台要显示链接，而且预览本身是短期的。DB 文件的权限是 0600。
- GC 增加：
  - 过期的预览把 `layer` 置 NULL，层随即变成孤儿，被回收。
  - 过期超过 7 天的预览删除整行。
  - M1 的"孤儿层"判断要把 `previews.layer` 算作引用。
- CLI 本地新增 `os.UserConfigDir()/tinyhost/previews.json`：`{ "<server>|<绝对路径>": "<local_key>" }`。
- `config.json`（安装脚本写入）：`{ "server": "https://th.reorx.com" }`。M1 定义的读取顺序里，"配置文件"这一档从这里读。

### 逻辑变动

**Agent API 新增**

| 方法与路径 | 说明 |
|---|---|
| `POST /_/api/v1/previews?key=&ttl=&new=1&source=` | body 为 tar.gz → `{id, url, expires_at, reused}`。`new=1` 时忽略 key，用随机 key 新建 |
| `GET /_/api/v1/previews` | 自己未过期的预览 |
| `DELETE /_/api/v1/previews/{id}` | 只能删自己的 |

- ttl 解析成 Go duration，范围 1h–168h，默认 24h。

**内容托管**
- `/_p/{token}/{rest...}`：
  - 找到并且未过期：按 M1 的路径解析（没有覆盖目录）返回内容。响应头 `Cache-Control: no-cache`、`X-Robots-Tag: noindex`，其余同 M1。
  - 过期且行还在：返回 410 和"已过期"页。
  - 不存在：404。
- 预览不检查可见性。

**后台**
- `GET /previews`：列出全部。
- `POST /previews/{id}/delete`。
- 首页导航加入口。

**`internal/bundle` 路径检查**
- `CheckAbsolutePaths(files) []Warning{File, Line, Text}`。
- 扫描扩展名为 `.html .htm .css .js .mjs`、大小不超过 1MB 的文件，逐行用以下正则匹配：
  - `(?:src|href|action|poster)\s*=\s*["']/[^/]`
  - `url\(\s*["']?/[^/]`
  - `(?:fetch|import)\s*\(\s*["'\x60]/[^/]`
  - `from\s+["']/[^/]`
- 每个文件最多报 5 条，总共最多报 20 条。
- `--json` 输出新增 `warnings` 字段（可选字段，符合冻结规则）。

**`skill/`**
- `skill/SKILL.md` 是模板，frontmatter 为 `name: tinyhost`、`description: …`；正文中的服务器地址写成 `{{SERVER}}`。
- `skill/skill.go` 用 `go:embed` 嵌入模板，并提供 `Render(server string)`。
- CLI 用已配置的服务器地址渲染；没有配置时用 `https://<your-tinyhost-server>`。
- 服务端用 `--apps-url` 渲染，提供 `GET /skill.md`（`text/markdown; charset=utf-8`）。

**安装脚本与下载代理**
- `skill/install.sh` 是模板（也 embed 进服务端），填入 `{{SERVER}}` 和 `{{VERSION}}`。逻辑：
  1. `uname -s` / `uname -m` 映射到 `linux|darwin` 和 `amd64|arm64`，其他组合报错退出。
  2. 用 curl（没有就用 wget）下载 `{{SERVER}}/_/dl/tinyhost_{{VERSION}}_<os>_<arch>.tar.gz` 和 `{{SERVER}}/_/dl/checksums.txt`。
  3. 用 `sha256sum` 或 `shasum -a 256` 校验。
  4. 解压，安装到 `${TINYHOST_INSTALL_DIR:-$HOME/.local/bin}`。
  5. 写 `config.json`（Linux 是 `$XDG_CONFIG_HOME/tinyhost` 或 `~/.config/tinyhost`；macOS 与 Go 的 `os.UserConfigDir` 一致，是 `~/Library/Application Support/tinyhost`）。
  6. 打印下一步。
- 服务端 `GET /install.sh` 返回渲染结果（`text/x-shellscript`）。
- `GET /_/dl/{file}`：
  - `file` 必须出现在本版本的 `checksums.txt` 里（`checksums.txt` 本身例外）。
  - 缓存目录是 `<data-dir>/dl/<version>/`，没有缓存时从 `https://github.com/<release-repo>/releases/download/v<version>/<file>` 下载，校验 sha256 后再存入缓存。
  - 新增参数：`--release-repo`（默认 `reorx/tinyhost`）；`--dl-dir`（本地目录，开发和测试时直接从这里读，例如 `make dist` 的输出）。
  - 版本是 `dev` 且没设 `--dl-dir` 时，`/install.sh` 返回一个只打印"此服务器是开发版本，不提供安装"的脚本。

**版本提示**
- Agent API 的所有响应带 `X-Tinyhost-Version` 头。
- CLI 比较自己和服务器的版本，比服务器旧时在 stderr 末尾提示升级命令；dev 版本不提示。

**`internal/notify`**
- `Hub` 带一个容量 100 的缓冲 channel，单个 goroutine 发送；满了就丢弃并记日志。
- 后端：
  - ntfy：POST 到主题 URL，设置 `Title`、`Click`、`Tags`、`Priority` 头；有 token 时加 `Authorization: Bearer`。
  - webhook：POST JSON `{event, title, message, url, at}`。
- 参数：`--ntfy-url`（如 `https://ntfy.reorx.com/tinyhost`）、`--ntfy-token`、`--webhook-url`、`--notify-events`（默认 `device.requested,app.created,app.deployed`）。
- 服务端核心在 `AddEvent` 之后调用注入的 `OnEvent(Event)` 回调，`cmd/tinyhost-server` 把它接到 Hub。
- 新增事件 `device.requested`，在 device/code 端点里产生。
- `app.deployed` 只针对 kind=app 的版本；数据版本对应 `data.pushed`。
- 消息里的链接：申请授权指向 `<admin>/device?code=…`；其余指向主入口。
- 发送超时 10 秒，不重试。

**CLI**
- 新增 `preview`、`previews`、`previews rm`。
- `skill` 输出渲染后的正文。
- deploy、data push、preview 在上传前打印路径警告（stderr）。

### 实施步骤

- [x] previews 表和 store 函数，GC 扩展，先写测试
- [x] 把验收标准 1–5、7–10 写成行为测试；6 写成手动验收脚本
- [x] 预览 API 和 `/_p/` 托管，后台预览页
- [x] `bundle.CheckAbsolutePaths` 及测试
- [x] `skill/SKILL.md` 正文、`skill/install.sh`、`/skill.md`、`/install.sh`、`/_/dl/` 代理（测试用 httptest 假的 GitHub）
- [x] `internal/notify`、事件回调接线，测试用 httptest 假的 ntfy
- [x] CLI：`preview`、`previews`、`skill`、版本提示，golden 快照
- [x] 根路径说明页
- [x] 更新 `docs/json-output.md`、`AGENTS.md`
- [ ] 手动验收：在一个 Linux 容器里跑安装脚本（容器跑在服务器或 CI 上，不在本机跑 docker），`--dl-dir` 指向 `make dist` 的产物，截图存到 `tmp/<日期>-m3-preview-distribution/`

### 测试与验证

| 验收 | 测试 |
|---|---|
| 1 | `TestPreviewIsPublicByUnguessableLink` |
| 2 | `TestPreviewReusesLinkPerClientAndKey`、`TestPreviewNewCreatesSeparateLink` |
| 3 | `TestExpiredPreviewShowsGoneThenNotFound`（注入时钟）、`TestPreviewTTLBounds` |
| 4 | `TestPreviewsAreScopedToClient`、`TestAdminCanDeleteAnyPreview` |
| 5 | `TestCheckAbsolutePaths`（表驱动，含 `//cdn`、`https://`、data URI 等不该报的情况）+ CLI golden |
| 6 | 手动：干净的 Linux 容器（amd64 和 arm64 各一次）+ 一台 macOS |
| 7 | `TestSkillServedMatchesCLIRender` |
| 8 | `TestDeviceRequestNotifiesWithApproveLink`（假 ntfy 断言请求头 `Click`） |
| 9 | `TestNotifyDefaultEvents` |
| 10 | `TestNotifyFailureDoesNotAffectDeploy` |

补充：`TestDownloadProxyOnlyServesListedFilesAndVerifiesChecksum`、`TestInstallScriptRendersServerAndVersion`。

### 风险与待定

- **路径检查的正则会误报或漏报**：例如 JS 里拼出来的路径查不到，模板字符串里的 `/` 可能误报。警告不阻止上传，误报的代价很小。
- **macOS 上 CLI 的配置目录**是 `~/Library/Application Support/tinyhost`，安装脚本必须和 `os.UserConfigDir` 保持一致，否则 CLI 读不到服务器地址。测试要覆盖 macOS。
- **预览 token 出现在访问日志里**（Caddy 和服务端都会记录 path）。日志只在服务器本机和自建的 VictoriaLogs 里，可以接受。
