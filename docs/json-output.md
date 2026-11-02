# tinyhost JSON 契约

供 HTTP API 调用者和 CLI 实现者查阅。Go 类型见 `internal/api/api.go`。从 M1 起字段只增不改；新增字段必须可选。时间均为 Unix 秒，版本为正整数；`current_version: null` 表示尚无主入口。数组为空时输出 `[]`。

## HTTP API

基地址为 apps origin 下的 `/_/api/v1`。除 device/code 和 device/token 外，所有端点仅接受 `Authorization: Bearer <token>`；cookie 不参与鉴权，不开放 CORS。撤销或到期的 token 返回 401。响应 JSON 使用 UTF-8，API 响应为 `Cache-Control: no-store`，不返回 hash；只有设备轮询成功时返回一次 token。GET 同时支持 HEAD。

| 方法 | 相对路径 | 请求 | 成功状态 / DTO | 业务错误 |
|---|---|---|---|---|
| GET | `/whoami` | 无 | 200 `WhoamiResult` | unauthorized |
| GET | `/apps` | 无 | 200 `AppsResult` | unauthorized |
| GET | `/apps/{slug}` | 无 | 200 `AppDetail` | app_not_found |
| GET | `/apps/{slug}/versions` | 无 | 200 `VersionsResult` | app_not_found |
| POST | `/apps/{slug}/deploys` | tar.gz；query 见下 | 201 `DeployResult` | slug_invalid, slug_taken, app_not_found, bad_bundle, too_large, bad_request |
| POST | `/apps/{slug}/data` | tar.gz；必填 query `dir`，可选 `message` | 201 `DeployResult` | app_not_found, overlay_conflict, bad_bundle, too_large |
| DELETE | `/apps/{slug}/data` | 必填 query `dir`，可选 `message`；无 body | 201 `DeployResult` | app_not_found, overlay_conflict, overlay_not_found |
| POST | `/apps/{slug}/promote` | `PromoteRequest`：`{"version":3}` | 200 `PromoteResult` | app_not_found, version_not_found, bad_request |
| DELETE | `/apps/{slug}` | 无 | 200 `DeleteResult` | app_not_found, not_creator |

每个端点还可能返回 `unauthorized`（401）或 `internal_error`（500）。未知路径及错误 HTTP 方法由路由器返回 404 / 405，响应不保证为 JSON。

### 发布请求

上传 body 是原始 tar.gz，可使用 `Content-Type: application/gzip`。解包后的默认上限为 50MiB、1000 条目，隐藏文件和 manifest 也计入上限；另限制 tar 元数据、尾部解压量和压缩请求大小。App 包必须有 `index.html`；数据包不要求。上传中的软/硬链接、重复路径、绝对路径、`..` 等危险路径会导致整个请求失败。点开头的段（`.well-known` 除外）丢弃并记入 skipped。根 `tinyhost.json` 只解析为 manifest，不进入托管层。

`deploys` query：

| 参数 | 默认 / 含义 |
|---|---|
| `create=1` | 新建 App；省略则只更新已存在 App。回收站仍占用 slug。 |
| `visibility=public\|private` | 新建时默认 private；更新不修改可见性。 |
| `title=...` | 显式覆盖包中 manifest.title，空字符串可清空标题。 |
| `message=...` | 本版本说明，默认空字符串。 |
| `promote=0` | 只生成版本；省略或其他值则更新主入口。 |

`data` 的 `dir` 是相对 App 根的目录，服务端补末尾 `/`；不能是根、含点开头的段或与已有目录嵌套。包内路径相对于该目录，整体替换该目录。允许向其他 Agent 的 App 发布和推数据。数据操作总是基于主入口当前版本、生成新版本并 promote。DELETE data 的 files/size 为 0，skipped 为 []。

`DeployResult`（三个发布类端点统一返回）：

```json
{
  "app": "demo",
  "version": 1,
  "kind": "app",
  "data_dir": null,
  "files": 2,
  "size": 120,
  "version_url": "https://th.reorx.com/demo@v1/",
  "main_url": "https://th.reorx.com/demo/",
  "promoted": true,
  "current_version": 1,
  "app_created_by": "c_example",
  "skipped": []
}
```

