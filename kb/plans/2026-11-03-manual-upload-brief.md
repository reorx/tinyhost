---
created: 2026-11-03
tags:
  - brief
  - admin-console
  - upload
---

# 执行 brief：后台 App 详情页手动上传新版本

- 执行者：Claude（sonnet），运行在 herdr 新开的 tab 里。brief 由协调 agent（Claude Opus）撰写。
- 工作位置：`/Users/reorx/Code/tinyhost`，当前 `master` 分支，直接在这个 checkout 上改，不开分支或 worktree。
- 开工前要读：
  - `AGENTS.md`
  - `kb/plans/2026-11-03-manual-upload.md`：要做什么、怎么做，都以它为准。
  - `kb/sessions/2026-11-02-th-m2-console.md` 的「页面清单」：后台路由、表单和 view model 的约定。
  - `internal/server/api.go`（`deploy` / `unpack`）、`internal/bundle/bundle.go`、`internal/server/console.go`、`internal/server/views/app.html`、`internal/server/views/static/admin.js`。

## 目标

完成 plan 实施步骤的第 1–7 步：
- 先写行为测试，再实现。
- 做本地实机验收，改 README。
- 写 session 总结，更新 AGENTS.md。
- 最后提交一次。

做完以后，本地后台的 App 详情页能把文件夹、zip、单个 html 上传成新版本，上传后页面顶部给出可点击的版本链接。

## 完成标准

- `mise exec -- go test ./...`、`mise exec -- go vet ./...` 通过；`mise exec -- gofmt -l internal cmd skill` 没有输出。
- plan「测试与验证」表格里的每一行都有对应的 Go 测试。测试名写成行为句子，函数上方加 `// Behavior:` 注释。现有的 Agent 测试和 CLI golden 不改动就能通过。
- 本地实机验收第 1–8 步都做了，证据归档在 `tmp/2026-11-03-manual-upload/`：截图、样例的生成命令、curl 输出。做不到的步骤（比如 agent-browser 给不了目录）写明原因和替代做法。
- plan 里第 1–7 步都已勾选。
- `kb/sessions/2026-11-03-th-manual-upload.md` 已写好，包含「页面清单增量」，内容有：上传接口的请求和响应、错误码、`AppView` 的新字段、`data-*` 钩子。
- AGENTS.md 已按 plan 第 7 步更新，每处一两句。
- 一个提交，包含代码、测试、README、plan 里勾选的步骤、session 总结和 AGENTS.md。plan 和本 brief 已经由协调者先提交了。提交信息用中文，照仓库现有的 `feat: …` 风格。

## 已经定下的决定

| 问题 | 决定 | 谁定的 |
|---|---|---|
| 支持哪些输入 | 文件夹、`.zip`、单个 `.html`、多个散文件；不支持 `.tar.gz` / rar / 7z | 用户确认 |
| 拖入后 | 立即上传，不二次确认；只认拖放框里的拖入 | 用户确认 |
| 「设为主入口」 | 默认勾上 | 用户确认 |
| 成功后 | 页面刷新，顶部横幅给出版本链接（新标签页打开）、主入口状态、绝对路径警告 | 用户确认 |
| zip 外壳 | 只去一层；`__MACOSX`、隐藏文件、`node_modules` 忽略 | 用户确认 |
| 文件夹怎么上传 | 浏览器里打成 tar.gz，服务端复用 `bundle.Unpack` | 协调者 |
| zip 在哪里解 | 服务端 `bundle.UnpackZip`，先落盘到 `<data-dir>/tmp` | 协调者 |
| 接口形态 | 后台域名的 `POST /apps/{slug}/upload`，返回 JSON；成功后跳 `?uploaded=N` | 协调者 |
| 警告怎么来 | 详情页渲染横幅时从 blob 重新计算 | 协调者 |

协调者定的几项，如果实现时发现有更好的做法可以改，但要在 session 总结里写明改了什么、为什么改。产品行为（PRD）不要自己改：觉得 PRD 有问题时，停下来在 tab 里说明，等用户决定。

## 不在范围内

- plan 第 8 步：发版、push、打 tag、部署。做完第 7 步就停下汇报，由协调者去问用户。
- README 截图、`SKILL.md`、CLI 支持 zip。
- 范围外发现的问题：
  - 符合 `kb/known-issues.md` 记录标准的（会影响使用或稳定，并且还有事要做），记进去。这个文件还不存在，需要新建。
  - 其余的写进 session 总结的「遗留问题」。
  - 不要顺手修范围外的代码。

## 工作方式

- BDD：每一组行为先写测试，跑一次确认是红的，再实现。
- Go 命令一律加 `mise exec --` 前缀。改了 `internal/server/views/` 之后，要重新 build `tinyhost-server` 再做浏览器验证，因为模板和静态文件是 embed 进二进制的。
- 本地服务：
  - `--apps-url http://localhost:3471 --admin-url http://admin.localhost:3471 --insecure-cookies`。
  - 数据目录和样例都放在 `tmp/` 下，不碰真实的 `~/Library/Application Support/tinyhost`。
  - 后台密码用 `tinyhost-server passwd --data-dir …` 设置，随便设，不要写进提交。
- 浏览器验收用 agent-browser。做完以后关掉自己的浏览器会话、停掉本地服务，执行 `mac-dev-cleanup --only browser --min-age 2`。加 `--min-age 2` 是因为别的会话可能还在用浏览器。
- 不在本机跑 docker 命令。

## 约束

- 只 `git add` 自己改过的文件，不要 `git add -A`：同一个 checkout 里可能有别的未提交改动。
- 不 push，不打 tag，不改 deploy 仓库。
- 仓库是公开的：提交、kb 文档和截图里不能出现线上服务器的 IP、token、密码。
- Agent API（`/_/api/v1/...`）的行为和 JSON 输出不能变；`internal/api` 的 DTO 只增不改。

## 完成后

在 tab 里给出最终汇报：

- 提交 hash
- 测试结果
- 证据目录 `tmp/2026-11-03-manual-upload/`
- session 总结的路径
- 自己做的、偏离 plan 的决定
- 需要用户手动验的项（至少有「从访达真实拖入文件夹」）
- 遗留问题
