---
created: 2026-11-03
tags:
  - admin-console
  - onboarding
  - agents
  - skill
---

# 后台 Agent 页提供可复制的 quickstart 指示

## PRD

### 背景

Owner 想让一个新环境里的 Agent（Claude Code、Codex 等）接入 tinyhost 时，现在只能参考 README 里的三行命令：

```sh
curl -fsSL --retry 3 --retry-all-errors https://apps.example.com/install.sh | sh
tinyhost skill
tinyhost login --name "report-agent" --wait 120
```

这段命令要手动替换成自己实例的域名，也没告诉 Agent 拿到批准链接后该怎么做。apps 根路径页上虽然有真实域名的安装命令，但那是给 Agent 自己读的，Owner 想要的是一段可以直接粘贴进 Agent 对话的指示。

### 目标

Owner 打开后台 Agent 页（线上是 `th.reorx.com/agents`），点一下「复制」，把复制的文字粘贴给新环境里的 Agent。Agent 照着做就能装好 CLI、读完使用说明、发起授权，并把批准链接和验证码发回给 Owner。

### 功能与交互

**入口**：后台 Agent 页右栏，新增一个「接入新 Agent」面板，放在现有「手动签发 token」面板上面。

**面板内容**：

- 一句说明：复制下面的指示，粘贴给新环境里的 Agent；它会安装 CLI、读使用说明，然后把批准链接发给你，打开链接批准即可。
- 「名称（可选）」输入框。占位文字提示：留空让 Agent 自己起名。
- 主按钮「复制 quickstart 指示」，点击后按钮文字短暂变成「已复制」（和后台其他复制按钮一致）。
- 可展开的「查看全文」，默认收起，展开后显示将要复制的完整文字，可以手动选中。

**指示文字**（英文，和 skill、README 一致）。服务器地址填的是这个实例真实的 apps 地址。内容依次是：

1. 一句话说明目的：配置 tinyhost，以便把网页发布到这个服务器。
2. 安装 CLI（macOS/Linux）：完整的安装命令；curl 不认识 `--retry-all-errors` 时去掉它重跑；安装程序提示改 PATH 时照做。
3. 运行 `tinyhost skill` 并完整阅读，它是完整的使用说明；Agent 支持 skill 的话，把输出存成名为 tinyhost 的 skill。
4. 运行 `tinyhost login --name "<名称>" --wait 120`，把输出的批准链接和验证码原样发给用户，然后等待；如果用户批准前命令就退出了，再运行 `tinyhost login` 继续等同一个申请。
5. 用户批准后，运行 `tinyhost whoami` 确认。

样例（填了名称 report-agent 时）：

```text
Set up tinyhost so you can publish web pages to https://tinyhost.space.

1. Install the CLI (macOS/Linux):
   curl -fsSL --retry 3 --retry-all-errors https://tinyhost.space/install.sh | sh
   If curl rejects --retry-all-errors, drop that option and rerun.
   Follow any PATH instruction the installer prints.
2. Run `tinyhost skill` and read all of it; it is the full usage guide.
   If you support skills, save its output as a skill named tinyhost.
3. Run: tinyhost login --name "report-agent" --wait 120
   Send me the approval link and verification code exactly as printed,
   then wait. If it exits before I approve, run `tinyhost login` again
   to keep waiting on the same request.
4. After I approve, run `tinyhost whoami` to confirm.
```

具体措辞可以在实现时微调，但必须覆盖上面五点。

**名称**：

- 输入框留空：第 3 步让 Agent 自己起一个能说明身份的短名（例如工具加机器名），再用它运行 login。
- 填了名称：第 3 步直接写进 `--name "<名称>"`。输入时，复制的内容和「查看全文」里的文字实时跟着变，不用刷新页面。
- 名称只允许字母、数字和 `.` `_` `@` `-`，最长 64 个字符。输入了不允许的字符时，在输入框下面提示允许的字符，复制的内容退回到「Agent 自己起名」的版本。

**边界情况**：

- 浏览器不支持剪贴板，或者脚本没加载：复制按钮和名称输入框不显示（和后台现有复制按钮的规则一致），「查看全文」仍可展开、手动选中复制。
- 这台服务器是开发版本，又没有配置本地安装包：面板里显示一条提示，说明安装命令在这台服务器上会失败。文字照常显示。
- 刚手动签发完 token、页面显示一次性 token 时，「接入新 Agent」面板照常显示。
- Agent 列表为空时，空状态提示改为引导用户用右边的「接入新 Agent」复制指示，或手动签发 token。
- 窄屏下右栏排到列表下面，两个面板依次排列。

