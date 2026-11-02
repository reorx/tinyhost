---
created: 2026-11-02
tags:
  - tinyhost
  - device-flow
  - auth
  - admin-console
---

# tinyhost M2：授权与后台

这是 [tinyhost 总 plan](2026-11-02-tinyhost.md) 的第二期，依赖 [M1 核心发布](2026-11-02-tinyhost-core-publish-spec.md)。

## PRD

### 背景

M1 的 token 要我登上服务器手动签发，和"Agent 自助接入"的目标不符。私有 App 现在也只能显示"需要登录"，没法查看。这一期补上 Agent 自助申请授权、我的后台，以及私有 App 的查看。

### 目标

- 新 Agent 从 `tinyhost login` 到能发布，我只需要在后台点一次批准。
- 我在 `th-admin.reorx.com` 管理所有 App、版本和 Agent。
- 我能打开自己的私有 App，但这不会让任何 App 页面获得管理权限。

### 功能与交互

**首次设置**
- 在服务器上运行 `tinyhost-server passwd`，输入两次密码。没设密码时，后台的所有页面都只显示"请先在服务器上运行 tinyhost-server passwd"。

**Agent 申请授权：`tinyhost login [--name 名称]`**
- 名称默认是 `<用户名>@<主机名>`。
- 输出：
  ```
  请把下面的链接发给用户，请他打开并批准（验证码 WDJB-MJHT，10 分钟内有效）：
    https://th-admin.reorx.com/device?code=WDJB-MJHT
  等待批准（最多 60 秒）…
  ```
- 60 秒内被批准：输出 `✓ 已授权为 <名称>`，凭证保存到本机，之后的命令自动使用。
- 60 秒内没批准：以"等待中"状态退出（退出码 4），提示"用户批准后再运行一次 tinyhost login"。再次运行时继续等待同一个申请，不会生成新验证码。`--wait 120` 可以调整等待时间。
- 被拒绝或已过期：报错并清掉本地的待批准记录；再运行一次就是全新的申请。
- 已经有有效凭证时：提示"已登录为 <名称>"，不发起新申请。`--force` 重新申请。
- 已设置 `TINYHOST_TOKEN` 环境变量时：提示"正在使用环境变量里的 token"，不发起申请。
- `tinyhost logout`：在服务端撤销自己的 token，并删除本地凭证。
- `tinyhost whoami`：名称、服务器、token 到期时间。

**我批准授权**
- 打开授权链接。没登录时先到登录页，登录后回到批准页。
- 批准页显示：Agent 自报的名称、主机名、系统和架构、CLI 版本、来源 IP、申请时间、验证码。
- 可以修改名称，并选择有效期：1 天 / 30 天（默认）/ 永久。
- 两个按钮："批准"和"拒绝"。操作后显示结果，Agent 那边下一次轮询就能拿到结果。
- 验证码不存在、已过期或已处理过：显示对应的提示。
- 后台首页顶部会列出所有待批准的申请，可以直接点进去批准。

**我的后台登录**
- 只在 `th-admin.reorx.com/login` 输入密码。登录状态在这个浏览器保持 30 天。
- 连续输错会被暂时限制。
- "退出"只退出当前浏览器。"退出所有会话"会让所有浏览器的后台会话和私有 App 查看会话一起失效（手机丢了时用）。

**后台首页：App 列表**
- 每行显示 slug、标题、可见性、当前版本、创建者、最近更新时间，点击进入详情。
- "新建 App"：填 slug、标题、可见性，建出一个空 App，供 Agent 之后往里推。

**App 详情**
- 主入口链接、可见性（可切换公开 / 私有）、创建者、创建时间。
- 改 slug：改完后旧地址（包括旧的版本地址）永久跳转到新地址；旧 slug 不能再分配给别的 App。
- 版本列表：版本号、类型、发布者、说明、时间。每个版本都有"打开"和"设为主入口"；非主入口的版本还有"删除"（删除后链接 404，内容被清理，用来处理"Agent 把不该公开的东西发上去了"这种情况）。
- 覆盖目录列表：目录、推送者、时间，可以"撤掉"。
- App 包声明的数据目录说明。
- "删除 App"：进回收站。
- 所有删除类操作都会先弹出确认。

