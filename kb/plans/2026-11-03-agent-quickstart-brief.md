---
created: 2026-11-03
tags:
  - brief
  - admin-console
  - onboarding
---

# 执行 brief：后台 Agent 页 quickstart 指示

- 执行者：Claude（sonnet），运行在 herdr 新开的 tab 里。brief 由协调 agent（Claude Opus）撰写。
- 工作位置：`/Users/reorx/Code/tinyhost`，当前 `master` 分支，直接在这个 checkout 上改，不开分支或 worktree。
- 开工前要读：
  - `AGENTS.md`
  - `kb/plans/2026-11-03-agent-quickstart.md`：要做什么、怎么做，都以它为准。
  - `kb/sessions/2026-11-02-th-m2-console.md` 的「页面清单」。
  - `kb/sessions/2026-11-02-th-m3-dist.md` 的「验收 6 脚本」：本地用 `--dl-dir` 验证安装的做法。

## 目标

完成 plan 实施步骤的第 1–7 步：先写行为测试，再实现；然后改 README，做本地实机验收，写 session 总结，更新 AGENTS.md，最后提交一次。做完以后，本地后台的 `/agents` 能复制 quickstart 指示，在全新 HOME 里照着指示做，能装好 CLI 并完成授权。

## 完成标准

- `mise exec -- go test ./...`、`mise exec -- go vet ./...` 通过，`mise exec -- gofmt -l internal cmd skill` 没有输出。
- plan「测试与验证」表格里的每一行都有对应的 Go 测试，测试名写成行为句子，函数上方加 `// Behavior:` 注释。
- 本地实机验收第 1–6 步都做了，证据归档在 `tmp/2026-11-03-agent-quickstart/`：截图、复制出来的 quickstart 文本、在全新 HOME 里执行时的终端输出。
- plan 里第 1–7 步都勾选了。`kb/sessions/2026-11-03-th-agent-quickstart.md` 已写好，包含「页面清单增量」。AGENTS.md 已按 plan 第 7 步更新。
- 一个提交，包含代码、测试、README、plan 里勾选的步骤、session 总结和 AGENTS.md。plan 和本 brief 已经由协调者先提交了。提交信息用中文，照仓库现有的 `feat: …` 风格。

## 已经定下的决定

| 问题 | 决定 | 谁定的 |
|---|---|---|
| 放在哪里 | 后台 Agent 页 | 用户 |
| 文字语言 | 英文 | 用户 |
| Agent 名称 | 可选输入框，实时写进文字；留空时让 Agent 自己起名 | 用户 |
| 面板位置和形态 | 右栏、「手动签发 token」上面；全文默认收进「查看全文」 | 协调者（用户看过概要，没有反对） |
| 带 token 的 quickstart | 不做 | 协调者（用户看过概要，没有反对） |
| 文字在哪里生成 | Go 的 `skill.Quickstart`；JS 只做占位符替换 | 协调者 |
| 名称字符规则 | `^[A-Za-z0-9._@-]{1,64}$`，只在 JS 里校验 | 协调者 |

协调者定的几项，如果实现时发现有更好的做法，可以改，但要在 session 总结里写明改了什么、为什么改。产品行为（PRD）不要自己改：觉得 PRD 有问题时，停下来在 tab 里说明，等用户决定。

## 不在范围内

- plan 第 8 步：发版、push、打 tag、部署。做完第 7 步就停下汇报，由协调者去问用户。
- 改 apps 根路径页、`SKILL.md` 的内容、README 截图。
- 范围外发现的问题：如果符合 `kb/known-issues.md` 的记录标准（会影响使用或稳定，并且还有事要做），就记进去，这个文件还不存在，需要新建；其余的写进 session 总结的「遗留问题」。不要顺手修范围外的代码。

## 工作方式

- BDD：每一组行为先写测试，跑一次确认是红的，再实现。
- Go 命令一律加 `mise exec --` 前缀。改了 `internal/server/views/` 之后，要重新 build `tinyhost-server` 再做浏览器验证，因为模板和静态文件是 embed 进二进制的。
- 本地服务用 `127.0.0.1:3471`。数据目录、dist 输出、验收用的 HOME 都放在 `tmp/` 下，不碰真实的 `~/Library/Application Support/tinyhost`，原因是那里存着用户连线上服务器用的凭证。执行全新 HOME 的命令时，照 `scripts/accept-install.sh` 用 `env -i HOME=… PATH=…`，避免继承 `TINYHOST_SERVER`、`TINYHOST_TOKEN`。
- 在临时数据目录里设置后台密码，用 `tinyhost-server passwd --data-dir …`。密码随便设，不要写进提交。
- 浏览器验收用 agent-browser。做完以后关掉自己的浏览器会话、停掉本地服务，执行 `mac-dev-cleanup --only browser --min-age 2`。加 `--min-age 2` 是因为别的会话可能还在用浏览器。
- 不在本机跑 docker 命令。

## 约束

- 只 `git add` 自己改过的文件，不要 `git add -A`：同一个 checkout 里可能有别的未提交改动。
- 不 push，不打 tag，不改 deploy 仓库。
- 仓库是公开的：提交、kb 文档和截图里不能出现线上服务器的 IP、token、密码。

## 完成后

在 tab 里给出最终汇报：

- 提交 hash
- 测试结果
- 证据目录 `tmp/2026-11-03-agent-quickstart/`
- session 总结的路径
- 自己做的、偏离 plan 的决定
- 遗留问题
