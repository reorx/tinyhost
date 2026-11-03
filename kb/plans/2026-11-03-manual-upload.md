---
created: 2026-11-03
tags:
  - admin-console
  - upload
  - bundle
  - zip
---

# 后台 App 详情页手动上传新版本（文件夹、zip、拖拽）

## PRD

### 背景

tinyhost 主要给 Agent 用：Agent 用 CLI 把目录发布成 App。Owner 有时也想自己用它快速建站，比如上线一份自己写的页面，或者把从别处拿到的打包文件（里面有 `index.html` 和资源文件）挂出去。

现在后台可以新建 App，但建好之后没有办法发布版本。详情页的空状态只能提示「让 Agent 运行 `tinyhost deploy`」。Owner 想自己发一版，就得装 CLI、走设备授权，对「拖个文件夹上去」这种事太重了。

从别处拿到的打包文件大多是 zip：GitHub 的「Download ZIP」、AI 建站或导出工具、macOS 的「压缩」都生成 zip。这类构建产物还常常用 `/assets/...` 这种以 `/` 开头的绝对路径引用资源，放在 `tinyhost.space/<slug>/` 下会加载不到，页面白屏。

### 目标

Owner 在后台 App 详情页把文件夹或 zip 拖进去（或点按钮选），就生成一个新版本，页面上直接给出这个版本的链接，点一下就在新标签页打开。

### 功能与交互

**入口和布局**

- App 详情页，摘要信息下面、版本列表上面，新增「上传新版本」区域。
- 区域里有：
  - 「说明（可选）」输入框，相当于 CLI 发布时的说明，会显示在版本列表里。
  - 「设为主入口」勾选框，默认勾上。
  - 一个拖放框，写着「把文件夹、.zip 或 .html 拖到这里」，框里有「选择文件夹」和「选择 .zip / .html」两个按钮。
  - 这台服务器的上限，比如「最大 50 MB · 1000 个文件」，数值取服务器配置。
  - 一句提示：页面里引用资源要用相对路径。
- 浏览器关闭了 JavaScript 或者太旧、不支持上传时，区域里只显示一句说明，不显示拖放框和按钮。
- 浏览器不支持选择文件夹时（例如手机），不显示「选择文件夹」按钮，其余照常。

**支持的内容**

| 拖入或选择的东西 | 怎么处理 |
|---|---|
| 一个文件夹 | 文件夹里面的内容就是网站根目录，文件夹本身的名字不算在路径里（和 CLI 的 `tinyhost deploy <目录>` 一样） |
| 一个 `.zip` | 解开后作为网站根目录。如果根目录没有 `index.html`，而所有内容都在同一个文件夹里、那个文件夹里有 `index.html`，就自动去掉这层外壳 |
| 一个 `.html` / `.htm` 文件 | 作为 `index.html` 发布 |
| 一次拖入多个文件或文件夹（比如在访达里全选 `dist/` 里的内容拖进来） | 全部放在网站根目录下，文件夹保留自己的名字 |

规则和 Agent 发布一致：

- 隐藏文件和文件夹（名字以 `.` 开头，`.well-known` 除外）、`node_modules`、zip 里 macOS 生成的 `__MACOSX` 都自动忽略。
- 根目录的 `tinyhost.json` 只当元数据（标题、数据目录说明），不作为网页文件提供。里面写了标题时，App 标题跟着更新。
- 新版本替换的是 App 包这一层，主入口版本上已有的覆盖目录保留。
- 操作记录里记一条「发布 App 包」，操作者是 Owner；配置了通知的，和 Agent 发布一样发通知。

**上传过程**

- 拖入或选好以后立即开始上传，没有二次确认。
- 只有拖到拖放框里才算数。拖到页面其他地方松手，什么都不发生（浏览器也不会跳去打开那个文件）。拖着文件经过拖放框时，框高亮。
- 上传前先在浏览器里检查：根目录有没有 `index.html`、文件数和总大小有没有超过上限。不通过就直接报错，不发请求。
- 上传中依次显示「正在读取文件」「正在打包」「上传中 xx%」（带进度条）「服务器处理中」。这期间按钮不可用，再拖入也不响应。
- 上传中想离开页面，浏览器会先确认。

**上传成功**

- 页面刷新，顶部出现结果横幅：
  - 标题「已发布 vN」。
  - 这个版本的链接，点击在新标签页打开；旁边有「打开」和「复制」按钮。
  - 主入口状态：勾了「设为主入口」时写「主入口已切到 vN」并附主入口链接；没勾时写「没有设为主入口，主入口仍是 vM」（还没有主入口时写「还没有主入口版本」）。
  - 如果这个版本的 HTML、CSS、JS 里有以 `/` 开头的绝对路径，列出可疑的行（文件名、行号、内容，最多 20 条），并说明这些资源在 `/<slug>/` 下可能加载不到，改成相对路径后重新上传。判断规则和 CLI 发布时的警告一样。
  - 「关闭」链接，回到不带横幅的详情页。