**README**：「Use with an Agent」一节补充：实例的 Owner 可以在后台 Agents 页复制一段现成的 quickstart 指示，直接粘贴给 Agent。

### 不做的事

- Windows 的安装指示。
- 改 apps 根路径页（它是给 Agent 自己读的，保持现状）。
- 签发 token 后生成带 token 的 quickstart：token 会留在 Agent 的聊天记录里，违背「只显示一次、配进环境变量」的设计。
- 记住上次填的名称。
- 修改 skill 的内容。

### 验收标准

- 打开后台 Agent 页，右栏顶部有「接入新 Agent」面板，「手动签发 token」面板仍在它下面，功能不变。
- 展开「查看全文」，文字里的安装命令是这个实例真实的 apps 地址，不含 `example.com` 或任何占位符。
- 不填名称点击复制，剪贴板里是让 Agent 自己起名的版本；填 `qs-accept` 后复制，第 3 步变成 `tinyhost login --name "qs-accept" --wait 120`，「查看全文」同步变化。
- 输入带空格或引号的名称时出现字符提示，复制的是自己起名的版本。
- 在一个全新的环境里照复制的文字执行：CLI 安装成功，`tinyhost skill` 有输出，login 给出批准链接和验证码；Owner 打开链接批准后，再运行 login 或 whoami 显示已登录，名称和填写的一致。
- 签发 token 后的页面上，一次性 token 和「接入新 Agent」面板同时可见。
- 开发版本且没有本地安装包的服务器上，面板显示安装不可用的提示。
- README 写明可以从后台 Agents 页复制 quickstart。
- 上线后，在 `th.reorx.com/agents` 复制，粘贴给一个新环境里的真实 Agent，能走完安装、授权到 whoami。

## Tech Design

### 现状

- `skill/skill.go`：`InstallCommand(server)` 是安装命令的唯一来源，apps 根路径页（`internal/server/distribution.go` 的 `RootView.InstallCommand`）用它，`internal/server/distribution_test.go` 的 `TestRootInstallCommandMatchesSkillAndRetries` 保证和 `SKILL.md` 里的字面量一致。`RenderInstall(server, build, local)` 在 `build == "dev" && !local` 时返回拒绝安装的脚本；server 传入 `version.Version` 和 `s.Config.DLDir != ""`。`SKILL.md`、`install.sh` 用 `//go:embed` 嵌入，`{{SERVER}}` 等占位符用 `strings.NewReplacer` 替换。
- `internal/server/console.go`：`AgentsView{Agents []AgentRow; Token, IssuedName string; ExpiresAt *int64}`。`consoleAgents`（GET `/agents`）和 `consoleIssueToken`（POST `/agents`）都渲染 `agents.html`，后者额外带一次性 token。路由在 `internal/server/admin.go` 的 `adminHandler`（`GET /agents`、`POST /agents`、`POST /agents/{id}/revoke`）。
- `internal/server/views/agents.html`：`.split` 两栏，主栏是 Agent 表格（含空状态文案），右栏是 `<aside class="panel">` 手动签发表单（字段 `name`、`ttl`）。
- `internal/server/views/static/admin.css`：`.split > aside { position: sticky; top: 80px; }`（第 179 行），窄屏 `.split` 变单列、aside 取消 sticky（第 548–549 行）；`.panel`、`.hint`、`.field`、`.input`、`.secret`、`.cmd` 等现成样式。
- `internal/server/views/static/admin.js`：`[data-copy]` 按钮只在有 `navigator.clipboard` 时显示，点击时读取当时的 `data-copy` 属性写入剪贴板，按钮文字变「已复制」1.5 秒。CSP 不允许 `style` 属性、`<style>` 和内联 `<script>`。
- CLI `login`（`internal/cli/auth.go`）：`--name`、`--wait`（默认 60）、`--force`；等待超时退出码 4，再运行继续同一申请。服务端 `internal/server/device.go:84` 要求名称非空、最多 256 字节。
- 测试里 `version.Version` 是 `"dev"`，`setup()`（`internal/server/server_test.go`）的 Config 是 `AppsURL: "http://apps.test"`、`AdminURL: "http://admin.test"`，没设 `DLDir`。

### 方案

指示文字只在 Go 里生成：在 `skill` 包新增 `Quickstart(server, name string) string`，模板嵌入为 `skill/quickstart.txt`，安装行复用 `InstallCommand`。后台 Agent 页在服务端渲染两个版本：不带名称的版本（默认显示、默认复制）和带名称占位符的版本（放在 data 属性里）。admin.js 只负责把用户输入的名称替换进占位符，再同步更新显示和复制按钮的 `data-copy`。