**回收站**
- 列出已删除的 App 和剩余天数，可以"恢复"或"立即清理"。

**Agent 管理**
- 列表：名称、批准时间、到期时间、最近使用时间和 IP、状态（有效 / 已过期 / 已撤销）。
- "撤销"：这个 Agent 的下一个请求就会失败，CLI 提示重新 `tinyhost login`。
- "手动签发 token"：填名称和有效期，token 只显示一次，附说明"配进 Agent 环境的 `TINYHOST_TOKEN` 环境变量"。用于每次都是全新容器的云端环境。

**操作记录**
- 最近 200 条：时间、操作者、动作、App、摘要，可以按 App 筛选。App 详情页也显示这个 App 最近的记录。

**查看私有 App**
- 在没登录的浏览器打开私有 App：先跳到 `th-admin.reorx.com` 登录（已登录则直接放行），然后自动跳回原地址看到内容。
- 之后 30 天，这个浏览器打开任何私有 App 都不用再登录。
- 这个查看状态只能用来看内容，不能用来做任何管理操作。

### 不做的事

- 第二个人类账号、按 App 授权给别人看、带密码的分享链接。
- Passkey、两步验证。
- Agent 自己改可见性或改 slug（只有 Owner 能做）。
- 后台的预览管理（M3）、通知（M3）。

### 验收标准

1. 新 Agent 运行 `tinyhost login`，我打开链接、登录、批准后，Agent 不用其他操作就能 `deploy`。
2. 我 60 秒内没批准时，`login` 以"等待中"退出；我批准后 Agent 再运行一次 `login` 就拿到授权，验证码不变。
3. 拒绝后 Agent 收到"已拒绝"；过期后收到"已过期"。
4. 撤销某个 Agent 后，它的下一个命令失败，并提示重新登录。
5. 选择"1 天"有效期的 Agent，一天后无法再发布。
6. 手动签发的 token 通过 `TINYHOST_TOKEN` 可以直接使用。
7. 在后台把 App 改为私有后，未登录的浏览器打开链接会跳到登录页，登录后回到原页面并看到内容。
8. 一个公开 App 的页面里放一段脚本，尝试调用批准授权、删除 App、改可见性：在我已经登录后台和私有查看的浏览器里打开这个页面，所有尝试都失败。
9. 改 slug 后，旧的主入口和旧的版本地址都跳转到新地址；用旧 slug `--create` 会失败。
10. 删除某个版本后，它的链接 404；主入口所在的版本不能删除。
11. 回收站里的 App 恢复后，链接和所有版本都恢复原样。
12. "退出所有会话"之后，所有浏览器都需要重新登录才能进后台和看私有 App。
13. 操作记录里能看到 M1 的发布、推数据、回滚、删除，以及本期的批准、撤销、改可见性。

## Tech Design

### 现状

M1 完成后：`clients` 表和 Bearer 鉴权已经有了，`tinyhost-server token create` 直接写 `clients`；apps 域名对私有 App 返回 401 页；`events` 已在记录 M1 的动作；admin 域名还没有路由。

### 方案

- **设备授权**：按 RFC 8628 实现，端点放在 apps 域名的 Agent API 下（Agent 只需要访问一个域名）。批准页在 admin 域名。批准时不生成 token，只把申请标记为已批准；Agent 下一次轮询时，在同一个事务里创建 client、生成 token，并把明文只返回这一次。服务端从不保存 token 明文。
- **后台**：服务端渲染的表单页面，cookie 会话。安全要点见总 plan 的关键决策 2：host-only 的 `__Host-` cookie、所有 POST 校验 Origin、页面禁止被嵌入、COOP 隔离。
- **私有查看**：apps 域名上没有密码框。没登录时跳到 admin 的 `/view-auth`，由它签发 60 秒一次性票据，再跳回 apps 域名的 `/_/auth` 换成查看会话 cookie。

