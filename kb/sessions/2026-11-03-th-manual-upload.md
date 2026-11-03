---
created: 2026-11-03
tags:
  - tinyhost
  - admin-console
  - upload
  - zip
---

## 摘要（th-manual-upload）

- 做了什么：后台 App 详情页新增「上传新版本」。Owner 可以拖入或选择文件夹、`.zip`、单个 `.html`、多个散文件，立即生成新版本；成功后页面刷新，顶部横幅给出版本链接、主入口状态和绝对路径警告。计划见 `kb/plans/2026-11-03-manual-upload.md` 第 1–7 步；第 8 步（发版、部署）没做。
- 先写测试再实现：`internal/bundle/zip_test.go`、`internal/server/upload_test.go`，blob 和 store 各一个。
- 本地：`go test ./...`、`go vet`、`gofmt -l internal cmd skill` 通过。`internal/cli` 的 `TestLocalCommandsWorkWithoutCredentials` 在这台机器上失败（读了真实的 `~/Library/Application Support/tinyhost/config.json`，skill 里的服务器地址是线上域名），本改动前同样失败，换干净 `HOME` 就通过，见「遗留问题」。
- 证据：`tmp/2026-11-03-manual-upload/`（截图 01–19、`NOTES.md`、`curl-v1-folder.txt`、样例生成脚本）。

## 偏离 plan 的地方

- 单个文件既不是 `.zip` 也不是 `.html`（拖入或选择）时，浏览器直接报「不支持的文件类型：只支持文件夹、.zip 和 .html」。plan 的实现说明写的是放到根目录再报缺 `index.html`，但 PRD 的出错表要求提示不支持的文件类型（`.tar.gz`、`.rar`），按 PRD 做。多个文件一起拖入仍放根目录。
- `uploadLimits` 里把 `MaxFiles` 的默认值 1000 补上（plan 只在算请求体上限时用 1000）。这样上传面板和错误文案能显示真实的数字，Agent 路径的行为不变。
- 跳过的文件在上传时只在浏览器里过滤；服务端的 tar.gz 路径不过滤 `node_modules`，保持 `Unpack` 不变（和 plan 一致）。

## 页面清单增量

接 `kb/sessions/2026-11-02-th-m2-console.md` 的「页面清单」。

**新路由**：`POST /apps/{slug}/upload`（admin 域名；没有会话 401 HTML 错误页，Origin 不对 403，由 `adminMiddleware` 处理）。

请求：

| 位置 | 内容 |
|---|---|
| `Content-Type` | `application/gzip`（`Unpack`）或 `application/zip`（`UnpackZip`），其他 415 `unsupported_type` |
| query `message` | 版本说明，最长 1024 字节，超出 400 `bad_request` |
| query `promote` | `0` 表示不设为主入口，其他值都设为主入口 |
| body | 压缩包原始字节；请求体上限 = 大小上限 + 文件数 × 2048 + 2MiB |

成功：201，`api.DeployResult`（和 Agent 发布同形），`version_url` 是 `<apps>/<slug>@vN/`。操作记录里 actor 是 `owner`、动作 `app.deployed`、detail 有 `version` 和 `message`，通知文案不变。

失败：`api.Error` JSON，文案是中文。

| HTTP | code | 情况 |
|---|---|---|
| 400 | `no_index` | 根目录没有 `index.html`；zip 在下一层找到时 hint 写「在 repo/dist/ 里找到了 index.html，可以只上传这个文件夹」 |
| 400 | `bad_bundle` | zip 损坏、加密（「不支持加密的 zip」）、文件名不是 UTF-8、不安全路径、符号链接、重复路径等 |
| 400 | `bad_request` | `message` 太长 |
| 404 | `app_not_found` | App 不存在或已删除 |
| 413 | `too_large` | 超过大小、文件数或请求体上限，文案带具体上限 |
| 415 | `unsupported_type` | Content-Type 不是 gzip 或 zip |

**`GET /apps/{slug}`**：新增可选查询参数 `uploaded=N`；N 是现有版本号时显示横幅，否则忽略。

`AppView` 新增字段（只增不改）：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Upload` | `UploadPanel{MaxMB, MaxFiles int; MaxBytes int64}` | 上传区域显示和 `data-*` 用的上限，和 `uploadLimits` 同源 |
| `Uploaded` | `*UploadedView{Number int; URL string; Current bool; PathWarnings []bundle.Warning}` | 横幅；没有横幅时是 nil。`Current` 是该版本现在是否为主入口 |

**`data-*` 钩子**（`app.html`、`static/upload.js`）：

| 钩子 | 用途 |
|---|---|
| `[data-upload]` + `data-slug`、`data-max-bytes`、`data-max-files` | 上传区域根节点；`upload.js` 只在存在它时工作 |
| `[data-upload-unsupported]` | 无 JS 或浏览器太旧时显示的一句说明，能力检测通过后隐藏 |
| `[data-upload-ui]` | 交互部分，默认 `hidden` |
| `[data-upload-message]`、`[data-upload-promote]` | 说明输入框、「设为主入口」勾选框（默认勾上） |
| `[data-dropzone]` | 拖放框（`tabindex=0`，`is-dragover`，`aria-disabled`、`aria-busy`） |
| `[data-pick-dir]`/`[data-dir-input]`、`[data-pick-file]`/`[data-file-input]` | 两个选择按钮和对应的隐藏 input；不支持 `webkitdirectory` 时隐藏前者 |
| `[data-upload-status]`、`[data-upload-status-text]`、`[data-upload-progress]` | 状态行和进度条 |
| `[data-upload-error]` | 错误区，`alert alert-danger` |
| `[data-upload-result]`、`[data-upload-promotion]`、`[data-path-warnings]` | 横幅、主入口状态一句、绝对路径警告块（没有警告时不渲染） |

CSS 类：`.upload`、`.upload-options`、`.dropzone`、`.is-dragover`、`.upload-status`、`.upload-result`、`.warn-list`。版本表空状态文案已改为同时提到上传区域和 `tinyhost deploy`。

## 实现要点

- 浏览器：文件夹拖入时在 `drop` 里同步取 `webkitGetAsEntry()`，再递归读（`readEntries` 循环到空）；打 tar 用 USTAR 头，路径超过 100 字节时加 PAX `x` 头（`<len> path=<path>\n`，len 含自身位数），`CompressionStream('gzip')` 压缩。文件 Blob 惰性引用，不会先全读进内存。
- 服务端 zip：`io.Copy` 到 `Blobs.TempFile()`（`<data-dir>/tmp/upload-*`，defer 删除），再 `bundle.UnpackZip`。
- `lifecycle.RLock` 从接口开始持有到提交完成，和 GC 不会交错。
- 横幅警告：`versionPathWarnings` 读 `LayerFiles`，最多扫 200 个文件、8MiB、20 条，单个文件超过 1MiB 跳过；出错只记日志，页面照常渲染。

## 遗留问题

- `internal/cli` 的 `TestLocalCommandsWorkWithoutCredentials` 会读开发机上真实的 CLI 配置（服务器地址），机器上装过 tinyhost CLI 就失败；用干净 `HOME` 跑才稳。不在本次范围，没改。没有达到 `kb/known-issues.md` 的记录标准（不影响应用使用），所以没新建那个文件。
- 浏览器侧的 tar 打包没有单元测试，靠服务端 PAX 测试和实机验收（长路径、中文名、空文件）交叉验证。
- 上传进度条和「服务器处理中」状态在本地太快，没有截图。
- 真实地从访达拖入文件夹没法自动化，需要用户手动验。