- 版本列表、当前版本、最近操作都已经是上传后的状态。
- App 是私有的，点版本链接时照常走后台登录态，不用再输密码。

**出错**

错误显示在上传区域里，说清原因，不会生成新版本，可以直接重新拖入。

| 情况 | 提示 |
|---|---|
| 根目录没有 `index.html` | 说明缺 `index.html`。如果在下一层文件夹里找到了它，指出是哪个文件夹（例如「在 dist/ 里找到了 index.html，可以只拖入 dist 文件夹」） |
| 超过大小或文件数上限 | 说明上限是多少 |
| zip 损坏、加密，或文件名不是 UTF-8 编码（常见于 Windows 中文系统打的 zip） | 说明原因；文件名编码的情况建议解压后拖文件夹 |
| 包里有不安全的路径（如 `../`）或符号链接 | 说明哪个文件有问题 |
| 不支持的文件类型（如 `.rar`、`.tar.gz`） | 说明只支持文件夹、`.zip`、`.html` |
| 后台登录已过期 | 提示刷新页面重新登录 |
| 文件夹是空的，或者全是被忽略的文件 | 说明没有可发布的文件 |
| 网络中断 | 说明上传失败，可以重试 |

**其他**

- 版本列表为空时的提示，改成同时提到上面的上传区域和 Agent 的 `tinyhost deploy`。

### 不做的事

- `.tar.gz`、`.rar`、`.7z` 等其他压缩格式（解压后拖文件夹即可）。
- 在「新建 App」表单里一步建好并上传。
- 在后台推送或替换覆盖目录（`tinyhost data push` 的功能）。
- 在线浏览、编辑版本里的文件。
- 断点续传、超过服务器上限的大文件上传。
- zip 外壳只去掉一层，不猜更深的目录（比如 `repo-main/dist/index.html` 不会自动用 `dist/`，只在错误提示里指出来）。
- 手机上选择文件夹（手机上可以选 zip 或 html）。
- CLI 支持 zip。

### 验收标准

1. 详情页摘要下方有「上传新版本」区域，有说明输入框、默认勾上的「设为主入口」、拖放框、两个选择按钮和服务器上限。
2. 拖入一个含子目录的文件夹，生成新版本；版本链接里每个文件都能访问，包括子目录里的文件、中文文件名、路径很长的文件。
3. 用「选择文件夹」选同一个文件夹，结果和拖入一样。
4. 拖入一个根目录有 `index.html` 的 zip，生成新版本，内容正确。
5. 拖入一个用 macOS「压缩」打的、外面包着一层文件夹的 zip，外壳被去掉，`__MACOSX` 和 `.DS_Store` 没有出现在版本里。
6. 拖入单个 `.html`，版本里是 `index.html`。
7. 全选一个文件夹里的内容拖入，结果和拖入这个文件夹一样。
8. 上传成功后页面顶部显示「已发布 vN」和可点击的版本链接，点击在新标签页打开这个版本；「复制」能复制链接。
9. 勾选「设为主入口」时主入口切到新版本，横幅写明；不勾时主入口不变，横幅写明主入口仍是哪个版本。
10. 填写的说明出现在版本列表里；操作记录里有一条 Owner 的「发布 App 包」。
11. 上传前主入口有覆盖目录的，上传后的新版本仍带着这些覆盖目录。
12. 上传的内容里有 `/assets/...` 这类绝对路径时，横幅列出对应的文件和行号；没有时不显示这部分。
13. 缺 `index.html`、超过上限、zip 损坏或加密、不支持的文件类型，都在上传区域显示对应的错误，版本数不变。
14. 缺 `index.html` 且下一层文件夹里有时，错误提示指出那个文件夹（文件夹和 zip 都要验）。
15. 隐藏文件、`node_modules` 没有出现在版本里。
16. 拖到上传区域以外的地方松手，不上传，页面也不跳走。
17. 上传中显示进度，按钮不可用。
18. 没有登录后台、或者从 apps 域名的页面发起，都不能上传。
19. Agent 用 CLI 发布的行为和输出不变。
20. 浅色、深色模式下区域和横幅都正常；窄屏（手机宽度）下不溢出。

## Tech Design

### 现状