关键决策：

- **文字在 Go 里生成，JS 只做占位符替换**。这样只有一处文案、能用 Go 测试覆盖，安装命令和 skill、根路径页一定一致。否决：在 JS 里拼文字（文案出现两份，测不到）；用 GET 表单 `?name=` 让服务端重新渲染（要刷新，不满足实时更新）。
- **占位符是导出常量 `skill.QuickstartNamePlaceholder = "__AGENT_NAME__"`**，服务端通过 data 属性把它传给 JS，不在 JS 里写死。测试保证「带占位符的版本把占位符替换成 x」等于 `Quickstart(server, "x")`，JS 替换的结果就和服务端生成的一致。
- **名称字符白名单 `^[A-Za-z0-9._@-]{1,64}$`，只在 JS 里校验**。名称会放进 shell 命令的双引号里，限制字符就不用处理 `$`、反引号、引号、反斜杠的转义；`@` 是为了兼容 CLI 默认名 `user@host` 的写法。`Quickstart` 本身不校验：服务端只用空名和占位符调用它。
- **安装是否可用抽成 `skill.InstallAvailable(build string, local bool) bool`**，`RenderInstall` 和后台页共用，避免两处各写一遍 `build == "dev" && !local`。
- **面板放右栏，全文默认收进 `<details>`**。右栏只有 300px 宽，而且是 sticky 的，全文展开会很高；默认收起，右栏高度和现在接近。展开后右栏如果超过视口，会导致底部看不到，所以这一页的 aside 不再 sticky（见逻辑变动）。

### 数据变动

无数据库或配置变动。`AgentsView` 只增加字段（view model 字段只增不改）：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Quickstart` | `string` | `skill.Quickstart(AppsURL, "")`，Agent 自己起名的版本 |
| `QuickstartNamed` | `string` | `skill.Quickstart(AppsURL, skill.QuickstartNamePlaceholder)` |
| `QuickstartPlaceholder` | `string` | `skill.QuickstartNamePlaceholder` |
| `InstallUnavailable` | `bool` | `!skill.InstallAvailable(version.Version, s.Config.DLDir != "")` |

### 逻辑变动

**`skill/quickstart.txt`（新增）+ `skill/skill.go`**

- `//go:embed quickstart.txt`，模板里放 `{{SERVER}}`、`{{INSTALL}}`、`{{LOGIN}}` 这类占位符，用 `strings.NewReplacer` 替换。英文措辞按 PRD 的样例。
- `const QuickstartNamePlaceholder = "__AGENT_NAME__"`。
- `func Quickstart(server, name string) string`：server 去掉末尾 `/`；安装行用 `InstallCommand(server)`。`name == ""` 时第 3 步让 Agent 先选一个能说明身份的短名（例如工具加机器名，如 `codex-laptop`），命令写成 `tinyhost login --name "<your-name>" --wait 120`；否则写 `tinyhost login --name "<name>" --wait 120`。两个版本的第 3 步其余文字（发回链接和验证码、退出后重跑 `tinyhost login`）相同。
- `func InstallAvailable(build string, local bool) bool { return build != "dev" || local }`，`RenderInstall` 改用它，行为不变。

**`internal/server/console.go`**

- `AgentsView` 加上表中四个字段。
- 新增 `func (s *Server) agentsView(rows []AgentRow) AgentsView`，填好 `Agents` 和 quickstart 相关字段；`consoleAgents` 和 `consoleIssueToken` 都用它，后者再设置 `Token`、`IssuedName`、`ExpiresAt`。需要 import `skill` 和 `version`（distribution.go 已经这样 import 过，没有循环依赖）。

**`internal/server/views/agents.html`**

