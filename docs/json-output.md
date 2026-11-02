# tinyhost JSON 契约

供 HTTP API 调用者和 CLI 实现者查阅。Go 类型见 `internal/api/api.go`。从 M1 起字段只增不改；新增字段必须可选。时间均为 Unix 秒，版本为正整数；`current_version: null` 表示尚无主入口。数组为空时输出 `[]`。

## HTTP API

基地址为 apps origin 下的 `/_/api/v1`。所有端点仅接受 `Authorization: Bearer <token>`；cookie 不参与鉴权，不开放 CORS。撤销或到期的 token 返回 401。响应 JSON 使用 UTF-8，API 响应为 `Cache-Control: no-store`，不返回 token/hash。GET 同时支持 HEAD。

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

由 th-m1-cli 任务补充。HTTP DTO 已冻结，CLI 应直接使用这些结构。