- **Agent 发布**：`internal/server/api.go` 的 `deploy` 先持有 `s.lifecycle.RLock()`，再由 `unpack` 处理请求体。`unpack` 根据 `Config.MaxUploadMB`（默认 50）和 `Config.MaxFiles`（默认 1000）算出 `bundle.Limits`，用 `http.MaxBytesReader` 限制请求体为 `MaxSize + 文件数×2048 + 2MiB`，然后调用 `bundle.Unpack`。`Unpack` 的 sink 把每个文件交给 `s.Blobs.Put`，结果追加到 `store.Layer.Files`。出错时 `unpack` 用 `fail` 写 Agent 风格的 JSON（413 `too_large` / 400 `bad_bundle`，提示里提到 CLI）。之后 `Store.CommitAppVersion(appID, layer, manifest, msg, by, promote)` 在一个事务里写入层和版本、沿用主入口版本的覆盖层、按 manifest 标题更新 App 标题；`s.deployed` 返回 201 的 `api.DeployResult`。
- **`internal/bundle`**：
  - `Unpack` 流式读取 tar.gz，处理规则包括：`normalizePath`（拒绝空段、`.`、`..`、反斜杠、NUL，长度不超过 512）、`hidden`（以 `.` 开头的段跳过，`.well-known` 除外）、拒绝重复路径和非普通文件、根目录的 `tinyhost.json` 走 `readManifest`。
  - 缺 `index.html` 时，`Unpack` 和 `Pack` 都返回 `fmt.Errorf("bundle requires index.html")`，没有哨兵错误。
  - `node_modules` 只在 CLI 侧的 `Pack` 里排除。
  - `check.go` 的 `CheckAbsolutePaths` 用正则找绝对路径引用，每个文件最多 5 条、总共最多 20 条。`checkable`（`.html/.htm/.css/.js/.mjs`）没有导出。
- **后台**：
  - `internal/server/admin.go` 的 `adminHandler` 注册路由，`adminMiddleware` 统一处理 CSP（`default-src 'self'`，所以同源的 XHR 可以发）、所有 POST 的 Origin 校验和会话检查；没有会话的 POST 返回 HTML 错误页 401，Origin 不对返回 403。
  - `internal/server/console.go` 的 `consoleApp` 渲染 `app.html`，view model 是 `AppView`；表单类操作走 `consoleForm`（请求体限制 16KiB）和 302。后台的业务错误一律是 HTML 错误页（`adminError`）。
  - `ownerAppEvent(action, appID, detail)` 写审计并触发 `OnEvent`。`cmd/tinyhost-server/notify.go` 处理 `app.deployed` 时读 detail 里的 `version` 和 `message`，并把 actor `owner` 显示为 Owner。
- **页面**：
  - `app.html` 的版本表空状态只提 `tinyhost deploy`。
  - `shared.html` 的 `assets` 模板给所有页面加载 `admin.js`（defer）。
  - CSP 不允许内联脚本和 `style` 属性。
  - 复制按钮的规则：`data-copy` 按钮默认 `hidden`，有剪贴板时由 `admin.js` 显示。
  - `agents.html` 顶部的 `.issued` 是现成的「一次性结果」横幅样式。
- **存储**：
  - `store.File{Path, Blob, Size}`，`store.Layer{ID, …, Files}`；`layer_files` 表按 `(layer_id, path)` 存文件。现在没有按层列出文件的方法，只有 `LookupFile`。
  - `blob.Store` 在 `<data-dir>/tmp` 写临时文件，这个目录和 `blobs` 在同一个文件系统上。生产环境的容器根文件系统是只读的，只有 `/data` 可写，所以不能用 `os.TempDir()`。

### 方案

浏览器负责把文件夹、单个 html 和多个散文件打成和 CLI 一样的 tar.gz，zip 原样上传。服务端在后台域名新增一个只接受 Owner 会话的上传接口，tar.gz 走现有的 `bundle.Unpack`，zip 走新增的 `bundle.UnpackZip`，两条路径共用 limits、sink 和提交逻辑。上传成功后返回 JSON，前端跳转到 `/apps/<slug>?uploaded=N`。详情页看到这个参数，就在服务端渲染结果横幅，并从已存储的文件里重新跑一遍绝对路径检查。

关键决策：

| 决策 | 理由 | 否决的备选 |
|---|---|---|
| 文件夹在浏览器里打成 tar.gz（自己写 tar 头，用原生 `CompressionStream('gzip')` 压缩） | 服务端只认一种文件夹格式，复用已经测过、带安全检查的 `Unpack`；tar 头格式简单 | multipart 逐个上传文件：Go 的 `Part.FileName()` 会去掉目录，还要在服务端再写一遍路径和限额校验 |
| zip 在服务端用 `archive/zip` 解 | 标准库可靠，能写 Go 行为测试；中央目录让「去掉外壳」和「找 index.html 候选」很直接；`archive/zip` 读取时会检查实际解压字节不超过声明的大小，防 zip bomb | 浏览器里用 `DecompressionStream('deflate-raw')` 解 zip：JS 要手写中央目录解析，测不了，还要处理 ZIP64、编码标志 |
| zip 先落盘到 `<data-dir>/tmp/upload-*` 再解 | `zip.NewReader` 需要 `ReaderAt`；落盘不占内存，即使自部署方把上限调到几百 MB 也没问题；`tmp` 和 blobs 在同一文件系统、在可写卷里 | 整个读进内存：上限调大后内存会尖峰 |
| 新接口返回 JSON，成功后前端跳转 `?uploaded=N` | 上传要显示进度，只能用 XHR，XHR 处理 JSON 最直接；跳转后版本表、当前版本、操作记录都是新的，横幅由服务端渲染 | 普通表单 POST 后直接渲染页面：没有进度，刷新会重复提交；只在面板里内联显示结果：版本表不会更新 |
| 横幅的绝对路径警告在详情页渲染时从 blob 重新计算 | 不需要在跳转之间传数据，全部在 Go 里，可以测试；只在带 `uploaded` 参数时计算，并限制扫描量 | 用 sessionStorage 把上传响应带到下一页：只能在 JS 里实现，测不了 |
| 「去掉外壳」和 `__MACOSX`、`node_modules` 过滤只放在 `UnpackZip`，`Unpack` 不变 | Agent 的 tar.gz 接口和 CLI 的 JSON 契约保持不变 | 在 `Unpack` 里加：流式读取时还不知道是否所有文件都在同一个文件夹下，而且会改动 Agent 接口的行为 |
| 上传脚本单独放 `static/upload.js`，只在 `app.html` 加载 | `admin.js` 是所有页面共用的小脚本，上传逻辑有两三百行 | 都塞进 `admin.js` |