### 数据变动

新增迁移：

```sql
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- owner_password_hash：bcrypt

CREATE TABLE device_requests (
  id                TEXT PRIMARY KEY,          -- "d_" + 随机
  device_code_hash  TEXT NOT NULL UNIQUE,
  user_code         TEXT NOT NULL UNIQUE,      -- 8 位，存不带连字符的形式
  name              TEXT NOT NULL,             -- Agent 自报的名称
  meta              TEXT NOT NULL,             -- json：hostname/os/arch/cli_version/request_ip
  status            TEXT NOT NULL CHECK (status IN ('pending','approved','denied','consumed')),
  approved_name     TEXT,
  token_ttl         INTEGER,                   -- 秒，NULL = 永久
  client_id         TEXT,                      -- consumed 时写入
  created_at        INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL,          -- created_at + 600
  last_polled_at    INTEGER
);

CREATE TABLE sessions (
  id_hash     TEXT PRIMARY KEY,                -- cookie 值的 sha256
  kind        TEXT NOT NULL CHECK (kind IN ('admin','viewer')),
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL,                -- 固定 30 天，不续期
  ip          TEXT,
  user_agent  TEXT
);

CREATE TABLE view_tickets (
  ticket_hash  TEXT PRIMARY KEY,
  next_path    TEXT NOT NULL,
  expires_at   INTEGER NOT NULL,               -- 60 秒
  used_at      INTEGER
);

CREATE TABLE slug_redirects (
  old_slug    TEXT PRIMARY KEY,
  app_id      TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL
);
```

- `clients.meta` 开始写入申请时的元数据。
- slug 是否被占用的判断改为：`apps.slug` 或 `slug_redirects.old_slug` 中存在即占用。
- GC 增加：删除过期超过 1 天的 `device_requests`、过期的 `sessions` 和 `view_tickets`。

### 逻辑变动

**Agent API（apps 域名）新增端点**

| 方法与路径 | 鉴权 | 说明 |
|---|---|---|
| `POST /_/api/v1/device/code` | 无 | body `{name, hostname, os, arch, cli_version}` → `{device_code, user_code, verification_uri, verification_uri_complete, expires_in: 600, interval: 3}`；每个 IP 每分钟最多 10 次 |
| `POST /_/api/v1/device/token` | 无 | body `{device_code}`。pending → 400 `authorization_pending`；距上次轮询不到 2 秒 → 400 `slow_down`；denied → 400 `access_denied`；过期 → 400 `expired_token`；approved → 200 `{token, client: {id, name}, expires_at}`，状态改为 consumed |
| `POST /_/api/v1/logout` | Bearer | 撤销当前 token |

- user_code 字母表 `BCDFGHJKLMNPQRSTVWXZ`，8 位，显示为 `XXXX-XXXX`。输入时去掉连字符和空格并转大写。
- verification URI 用 `--admin-url` 拼接：`<admin>/device?code=XXXX-XXXX`。
- 返回 401 时 hint 统一为"运行 tinyhost login 重新授权"。

**admin 域名路由**（`internal/server/admin.go`，模板放在 `internal/server/views/`）

| 路由 | 说明 |
|---|---|
| `GET/POST /login`、`POST /logout`、`POST /logout-all` | 登录 / 退出 / 全部会话失效（清空 sessions 表） |
| `GET /` | App 列表 + 待批准申请；`POST /apps` 新建空 App |
| `GET /apps/{slug}` | 详情 |
| `POST /apps/{slug}/visibility`、`/rename`、`/promote`、`/versions/{n}/delete`、`/data/remove`、`/delete` | 写操作 |
| `GET /trash`；`POST /trash/{id}/restore`、`/trash/{id}/purge` | 回收站 |
| `GET /device?code=`；`POST /device/{id}/approve`、`/device/{id}/deny` | 批准授权 |
| `GET /agents`；`POST /agents`（手动签发）、`/agents/{id}/revoke` | Agent 管理 |
| `GET /events?app=` | 操作记录 |
| `GET /view-auth?next=` | 签发查看票据并跳回 apps 域名 |
| `GET /static/*` | CSS 和一个很小的 `admin.js`（删除确认） |