- 右栏改成包住两个面板的容器，例如 `<div class="aside-stack">`（或保持 `<aside>`，内部放两个 `<section class="panel">`）。手动签发面板的标题、表单 action、字段名、文案不变。
- 新面板结构（类名、data 属性名供参考，可调整）：
  - `<section class="panel" aria-labelledby="quickstart-title">`，内含 `h2`「接入新 Agent」和 `.hint` 说明。
  - `{{if .InstallUnavailable}}`：一条提示，说明这台服务器是开发版本、没配置 `--dl-dir`，安装命令会失败。
  - 名称字段：`<div class="field" data-quickstart-name hidden>`，内含 `label` + `<input class="input" id="quickstart-name" maxlength="64" autocomplete="off" placeholder="留空让 Agent 自己起名">` + 默认隐藏的字符提示 `<p class="hint" data-quickstart-invalid hidden>`。**不要放进 `<form>`**，避免回车提交。
  - 复制按钮：`<button type="button" class="btn btn-primary" data-copy="{{.Quickstart}}" data-quickstart-copy hidden>复制 quickstart 指示</button>`，沿用现有 `data-copy` 逻辑。
  - `<details><summary>查看全文</summary><pre class="quickstart" data-quickstart-text data-default="{{.Quickstart}}" data-named="{{.QuickstartNamed}}" data-placeholder="{{.QuickstartPlaceholder}}">{{.Quickstart}}</pre></details>`。
- 空状态第二行改为：用右边的「接入新 Agent」复制指示给 Agent，或者手动签发 token。

**`internal/server/views/static/admin.js`**

新增一段：找到 `[data-quickstart-text]`，且 `navigator.clipboard` 可用时，显示名称字段，监听输入框的 `input` 事件。名称 trim 后为空，就用 `data-default`；符合 `^[A-Za-z0-9._@-]{1,64}$`，就用 `data-named` 替换占位符（用 `split(placeholder).join(name)`）；不符合，就显示字符提示并用 `data-default`。每次更新时，同步改 `<pre>` 的 `textContent` 和复制按钮的 `data-copy` 属性。现有复制逻辑不改。

**`internal/server/views/static/admin.css`**

- `.quickstart`：等宽字体、小字号、`white-space: pre-wrap`、`overflow-wrap: anywhere`、`max-height` 加 `overflow: auto`，背景和边框沿用 `.secret` / `.cmd` 的令牌，深浅色都要看。
- 右栏两个面板之间留间距，例如 `.aside-stack { display: grid; gap: 16px; }`。
- 这一页右栏取消 sticky：如果保留 `<aside>` 并给它加修饰类，就写 `.split > aside.aside-stack { position: static; }`；如果改成 `<div>`，`.split > aside` 规则自然不再生效。
- `details > summary` 的样式要和后台风格一致（光标、颜色、间距）。

**`README.md`**

在「Use with an Agent」第一段（「Open its apps address and copy the installation command, or ask its owner for that command.」）补一句，说明 Owner 可以在后台 **Agents** 页复制一段现成的 quickstart 指示直接粘贴给 Agent。其余步骤不变。README 截图（admin、approval、dashboard）都不涉及 Agent 页，不用更新。

### 实施步骤

- [ ] 1. 写 `skill` 包的行为测试（先红）：`Quickstart` 的内容和名称两种版本、占位符替换的一致性、`InstallAvailable`。
- [ ] 2. 实现 `skill/quickstart.txt`、`Quickstart`、`QuickstartNamePlaceholder`、`InstallAvailable`，`RenderInstall` 改用 `InstallAvailable`；`mise exec -- go test ./skill` 通过。
- [ ] 3. 写 `internal/server` 的行为测试（先红）：GET `/agents` 和签发 token 后的页面都有 quickstart 面板，以及开发版本提示的显示条件。
- [ ] 4. 实现 `AgentsView` 字段、`agentsView` helper、`agents.html`、`admin.css`、`admin.js`；`mise exec -- go test ./...`、`mise exec -- go vet ./...`、`gofmt -l` 都通过。
- [ ] 5. 改 README。
- [ ] 6. 本地实机验收（见下一节），截图归档到 `tmp/2026-11-03-agent-quickstart/`。
- [ ] 7. 写 session 总结 `kb/sessions/2026-11-03-th-agent-quickstart.md`，内容包括「页面清单增量」：`AgentsView` 新字段、新的 data 属性和名称规则。更新 AGENTS.md：`internal/server/console.go` 条目的页面清单指针里加上这份 session；`skill/` 条目补一句 `Quickstart` / `InstallAvailable`；`admin.js` 的职责列表补上 quickstart 名称替换。连同代码一起提交，一个提交。
- [ ] 8. **停下来问用户**是否发版 `v0.1.1` 并部署。同意后：push tag，等 Release 和 ghcr 镜像完成，再按 deploy 仓库 `kb/docs/tinyhost.md` 的 SOP 部署（部署执行者在 deploy 目录里工作）；最后核对线上 `X-Tinyhost-Version`，打开 `https://th.reorx.com/agents` 确认面板里是 `https://tinyhost.space`。线上用真实 Agent 走一遍由用户做，或者由用户指定执行方式。