### 数据变动

没有 schema 变动，也不用迁移。新增的都是读取接口和临时文件：

- `Store.LayerFiles(layer string) ([]File, error)`：按 path 排序返回一层的所有文件（`SELECT path,blob,size FROM layer_files WHERE layer_id=? ORDER BY path`）。
- zip 上传时的临时文件放在 `<data-dir>/tmp/upload-*`，处理完由 defer 删除。进程崩溃留下的残留和现有 `blob-*` 一样不做清理。

### 逻辑变动

**`internal/bundle`**

- `bundle.go`：
  - 新增 `var ErrNoIndex = errors.New("bundle requires index.html")`。`Unpack` 原来返回 `fmt.Errorf("bundle requires index.html")` 的地方改成返回 `ErrNoIndex`，文字不变，所以 Agent JSON 和 CLI golden 不受影响。
  - 新增 `type MissingIndexError struct{ Candidates []string }`：`Error()` 返回 `ErrNoIndex.Error()`，`Unwrap()` 返回 `ErrNoIndex`。`Candidates` 是含有 `index.html` 的目录（带结尾 `/`），按层级由浅到深、同层按字典序，最多 3 个。
  - 新增 `var ErrEncrypted = errors.New("encrypted zip entries are not supported")`。
- `pack.go`：缺 index 时改为返回 `ErrNoIndex`。
- `check.go`：`checkable` 导出成 `Checkable`，内部调用随之改名。
- 新文件 `zip.go`：`func UnpackZip(r io.ReaderAt, size int64, limits Limits, sink func(string, io.Reader) error) (Summary, error)`，语义对齐 `Unpack`：
  1. `zip.NewReader(r, size)`，有任何错误都返回，包括 `zip.ErrInsecurePath`，因为那说明有不安全路径。
  2. 收集条目：
     - 跳过目录条目（名字以 `/` 结尾或 `FileInfo().IsDir()`）。
     - 跳过第一段是 `__MACOSX` 的条目，不计入任何统计。
     - `f.Flags&0x1 != 0` 返回 `ErrEncrypted`。
     - `!f.Mode().IsRegular()`（符号链接等）返回 `unsupported entry`。
     - 名字不是合法 UTF-8 时返回 `fmt.Errorf("file name is not UTF-8: %q", name)`。
     - 名字过一遍 `normalizePath`。
  3. 分类：路径里有 `hidden` 段或 `node_modules` 段的，记进 `Skipped`，不计入限额。其余的是「保留条目」。
  4. 去外壳：只在 `limits.RequireIndex` 时做。条件是保留条目里没有 `index.html`、全部保留条目的第一段都是同一个 `X` 且都含 `/`、并且 `X/index.html` 存在。满足时，所有条目的路径都去掉前缀 `X/`。不在 `X/` 下的被跳过条目（例如根目录的 `.DS_Store`）在 `Skipped` 里保留原名。
  5. 去外壳之后检查重复路径；`count > MaxFiles`，或者 `UncompressedSize64` 之和超过 `MaxSize`，返回 `ErrTooLarge`。
  6. 按归档顺序处理保留条目：根目录的 `tinyhost.json` 走 `readManifest`，其余的 `f.Open()` 后交给 sink，再 drain、Close，累计 `Files`、`Size`，记录是否有 `index.html`。`archive/zip` 在实际解压字节超过声明大小或 CRC 不对时会报错，原样返回。
  7. `RequireIndex` 且没有 `index.html` 时，返回 `&MissingIndexError{Candidates: …}`，候选从保留条目（去外壳之后）里找。

**`internal/blob`**

- 新增 `func (s *Store) TempFile() (*os.File, error)`：在 `<root>/tmp` 里 `os.CreateTemp(…, "upload-*")`，删除由调用者负责。