- 中间件：
  - 除 `/login` 和 `/static/` 外都要求有效的 admin 会话；未登录的 GET 跳到 `/login?next=<原路径>`。
  - 所有 POST 都要求 `Origin` 等于 admin 源；没有 Origin 时要求 `Sec-Fetch-Site: same-origin`；都不满足就 403。
  - 统一响应头：`Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; form-action 'self'`、`Cross-Origin-Opener-Policy: same-origin`、`Referrer-Policy: same-origin`、`Cache-Control: no-store`。
- 会话 cookie：`__Host-th_admin`，`HttpOnly; Secure; SameSite=Lax; Path=/`，30 天。用 Lax 而不用 Strict：从 ntfy 或 Bark 点开链接属于外部导航，Strict 不会带 cookie，会误显示为未登录。CSRF 由 Origin 校验负责。
- 登录：bcrypt（`golang.org/x/crypto/bcrypt`，cost 12）。限速：每个 IP 每分钟 5 次，全局每分钟 30 次，用内存令牌桶。
- 改 slug：在一个事务里改 `apps.slug`，插入 `slug_redirects`；如果旧 slug 本身已有跳转记录，把它们一并指向这个 App（`app_id` 不变，自然成立）。
- 删除版本：不允许删除 `current_version`；删除行后由 GC 清理层和文件块。
- 恢复：把 `deleted_at` 置为 NULL。
- 立即清理：直接删除 App 行。
- 手动签发：复用 M1 的 `CreateClient`。
- 所有写操作都调用 `AddEvent`，actor 为 `owner`。

**私有查看**
- apps 域名的内容托管遇到私有 App 时，检查 `__Host-th_view` 会话（kind 为 viewer、未过期）：
  - 有效：照常返回，`Cache-Control` 改为 `private, no-cache`（版本链接也一样），防止 CDN 缓存。
  - 无效且是页面导航（`Sec-Fetch-Mode: navigate`，或 `Accept` 含 `text/html`）：302 到 `<admin>/view-auth?next=<原路径和查询>`。
  - 无效且不是导航：401。
- admin `GET /view-auth`：
  - `next` 必须以 `/` 开头、不以 `//` 开头、不含 `\`，否则 400。
  - 需要 admin 会话；没有就先走登录。
  - 生成票据（32 字节随机数，存 hash，60 秒有效），302 到 `<apps>/_/auth?ticket=…&next=…`。
- apps 域名 `GET /_/auth`：
  - 校验票据未使用、未过期，并标记为已使用。
  - 创建 viewer 会话，种 `__Host-th_view`（`HttpOnly; Secure; SameSite=Lax; Path=/`，30 天）。
  - 302 到 `next`（同样校验）。
- 服务端没有任何代码路径用 viewer 会话做读取内容以外的事。Agent API 的中间件只看 Bearer。

**`--insecure-cookies`（仅开发用）**
- 去掉 `Secure` 和 `__Host-` 前缀，cookie 名变成 `th_admin`、`th_view`，便于在 `http://localhost:3471` 和 `http://admin.localhost:3471` 上手动测试。
- 启动时打印警告。

**`cmd/tinyhost-server`**
- 新增 `passwd`：终端里不回显地读两次密码（`golang.org/x/term`）；非终端时从 stdin 读一行。
- `serve` 必须提供 `--admin-url`，且 host 和 apps 不同。

