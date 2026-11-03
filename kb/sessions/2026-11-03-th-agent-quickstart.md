---
created: 2026-11-03
tags:
  - tinyhost
  - admin-console
  - onboarding
  - agents
---

## 摘要（th-agent-quickstart）

- 做了什么：后台 Agent 页新增「接入新 Agent」面板，Owner 复制一段英文指示给新环境里的 Agent，Agent 照着装 CLI、读 skill、发起授权、发回批准链接。计划见 `kb/plans/2026-11-03-agent-quickstart.md` 第 1–7 步；第 8 步（发版、部署）没做，等用户决定。
- 文案只在 Go 里生成：`skill.Quickstart(server, name)`，模板 `skill/quickstart.txt`；`skill.QuickstartNamePlaceholder`、`skill.InstallAvailable(build, local)` 同在 `skill/skill.go`。`RenderInstall` 改用 `InstallAvailable`，行为不变。
- 先写测试再实现：`skill/quickstart_test.go` 4 个，`internal/server/console_test.go` 3 个（面板、签发后仍在、开发版提示条件），各自先红后绿。
- 本地：`go test ./...`（干净 HOME）、`go vet ./...` 通过，`gofmt -l internal cmd skill` 无输出。
- 证据：`tmp/2026-11-03-agent-quickstart/`（截图、`quickstart-default.txt`、`quickstart-named.txt`、全新 HOME 的终端输出、验收脚本）。

## 页面清单增量

接 `kb/sessions/2026-11-02-th-m2-console.md` 的「页面清单」，只有 `GET /agents` 和 `POST /agents` 的结果页变了；路由、表单字段不变。

`AgentsView` 新增字段（只增不改）：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Quickstart` | `string` | `skill.Quickstart(AppsURL, "")`，Agent 自己起名的版本 |
| `QuickstartNamed` | `string` | `skill.Quickstart(AppsURL, skill.QuickstartNamePlaceholder)` |
| `QuickstartPlaceholder` | `string` | `skill.QuickstartNamePlaceholder`（`__AGENT_NAME__`） |
| `InstallUnavailable` | `bool` | `!skill.InstallAvailable(version.Version, DLDir != "")` |

`s.agentsView(rows)` 生成这些字段，`consoleAgents` 和 `consoleIssueToken` 共用。

`agents.html` 右栏改成 `<aside class="aside-stack">`，里面两个 `<section class="panel">`：「接入新 Agent」在上，「手动签发 token」在下（标题、表单 action、字段名不变）。新面板的钩子：

- `data-quickstart-name`：名称字段容器，默认 `hidden`，有 `navigator.clipboard` 才显示；输入框 `#quickstart-name` 不在 `<form>` 里。
- `data-quickstart-invalid`：字符提示，默认 `hidden`。
- `data-quickstart-copy`：复制按钮，同时带 `data-copy`（沿用通用复制逻辑，默认 `hidden`）。
- `data-quickstart-text`：`<pre>`，带 `data-default`、`data-named`、`data-placeholder`。
- `data-install-unavailable`：开发版又没有 `--dl-dir` 时显示的提示段落。
- `details.quickstart-full`：「查看全文」，默认收起。

名称规则：`^[A-Za-z0-9._@-]{1,64}$`，只在 `admin.js` 里校验；空（trim 后）或不合规都回到 `data-default`，合规时用 `data-named.split(placeholder).join(name)`，同步改 `<pre>` 的 `textContent` 和按钮的 `data-copy`。没有 JS 或剪贴板时，输入框和按钮不显示，全文仍可展开手选。

空状态文案改为：用右边「接入新 Agent」复制指示，或手动签发 token。

## 验收

按 plan 的本地实机验收 1–6 做完，`make dist VERSION=0.1.1-qs`，服务端用同版本构建，数据目录和 HOME 都在 `tmp/`：

1. 桌面、窄屏、深浅色截图：面板、展开全文、填名称、非法名称（`bad name"` 出现字符提示，复制内容退回自己起名版本）。
2. 剪贴板：agent-browser 读不到（`readText` 权限被拒），按 plan 用按钮的 `data-copy` 属性核对，和展开的全文逐字节一致（`quickstart-named.txt` 与 `quickstart-named-shown.txt` `cmp` 相同）。
3. 全新 HOME（`env -i`）照 `quickstart-named.txt` 执行：安装行成功；`tinyhost skill` 有输出；`login --name "qs-accept" --wait 1` 退出码 4，输出批准链接和验证码；浏览器批准后 `tinyhost login` 显示「已授权为 qs-accept」，`whoami` 显示 `qs-accept`。
4. 签发 token 后，一次性 token 和面板同屏。
5. 不带 `--dl-dir` 的 dev 构建：面板显示开发版提示。带了 `--dl-dir` 的 `0.1.1-qs` 构建没有提示。

验收后已停服务、删数据目录、关浏览器会话并跑过 `mac-dev-cleanup --only browser --min-age 2`。

## 发版与上线

- 用户授权做完直接发版。README 理顺 quickstart 那句的语序后（`3eae5cf`），master CI 绿；`v0.1.1` 附注 tag 的 [Release 流水线](https://github.com/reorx/tinyhost/actions/runs/41585960264)一次绿，正式版 10 个资产，ghcr 镜像 `0.1.1` 有 amd64/arm64。
- 部署由 deploy 目录的执行 agent 完成（deploy 仓库 `c97137d`）：升级前手动备份成功，App/版本数、密码哈希、ntfy key 前后一致；两个域名 `X-Tinyhost-Version: 0.1.1`，容器 healthy，线上 `admin.js` 已带 quickstart 逻辑；[accept-install](https://github.com/reorx/tinyhost/actions/runs/41588054889) 三平台绿，产生的 3 条授权申请未批准，会自动过期。
- 线上 Agent 页需要 owner 登录，执行 agent 没有登录；在 `th.reorx.com/agents` 真点复制、粘贴给真实 Agent 的验收留给用户。

## 自己做的决定

- 无偏离 plan 的产品行为。小改动：「查看全文」里在第 3 步加了一句让 Agent 自己起名的话，放在 `Run:` 前同一行（`First choose a short name that says who you are …`）；PRD 样例里没有这句，plan 的逻辑变动一节有要求。
- 测试里用 `html.EscapeString` 对照模板输出，通过了，plan 没有预料到的反引号转义问题没有出现。
- 警告段落的颜色用已有的 `--warn` 令牌。

## 遗留问题

- `internal/cli` 的 `TestLocalCommandsWorkWithoutCredentials` 读本机真实的 CLI 配置（`os.UserConfigDir()/tinyhost/config.json`）：机器上装过 CLI 并连了服务器时，`tinyhost skill` 里的服务器地址不再是占位符，和 golden 不一致而失败。这与本任务无关（stash 掉我的改动同样失败）。用干净 `HOME` 跑 `go test ./...` 就是绿的。修法是测试里设置 `HOME`/`XDG_CONFIG_HOME` 到临时目录。按 brief 没有顺手改。它只影响开发机上跑测试，不满足 `kb/known-issues.md` 的记录标准，所以没有新建那个文件。
- 窄屏下右栏排到列表下面，空状态文案里的「右边」不准确（plan 里的措辞）；影响很小，没改。
- 浏览器验收读不到剪贴板；真正的复制按钮点击（写剪贴板、按钮变「已复制」）靠通用 `data-copy` 逻辑，没有单独验证。上线后用真实浏览器点一次最稳。