**`internal/store`**

- `versions.go`：新增 `LayerFiles`（见「数据变动」）。

**`internal/server`**

- `api.go`：把 `unpack` 里和响应无关的部分抽成两个小函数，Agent 的 `unpack` 改用它们，行为和错误输出完全不变：
  - `func (s *Server) uploadLimits(dir string) (bundle.Limits, int64)`：返回限额和请求体上限。
  - `func (s *Server) layerSink(layer *store.Layer, dir string) func(string, io.Reader) error`。
- 新文件 `upload.go`：
  - `func (s *Server) consoleUpload(w, r)`，路由 `POST /apps/{slug}/upload`，注册在 `adminHandler`（继承会话和 Origin 校验）：
    1. `s.lifecycle.RLock()`，直到提交完成。
    2. 按 slug 找 App。不存在或已删除时返回 JSON 404 `app_not_found`。不用 `consoleFindApp`，因为它输出 HTML。
    3. 查询参数：`message`，超过 1024 字节返回 400 `bad_request`；`promote`，`"0"` 表示不设为主入口，其他值都设为主入口。
    4. 用 `mime.ParseMediaType` 解析 Content-Type：
       - `application/gzip`：`r.Body = http.MaxBytesReader(…)`，然后 `bundle.Unpack(r.Body, limits, sink)`。
       - `application/zip`：同样的 `MaxBytesReader`，`io.Copy` 到 `s.Blobs.TempFile()`，然后 `bundle.UnpackZip(f, n, limits, sink)`，defer 关闭并删除。
       - 其他类型返回 415 `unsupported_type`。
    5. 错误映射，响应都是 `api.Error` JSON，文案写给 Owner，用中文：
       - `ErrTooLarge` 或 `*http.MaxBytesError` → 413 `too_large`，「超过上限：最多 X MB、Y 个文件」。
       - `errors.Is(err, bundle.ErrNoIndex)` → 400 `no_index`，「根目录没有 index.html」。拿得到 `MissingIndexError` 的候选时，hint 写「在 dist/ 里找到了 index.html，可以只上传这个文件夹」。
       - `ErrEncrypted` → 400 `bad_bundle`，「不支持加密的 zip」。
       - 其余 → 400 `bad_bundle`，「无法解析上传的内容：」加原错误。
    6. `s.Store.CommitAppVersion(a.ID, layer, summary.Manifest, message, "owner", promote)`，出错交给 `s.mutationError`。
    7. `s.ownerAppEvent("app.deployed", a.ID, map[string]any{"version": v.Number, "message": v.Message})`，detail 和 Agent 发布同形，通知文案不用改。
    8. `s.deployed(w, a, v, summary, promote)` 返回 201 的 `api.DeployResult`。
  - `func (s *Server) versionPathWarnings(v *store.Version) ([]bundle.Warning, error)`：
    - 用 `LayerFiles(v.BaseLayer)` 取文件，按 path 顺序处理 `bundle.Checkable` 且 `Size <= 1MiB` 的文件。
    - 逐个从 blob 读出内容，调用 `CheckAbsolutePaths(map[string][]byte{path: content})`，结果累加。
    - 到 20 条、扫过 200 个文件或累计读了 8MiB 时停止。
- `console.go`：
  - `AppView` 只增字段：
    - `Upload UploadPanel{MaxMB, MaxFiles int; MaxBytes int64}`：数值和 `uploadLimits` 同源，默认 50 / 1000。
    - `Uploaded *UploadedView{Number int; URL string; Current bool; PathWarnings []bundle.Warning}`。
  - `consoleApp` 读取 `?uploaded=`：是正整数、并且版本存在时，填 `Uploaded`（URL 规则和 `VersionRow.URL` 一致，`Current` 看 `a.CurrentVersion`）。`versionPathWarnings` 出错只记日志、不显示警告，页面照常渲染。参数不合法或版本不存在时不显示横幅，也不报错。
- `admin.go`：在 `adminHandler` 里注册 `POST /apps/{slug}/upload` 到 `consoleUpload`。这条路由不能走 `consoleAppAction` 的循环，因为后者会调 `consoleForm`，把请求体限制在 16KiB。

**`internal/server/views`**