### 测试与验证

**Go 行为测试**（测试名写成行为句子，函数上方加 `// Behavior:` 注释）：

| 验收标准 | 测试 |
|---|---|
| 文字含真实地址、无占位符 | `skill`：`Quickstart("https://apps.example.com/", "")` 包含 `InstallCommand("https://apps.example.com")`、`tinyhost skill`、`tinyhost login --name`、`--wait 120`、`tinyhost whoami`，不含 `{{`、`__AGENT_NAME__`、`.com//` |
| 填名称后第 3 步变化 | `skill`：`Quickstart(s, "report-agent")` 包含 `tinyhost login --name "report-agent" --wait 120`，不含让 Agent 自己起名的那句；空名版本包含那句 |
| JS 替换结果和服务端一致 | `skill`：`strings.ReplaceAll(Quickstart(s, QuickstartNamePlaceholder), QuickstartNamePlaceholder, "qs-accept") == Quickstart(s, "qs-accept")`，并且占位符在带名称的版本里只出现一次 |
| 开发版本提示 | `skill`：`InstallAvailable("dev", false)` 为 false，`("dev", true)` 和 `("0.1.1", false)` 为 true；现有 `RenderInstall` 测试保持通过 |
| Agent 页有面板 | `internal/server`：owner 会话 GET `/agents`，body 包含 `html.EscapeString(skill.Quickstart("http://apps.test", ""))`（出现在 `data-copy` 和 `<pre>` 里）、转义后的 `QuickstartNamed`、`data-placeholder`、`http://apps.test/install.sh`；不含 `style=`、`<style` |
| 签发 token 后面板仍在 | `internal/server`：POST `/agents` 签发后 body 同时包含明文 token 和 quickstart 文本（可以扩展 `TestConsoleManualTokenAndAgentStates`，或单独写一个） |
| 开发版本提示条件 | `internal/server`：默认 setup（dev、无 DLDir）页面有提示；设置 `f.s.Config.DLDir` 后没有提示 |

**本地实机验收**（用 agent-browser；改了 `views/` 要重新 build `tinyhost-server`）：

1. `mise exec -- make dist VERSION=0.1.1-qs`，用同一个 VERSION build `tinyhost-server`（`go build -ldflags "-X github.com/reorx/tinyhost/internal/version.Version=0.1.1-qs" ./cmd/tinyhost-server`，或者用 `make build` 的 LDFLAGS），在临时数据目录里设好密码后启动：`serve --data-dir tmp/qs-data --listen 127.0.0.1:3471 --apps-url http://localhost:3471 --admin-url http://admin.localhost:3471 --insecure-cookies --dl-dir <dist 绝对路径>`。
2. 浏览器登录 `admin.localhost:3471`，打开 `/agents`：截图面板（收起、展开全文；桌面和窄屏；深浅色）。
3. 不填名称点击复制；填 `qs-accept` 后再复制。读取剪贴板（`navigator.clipboard.readText()`，读不到就读按钮的 `data-copy` 属性），存成 `tmp/2026-11-03-agent-quickstart/quickstart-*.txt`，核对第 3 步和展开的全文一致。再输入 `bad name"` 截图字符提示。
4. 在全新 HOME 里照 `quickstart-named.txt` 的命令执行，参考 `scripts/accept-install.sh` 的 `env -i HOME=... PATH=...` 写法，脚本放在 `tmp/`：运行安装行 → `tinyhost skill` 有输出 → `tinyhost login --name "qs-accept" --wait 1` 退出码 4，输出里有批准链接和验证码 → 在浏览器打开链接批准（截图）→ 再运行 `tinyhost login` 显示已登录 → `tinyhost whoami` 显示名称 `qs-accept`。
5. 签发一个 token，截图一次性 token 和面板同屏。
6. 用不带 `--dl-dir` 的 dev 构建启动一次，截图开发版本提示。
7. 验收结束后停掉服务、删除临时数据目录，执行 `mac-dev-cleanup --only browser`。

### 风险与待定

- `navigator.clipboard` 需要安全上下文：线上是 HTTPS，本地 `admin.localhost` 浏览器也视为安全上下文。验收时如果读不到剪贴板，用 `data-copy` 属性核对内容。
- 名称白名单比服务端（任意字符，最多 256 字节）严格：Owner 想用中文名时只能留空，或者批准时在后台改名（批准页可以改名）。这是有意的取舍，先这样。
- 发版是对外操作（tag 推送后不能移动或删除），第 8 步必须先得到用户同意。