`app` 为 slug 字符串，`version` 为数字，`kind` 为 app 或 data。`data_dir` 在 App 包发布为 null，数据操作为标准化后的目录字符串。files/size 只统计本次新增层中的托管文件；manifest 不计入。`promoted` 表示本次操作是否要求移动主入口；`current_version` 为响应时读取的主入口，并发操作可能已经再次移动它。`app_created_by` 为客户端 ID（未来 Owner 创建的为 owner）。

### 查询响应

`WhoamiResult`：

```json
{"client":{"id":"c_example","name":"dev-agent","created_at":1793577600,"expires_at":null},"server_version":"dev"}
```

`AppsResult` 为 `{"apps":[App,...]}`，只列未删除 App，按 updated_at 降序、slug 排序。`App`：

```json
{"id":"a_example","slug":"demo","title":"报告","visibility":"public","created_by":"c_example","current_version":1,"created_at":1793577600,"updated_at":1793577600,"main_url":"https://th.reorx.com/demo/"}
```

`AppDetail`：`{"app":App,"manifest":Manifest,"overlays":[Overlay,...]}`。

- `Manifest` 是主入口 App 包的元数据，字段 `app?: string`、`title?: string`、`data?: {目录:说明}`。空 manifest 为 `{}`。
- `Overlay` 为 `{"dir":"data/","pushed_by":"c_example","pushed_at":1793577600}`，保留该层最初推送者和时间。
- 无主入口时 manifest 为 `{}`、overlays 为 `[]`。

`VersionsResult` 为 `{"versions":[Version,...]}`，编号降序。`Version`：

```json
{"number":2,"kind":"data","data_dir":"data/","message":"新数据","created_by":"c_example","created_at":1793577600,"current":true}
```

App 包版本的 data_dir 为 null。GC 清理过的数据版本不再列出；App 包版本全部保留。

### promote 和删除响应

`PromoteResult`：`{"app":"demo","current_version":3,"main_url":"https://th.reorx.com/demo/"}`。

`DeleteResult`：`{"app":"demo","deleted":true,"trash_days":7}`。只有创建者能调用；删除后内容立即不可访问，回收站保留期内 slug 不释放。CLI 应提示可在该期限内请 Owner 恢复。

### 错误格式

DTO `Error` 包含 `ErrorDetail`：

```json
{"error":{"code":"app_not_found","message":"App 不存在或已删除","hint":"新建请加 --create"}}
```

| code | HTTP | 含义 / 附加可选字段 |
|---|---|---|
| unauthorized | 401 | 缺少、无效、过期或撤销的 Bearer token |
| app_not_found | 404 | App 不存在或已软删除 |
| slug_taken | 409 | `created_by: string`、`created_at: integer`、`deleted: boolean` 说明占用者及回收站状态 |
| slug_invalid | 400 | slug 长度 3–40，小写字母/数字/连字符，首尾非连字符，无 --，不得使用保留字 |
| not_creator | 403 | 只有创建者能删除；message 包含创建者 ID |
| bad_bundle | 400 | tar.gz、manifest、文件类型或路径不安全，或 App 包没有 index.html |
| too_large | 413 | 上传大小、条目或元数据超限 |
| overlay_conflict | 409 | 与已有目录嵌套；`existing_dirs: string[]` 列出已有目录 |
| overlay_conflict | 400 | 目录格式非法；无 existing_dirs |
| overlay_not_found | 404 | 要撤掉的覆盖目录不存在 |
| version_not_found | 404 | promote 目标不存在、非正整数或已被清理 |
| bad_request | 400 | 非法 visibility、JSON 格式错误或多余 JSON 内容 |
| internal_error | 500 | 服务端内部错误；细节只进服务端日志 |

## CLI --json

所有命令都接受 `--json`，可放在命令前后。成功和失败都向 stdout 输出一个缩进两空格的 JSON 文档并换行；stderr 用于登录链接和等待提示、非致命警告或 stdout 写入失败。未加 `--json` 时，成功信息写 stdout，错误以 `错误：`、`提示：` 写 stderr。

CLI 直接序列化下表的 `internal/api` DTO，不包装额外的 success、result 字段，不输出 store 模型。远端命令的字段和含义与上面的 HTTP 响应完全一致。