- `app.html`：
  - `<head>` 里在 `head` 模板之后加 `<script src="/static/upload.js" defer></script>`。
  - 横幅：`{{with .Uploaded}}` 放在 `<main>` 顶部、面包屑之前，复用或参照 `.issued` 样式，带 `data-upload-result` 标记。内容按 PRD：
    - 版本链接（`target="_blank" rel="noopener"`）。
    - 「打开」按钮，「复制」按钮（`data-copy`，默认 `hidden`）。
    - 主入口状态。
    - 有 `PathWarnings` 时用 `callout-warn` 列出 `文件:行 内容`。
    - 「关闭」链接到 `/apps/<slug>`。
  - 上传区域：`<section class="section upload" data-upload data-slug=… data-max-bytes=… data-max-files=…>`，放在摘要 `<dl class="summary">` 之后、版本区之前：
    - 默认只显示 `<p data-upload-unsupported>`，内容是「上传需要启用 JavaScript 的较新浏览器」。
    - 交互部分包在 `<div data-upload-ui hidden>` 里：说明输入框（`maxlength=1024`）、「设为主入口」checkbox（默认 checked）、拖放框 `[data-dropzone]`、「选择文件夹」按钮 `[data-pick-dir]` 和对应的 `<input type="file" webkitdirectory hidden>`、「选择 .zip / .html」按钮 `[data-pick-file]` 和 `<input type="file" accept=".zip,.html,.htm" hidden>`、上限说明、相对路径提示。
    - 状态行 `[data-upload-status]`（文字加 `<progress>`）和错误区 `[data-upload-error]`（`alert alert-danger`），都默认 `hidden`。
  - 版本表空状态改为：「把文件夹或 .zip 拖到上面的「上传新版本」，或者让 Agent 运行 `tinyhost deploy <目录> --app <slug>`。」
- `static/admin.css`：新增 `.upload`、`.dropzone`、`.dropzone.is-dragover`、`.dropzone[aria-disabled="true"]`、上传状态行、`progress` 和横幅的样式。浅色、深色都用 `:root` 里的令牌，窄屏不溢出。不写 `style` 属性。
- 新文件 `static/upload.js`，页面没有 `[data-upload]` 时什么也不做：
  1. **能力检测**：需要 `CompressionStream`、`DataTransfer`、`XMLHttpRequest`、`TextEncoder`。满足时隐藏 `[data-upload-unsupported]`、显示 `[data-upload-ui]`。`'webkitdirectory' in document.createElement('input')` 为假时隐藏「选择文件夹」。
  2. **收集文件**，结果是 `{path, file}` 列表，或者一个 `{zip: file}`：
     - 拖入：必须在 `drop` 事件里**同步**把 `dataTransfer.items` 逐个转成 `webkitGetAsEntry()`（handler 返回后 items 就失效），返回 null 时回退到 `getAsFile()`。然后分类：
       - 只有一个目录：递归遍历，路径相对于这个目录。`readEntries` 要循环调用直到返回空数组。
       - 只有一个 `.zip` 文件：走 zip。
       - 只有一个 `.html` / `.htm` 文件：路径设为 `index.html`。
       - 其余情况（多个条目，或单个其他文件）：都放在根目录，目录保留自己的名字。
     - 选择文件夹：`file.webkitRelativePath` 去掉第一段。
     - 选择文件：`.zip` 走 zip，`.html` / `.htm` 设为 `index.html`。处理完把 input 的 value 清空，这样再次选择同一个文件也会触发。
  3. **过滤和预检**（只针对文件列表）：
     - 任一段以 `.` 开头（`.well-known` 除外）或等于 `node_modules` 的跳过。
     - 根目录没有 `index.html` 时报错。在保留文件里找最浅的 `*/index.html`，最多列 3 个目录，提示「在 X/ 里找到了 index.html，可以只拖入 X 文件夹」。
     - 列表为空时报「没有可发布的文件」。
     - 文件数超过 `data-max-files`，或总字节数超过 `data-max-bytes` 时报错。
     - zip 只检查 `size > data-max-bytes`。
  4. **打包**（只针对文件列表）：拼一个 `Blob`，内容依次是每个文件的 tar 头、文件 Blob、补零到 512 的倍数，最后是两个 512 字节的零块。tar 头是 USTAR 格式：
     - `name` 用 UTF-8，不超过 100 字节时直接写入；超过时在前面加一个 PAX 头（typeflag `x`），记录 `"<len> path=<path>\n"`，其中 `len` 包含它自己的位数。
     - `mode` 写 `0000644`，`size` 和 `mtime` 用 11 位八进制加 NUL，typeflag `0`，magic 写 `ustar\0`，version 写 `00`。
     - `chksum` 先填 8 个空格再算总和，写成 6 位八进制、NUL、空格。
     - 然后 `blob.stream().pipeThrough(new CompressionStream('gzip'))`，再用 `new Response(…).blob()` 得到上传体。文件 Blob 是惰性引用，不会先把所有文件读进内存。
  5. **上传**：用 XHR `POST /apps/<slug>/upload?message=…&promote=1|0`，Content-Type 设为 `application/gzip` 或 `application/zip`。
     - `xhr.upload.onprogress` 更新进度条；上传完成后状态变成「服务器处理中」。
     - 上传期间设置 `aria-busy`、禁用按钮、忽略新的拖入，注册 `beforeunload` 提示。
  6. **结果**：
     - 201：`location.assign('/apps/' + encodeURIComponent(result.app) + '?uploaded=' + result.version)`。
     - 失败时，响应是 JSON 的就显示 `message` 和 `hint`。不是 JSON 的按状态码给提示：401 是「登录已过期，请刷新页面重新登录」，403 是「请求被拒绝，请刷新页面后重试」，413 是超过上限，其他显示「上传失败（HTTP N）」。网络错误显示「网络中断，上传失败，可以重试」。出错后恢复可用状态。
  7. **拖放细节**：
     - `dragenter` / `dragover` 到拖放框时加 `is-dragover`，`dragleave` / `drop` 时移除。`dragleave` 要判断是不是真的离开了框，不能因为经过子元素就移除。
     - 在 `document` 上监听 `dragover` / `drop`：落点不在拖放框内时 `preventDefault`，防止浏览器打开文件，也不上传。
     - 拖放框加 `tabindex="0"`，按回车或空格等同于「选择文件夹」，不支持选择文件夹时等同于「选择文件」。