**CLI**
- 新增 `login`、`logout`；`whoami` 增加到期时间。
- 文件放在 `os.UserConfigDir()/tinyhost/`：
  - `credentials.json`（0600）：`{ "<server>": {token, client_id, name, expires_at} }`，按服务器地址区分，可以同时登录多个服务器。
  - `pending-login.json`：`{server, device_code, user_code, verification_uri_complete, expires_at}`。
- 轮询按 `interval` 进行，遇到 `slow_down` 时间隔加 5 秒。到 `--wait` 时退出码 4。

### 实施步骤

- [ ] 迁移和 store 函数：settings、device_requests、sessions、view_tickets、slug_redirects，先写测试
- [ ] 把验收标准 1–13 写成行为测试（此时全部失败）
- [ ] 设备授权端点 + 批准页
- [ ] admin 登录、会话、Origin 校验中间件、响应头
- [ ] 后台页面：App 列表与新建、详情（可见性、改名、版本、覆盖目录、删除）、回收站、Agent、操作记录
- [ ] 私有查看：view-auth、`/_/auth`、内容托管的会话检查
- [ ] `tinyhost-server passwd`；`serve` 校验 `--admin-url`
- [ ] CLI `login` / `logout` / `whoami` 及 golden 快照
- [ ] 更新 `docs/json-output.md`、`AGENTS.md`、`skill/SKILL.md`（登录一节）
- [ ] 本地手动验收：用 `localhost` / `admin.localhost` 走一遍授权、私有查看、改名、回收站，截图存到 `tmp/<日期>-m2-auth-console/`

### 测试与验证

| 验收 | 测试 |
|---|---|
| 1 | `TestDeviceFlowApproveThenTokenWorks`（服务端）+ CLI 进程内 `login` 测试（用测试钩子在轮询期间批准） |
| 2 | `TestLoginTimesOutAsPendingAndResumesSameCode` |
| 3 | `TestDeniedAndExpiredDeviceRequests` |
| 4 | `TestRevokedClientGets401WithLoginHint` |
| 5 | `TestTokenTTLIsEnforced`（注入时钟） |
| 6 | `TestManuallyIssuedTokenWorksFromEnv` |
| 7 | `TestPrivateAppRedirectsThroughAdminAndBack`：跟随两次跳转、带上 cookie，最后 200 |
| 8 | `TestAppPageCannotPerformControlledActions`：模拟浏览器从 apps 源发请求——带 viewer 和 admin 两个 cookie、`Origin: https://th.reorx.com`、`Sec-Fetch-Site: same-site`，对 admin 的批准、删除、改可见性逐一 POST，都必须 403；对 Agent API 的写操作不带 Bearer，都必须 401 |
| 9 | `TestRenameRedirectsOldPathsAndReservesOldSlug` |
| 10 | `TestDeleteVersionAndCurrentVersionIsProtected` |
| 11 | `TestRestoreFromTrash` |
| 12 | `TestLogoutAllInvalidatesEverySession` |
| 13 | `TestEventsRecordedForOwnerAndAgentActions` |

补充：`TestAdminPostWithoutOriginRejected`、`TestAdminHeadersDenyFraming`、`TestViewAuthRejectsOffsiteNext`、`TestLoginRateLimited`、`TestDeviceCodeRateLimited`。

手动验收里专门做一次真实浏览器测试：发布一个带恶意脚本的公开测试 App，脚本里用 fetch 和表单 POST 调后台和 API；在已登录的浏览器打开它，确认后台数据没有变化。

### 风险与待定

- **Origin 校验的兼容性**：现代浏览器的 POST 都会带 Origin。少数隐私插件会去掉它；这种情况会退到 `Sec-Fetch-Site`，两者都没有时请求会失败。被误伤时再考虑在表单里加 CSRF token。
- **app 页面画假登录框**（总 plan 里已接受的风险）：登录页文案写明"tinyhost 只会在 th-admin.reorx.com 要求输入密码"。
- **bcrypt 的 cost 12** 在服务器上大约 250ms 一次，配合限速，可以接受。