| 命令 | 成功 DTO |
|---|---|
| `deploy [路径]` | `DeployResult` |
| `data push <路径> --app SLUG --to 目录/` | `DeployResult` |
| `data rm --app SLUG --to 目录/` | `DeployResult`，files/size 为 0 |
| `apps` | `AppsResult` |
| `info <slug>` | `AppDetail` |
| `versions <slug>` | `VersionsResult` |
| `promote <slug> <vN>` | `PromoteResult` |
| `delete <slug>` | `DeleteResult` |
| `whoami` | `WhoamiResult`；文本模式另显示所连接的服务器地址 |
| `version` | `VersionResult`：`{"version":"dev"}`，值由构建注入 |
| `help`、无命令、`--help` | `HelpResult`：`{"help":"AGENTS: run ...\n..."}` |
| `skill` | `SkillResult`：`{"skill":"..."}`；M2 包含登录说明，M3 补全其他正文 |

本地错误也使用 `Error` DTO。远端业务错误保留服务端的 code/message/hint 和可选上下文字段。CLI 本地 code：`usage`（参数或配置地址用法错误）、`unauthorized`（缺少 token）、`slug_invalid`（本地 slug 校验失败）、`cli_error`（打包、配置读取、网络、响应解析等一般错误）。hint 可以为空字符串。

| 退出码 | 含义 |
|---|---|
| 0 | 成功；包括帮助和 skill 占位说明 |
| 1 | 一般错误，含发布/查询业务失败、打包失败、网络错误和输出失败 |
| 2 | 用法错误：未知命令/参数、位置参数数量错误、缺少 --app、非法 vN 或服务器地址 |
| 3 | 未授权：缺少凭证，或服务端 HTTP 401（无效/撤销/过期 token） |
| 4 | login 授权等待中；成功形状的 LoginResult，status 为 pending |

服务器读取顺序为 `--server` > `TINYHOST_SERVER` > `os.UserConfigDir()/tinyhost/config.json` 的 server 字段。macOS 为 `~/Library/Application Support/tinyhost/config.json`；Linux 为 `$XDG_CONFIG_HOME/tinyhost/config.json` 或 `~/.config/tinyhost/config.json`。token 优先读 `TINYHOST_TOKEN`，否则读配置目录下 `tinyhost/credentials.json` 对应服务器的条目。服务器地址只接受 http(s) origin，可带末尾 `/`，不能带路径、URL 凭证、query 或 fragment。

`deploy --create` 成功后才写回本地 tinyhost.json；写回失败时发布已经生效，因此仍输出成功 DTO、退出 0，并在 stderr 提示显式传 --app，避免重试生成重复版本。符号链接的打包跳过警告也只进 stderr。stdout 写入失败退出 1，管道读端可能已无法接收错误 JSON。

golden 快照位于 `internal/cli/testdata/golden/`，覆盖所有命令和代表性错误；仅屏蔽随机 ID、时间和本地测试服务器端口，保留字段、null、数组、版本和统计值。更新：`mise exec -- go test ./internal/cli -update`。

## M2 设备授权与 CLI 登录

| 方法 | 相对路径 | 请求 | 成功 / 错误 |
|---|---|---|---|
| POST | `/device/code` | `DeviceCodeRequest`：name 必填，hostname/os/arch/cli_version 可选 | 200 `DeviceCodeResult`；400 bad_request，429 rate_limited |
| POST | `/device/token` | `{ "device_code": "秘密设备码" }` | 200 `DeviceTokenResult`；400 authorization_pending / slow_down / access_denied / expired_token |
| POST | `/logout` | Bearer，无 body | 200 `LogoutResult`：`{"logged_out":true}`；401 unauthorized |

设备码端点不要求 Bearer，不接受 cookie 作为授权。DeviceCodeResult 字段为 device_code、user_code、verification_uri、verification_uri_complete、expires_in（600 秒）、interval（3 秒）。device_code 是秘密，user_code 才可展示。用户在 verification_uri_complete 上批准后轮询，事务创建 client 并消费申请，token 明文仅返回一次。DeviceTokenResult 为 `{token, client: Client, expires_at: integer|null}`。轮询距上次不足 2 秒返回 slow_down，CLI 将间隔加 5 秒。所有 401 hint 为「运行 tinyhost login 重新授权」。