**文档**

- `README.md`：第 31 行「Owner control」那条补一句，Owner 可以在 App 页面把文件夹或 `.zip` 直接上传成新版本。截图不更新。
- `docs/json-output.md`：不用改，新接口在后台域名，不属于 Agent API。session 总结里写清这个接口的请求和响应。
- `AGENTS.md`：代码地图更新（见实施步骤）。

### 实施步骤

- [x] 1. bundle：先写 `zip_test.go` 的行为测试（红），再实现 `ErrNoIndex`、`MissingIndexError`、`ErrEncrypted`、`Checkable`、`UnpackZip`。确认 `Unpack`、`Pack` 的现有测试和 CLI golden 不变。
- [x] 2. blob 和 store：`TempFile`、`LayerFiles`，各配一个行为测试。
- [x] 3. server：先写上传接口和详情页横幅的行为测试（红），再抽 `uploadLimits` / `layerSink`，实现 `upload.go`、注册路由、扩展 `AppView` 和 `consoleApp`。`go test ./...` 全绿。
- [x] 4. 页面：`app.html` 的上传区域、横幅、空状态文案，`admin.css`，`upload.js`。改完重新 build `tinyhost-server`。
- [x] 5. 本地实机验收（见「测试与验证」），截图和样例归档到 `tmp/2026-11-03-manual-upload/`。
- [x] 6. README 补一句；写 session 总结 `kb/sessions/2026-11-03-th-manual-upload.md`，包含「页面清单增量」：路由、请求和响应、view model 新字段、`data-*` 钩子。
- [x] 7. 更新 AGENTS.md 代码地图，每处一两句：
  - `internal/bundle`：`UnpackZip`，只用于后台上传，会去外壳、过滤 `__MACOSX` 和 `node_modules`。
  - `internal/server`：`upload.go` 是后台唯一返回 JSON 的写接口，`?uploaded=` 横幅的警告从 blob 重新计算。
  - `views`：`upload.js` 只在 `app.html` 加载。

  然后一次提交。
- [x] 8. 发版和上线：由协调者问过用户后再做，不在本次执行范围内。

### 测试与验证

测试名写成行为句子，函数上方加 `// Behavior:` 注释。zip 测试用 `archive/zip` 的 `Writer` 在内存里造包：加密条目用 `CreateRaw` 并设置 `Flags |= 1`；「声明大小比实际小」也用 `CreateRaw` 写错误的 `UncompressedSize64`；符号链接用 `FileHeader.SetMode(fs.ModeSymlink|0777)`。

| 验收标准 | 自动测试（Go） |
|---|---|
| 4 | `UnpackZip`：根目录有 `index.html` 的 zip 完整解出，`Files`、`Size` 正确；server：`application/zip` 上传后，版本链接里每个文件都能访问 |
| 5 | `UnpackZip`：单层外壳被去掉；`__MACOSX/` 的条目既不进版本也不进 `Skipped`；根目录 `.DS_Store` 进 `Skipped`；外壳里的 `tinyhost.json` 被当成 manifest |
| 2、3、6、7（服务端部分） | server：`application/gzip` 上传后 201，`version_url` 正确，嵌套路径、中文文件名、超过 100 字节的路径（测试里用 `tar.Writer` 写 PAX）都能取回 |
| 8 | server：`GET /apps/demo?uploaded=1` 有 `data-upload-result`、版本链接和 `target="_blank"`；`?uploaded=99`、`?uploaded=x` 没有横幅 |
| 9 | server：`promote=0` 后主入口不变，横幅写「主入口仍是 v1」；默认上传后主入口指向新版本 |
| 10 | server：`message` 写进版本；`Events` 里有 actor `owner`、action `app.deployed`；`OnEvent` 收到的 detail 有 `version` |
| 11 | server：先用 Agent 推送覆盖目录，再由 Owner 上传，新版本带着原来的覆盖层 |
| 12 | server：上传内容含 `<script src="/assets/a.js">` 时，横幅列出 `index.html` 和行号；内容干净时没有警告块 |
| 13 | server：缺 index → 400 `no_index`；超过上限（测试里把 `MaxFiles` 调小）→ 413；`text/plain` → 415；乱码 zip、加密 zip → 400；`message` 超长 → 400；已删除的 App → 404 JSON。每种情况都检查版本数不变 |
| 14（zip） | `UnpackZip`：`repo/README.md` 加 `repo/dist/index.html` → `MissingIndexError.Candidates == ["repo/dist/"]`，并且 `errors.Is(err, ErrNoIndex)`；server 的 hint 里有 `repo/dist/` |
| 15 | `UnpackZip`：隐藏文件和 `node_modules/` 进 `Skipped`，不进 sink，也不计入限额 |
| 18 | server：没有会话 → 401，版本数不变；Origin 是 apps 域名 → 403（加进 `TestAppPageCannotPerformControlledActions` 的路由表）；只带 Bearer token、不带会话 → 401 |
| 19 | 现有 Agent 测试和 CLI golden 全部不改且通过；新增：`Unpack` 缺 index 的错误满足 `errors.Is(err, ErrNoIndex)`，错误文字不变 |
| 1、20（服务端部分） | server：详情页有 `data-upload`、`data-max-bytes`、`data-max-files`（随 `Config` 变化）、`/static/upload.js`；空状态文案提到上传 |
| 其他 | `UnpackZip`：拒绝 `../x`、`/abs`、反斜杠、符号链接、非 UTF-8 文件名、去外壳后重复的路径、超过文件数或声明大小、实际解压超过声明大小。`LayerFiles` 按 path 排序。`TempFile` 建在 `<root>/tmp` 下 |

**本地实机验收**（agent-browser，Chrome；服务端加 `--insecure-cookies`，apps `localhost:3471`，admin `admin.localhost:3471`）：

1. 在 `tmp/2026-11-03-manual-upload/fixtures/` 下准备样例：
   - `site/`：有 `index.html`、`css/a.css`、`img/中文.png`、一个超过 100 字节的深层路径、一个空文件、`.DS_Store`、`node_modules/x.js`。
   - `site.zip`：用 `ditto -c -k --sequesterRsrc --keepParent site site.zip` 生成，带外壳和 `__MACOSX`。
   - `flat.zip`：根目录直接有 `index.html`。
   - `repo.zip`：`index.html` 在 `repo/dist/` 下。
   - `vite-dist/`：`index.html` 里引用 `/assets/index.js`。
   - `single.html`。
   - 一个 `.tar.gz`。
2. 新建一个公开的 App，在详情页逐项操作：
   - 用「选择文件夹」上传 `site/`（agent-browser 的 `upload` 对 `webkitdirectory` input 传目录）。
   - 用「选择 .zip / .html」上传 `site.zip`、`flat.zip`、`single.html`。
   - 用 eval 构造 `DataTransfer` 派发 `drop` 事件，模拟拖入单个 zip 和多个散文件。
   - 每次都截图横幅，并用 curl 访问 apps 域名，确认文件都在（包括长路径、中文名、空文件），隐藏文件和 `node_modules` 不在。
3. 错误路径：`repo.zip`（提示 `repo/dist/`）、`.tar.gz`（不支持）、一个缺 `index.html` 的文件夹（浏览器侧提示）、把 `--max-files` 调小后上传（超过上限）。每种都截图错误区。
4. 上传 `vite-dist/`，截图横幅里的绝对路径警告。
5. 不勾「设为主入口」上传一次，确认横幅和摘要里的当前版本。
6. 拖到拖放框以外的地方，用 eval 派发 `drop`，确认页面没跳、版本数不变。
7. 浅色、深色和 390px 宽度下各截一张上传区域和横幅。
8. 真实地从访达把文件夹拖进去做不了自动化，在汇报里列为「需要用户手动验一次」。

### 风险与待定

- **agent-browser 能不能给 `webkitdirectory` input 传目录**，取决于它底层的 Playwright 版本。传不了时，用 eval 构造带 `webkitGetAsEntry` 的拖放事件也模拟不了目录，只能把文件夹路径留给用户手动验，在汇报里写明。
- **zip 不是 UTF-8 编码**：直接拒绝，提示解压后拖文件夹，不尝试猜 GBK。
- **Go 1.27 的 `zipinsecurepath` 默认值**：不管默认值是多少，`NewReader` 返回 `ErrInsecurePath` 都按拒绝处理，各条目也都过一遍 `normalizePath`，两种情况都安全。
- **浏览器侧的 tar 打包没有 Go 单测覆盖**，靠服务端测试（PAX 长路径、中文名）加实机验收第 2 步交叉验证。实现时要特别注意 PAX 记录长度的自引用计算，以及 checksum 字段的格式。
- **`?uploaded=N` 可以手动构造**：只会显示一个已有版本的横幅，没有副作用。
- **上传接口是后台域名上第一个返回 JSON 的写接口**。会话和 Origin 校验仍由 `adminMiddleware` 统一负责，接口自己不做鉴权判断，以免和中间件不一致。