`login [--name 名称] [--wait 60] [--force] --json` 输出 LoginResult：必有 status；可选 name、server、expires_at、user_code、verification_uri_complete。status 为 authorized / already_logged_in / environment / pending。pending 退出 4，其他成功退出 0；错误保持 Error DTO，拒绝和过期退出 1。JSON stdout 始终只有一个文档，授权链接和等待提示走 stderr。

`logout --json` 输出 LogoutResult。whoami 的 JSON 字段保持 M1 契约，文本新增 token 到期时间（或永久）。credentials.json（0600）为 `{ "<server>": {token,client_id,name,expires_at} }`；pending-login.json（0600）为 `{server,device_code,user_code,verification_uri_complete,expires_at,interval?}`。更新文件使用同目录原子替换，保留其他服务器凭证。

## M3 预览与路径警告

| 方法 | 相对路径 | 请求 | 成功 / 错误 |
|---|---|---|---|
| POST | `/previews` | tar.gz；`key` 必填，`ttl` 默认 24h，`new=1` 可选，`source` 可选 | 201 `PreviewResult`；400 invalid_ttl / invalid_preview / bad_bundle，413 too_large |
| GET | `/previews` | 无 | 200 `PreviewsResult`，仅调用者尚未过期的预览 |
| DELETE | `/previews/{id}` | 无；也接受链接中的 token | 200 `DeletePreviewResult`；404 preview_not_found（包括其他 Agent 的预览） |

`ttl` 使用 Go duration，包含端点的范围为 1h–168h，不接受 `30d`。`key` 在每个 client 内唯一，最长 1024 字节；source 最长 4096 字节，仅用于显示。相同 client/key 原子替换内容并重算到期时间，保留 id/url/created_at。`new=1` 忽略 key，随机生成独立 key，旧链接继续保留。不创建 App 或版本，不支持覆盖目录。包规则和 deploy 相同。

`PreviewResult`：

```json
{"id":"p_example","url":"https://th.reorx.com/_p/k7f2m3xq4c5d3h5zabce/","expires_at":1793664000,"reused":false}
```

`PreviewsResult` 为 `{"previews":[Preview,...]}`，空列表为 `[]`；按 updated_at 降序、id 排序。Preview 字段为 id、url、source（CLI 上报来源路径的 basename）、created_at、updated_at、expires_at。内部 local_key、层 ID、client_id 不输出。`DeletePreviewResult` 为 `{"id":"p_example","deleted":true}`，删除立即使链接 404。

预览链接不要求登录。20 位小写 base32 路径 token 含 100 bit 随机性，存储明文以便后台展示；它只授权读取预览，不授权写操作。内容沿用 M1 路径解析、ETag、HEAD 和 Range，使用 `Cache-Control: no-cache`、`X-Robots-Tag: noindex`。到期立即返回 410 和「这个预览已过期」页；GC 将层置 NULL，过期超过七天后删除记录并变为 404。GC 前的保留行也可用相同 key 更新恢复。

CLI 新命令：

| 命令 | 成功 DTO |
|---|---|
| `preview [路径] [--ttl 24h] [--new]` | `PreviewResult` |
| `previews` | `PreviewsResult` |
| `previews rm <链接或 ID>` | `DeletePreviewResult` |

CLI 在配置目录保存 `previews.json`，形状为 `{ "<server>|<绝对路径>": "<local_key>" }`，0600，同目录临时文件原子替换。`--new` 轮换本地 key，后续普通 preview 沿用新链接，旧链接独立过期。key 在上传前保存，失败重试不会重复创建预览。删除链接时校验服务器 origin 和 20 位 token，然后由服务端解析为 ID 并检查创建者；拒绝其他服务器的链接。到期后的保留期内，ID 和链接都可用于删除。

deploy、data push、preview 在打包时检查实际纳入上传的 `.html .htm .css .js .mjs` 文件（每文件不超过 1MiB），忽略远程、协议相对、data URI 和相对地址。警告按文件/行排序，每行一条、每文件最多五条、总共二十条；stderr 在上传前显示文件、行号、原文和相对路径建议，上传照常执行。

`DeployResult` 和 `PreviewResult` 增加可选字段 `warnings?: [{"file":"index.html","line":1,"text":"<script src=\"/app.js\">"}]`，没有警告时省略。仅 CLI 本地生成此字段；HTTP 直传不会执行路径检查。原有 skipped、JSON 输出流和退出码语义保持不变。
