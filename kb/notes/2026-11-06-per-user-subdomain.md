---
created: 2026-11-06
tags:
  - security
  - multi-tenant
  - subdomain
  - saas
  - browser-isolation
  - research
---

# 调研：给每个用户分配独立子域

这篇写给准备把 tinyhost 做成多用户 SaaS 的人。它回答三个问题：每个用户一个子域（`<user>.tinyhost.space`）在安全上具体换来什么，还有哪些别的好处，在现有系统上要付出什么代价、会遇到哪些障碍。

## 背景

- 现在所有 App 都在一个 origin 下，路径是 `tinyhost.space/<slug>/`。SECURITY.md 的「All Apps on an instance share the apps origin」一段已经承认 App 之间不隔离：浏览器里有查看会话时，一个 App 的脚本能读取其他私有 App 并把内容发出去。单 Owner 自部署时，所有 Agent 都是 Owner 自己的，这个代价可以接受。
- 做 SaaS 以后，这条边界会变成用户之间的边界：用户 A 的公开 App 被用户 B 打开时，A 的脚本能带着 B 的查看 cookie 读走 B 的私有 App。这是跨租户漏洞，做 SaaS 之前必须解决。
- 讨论过另一条路：私有内容做端到端加密，运营者也读不到。结论是代价太大，放弃。那次讨论的另一个结论是：不管加不加密，origin 隔离都是 SaaS 的前提，所以单独拿出来调研。

## 方案形态

| 主机 | 提供什么 | 鉴权 |
|---|---|---|
| `tinyhost.space`（apex，服务主机） | Agent API `/_/api/v1/`、`/skill.md`、`/install.sh`、`/_/dl/`、根路径说明页、`/_/health`、旧链接的 301 | Bearer，不用 cookie |
| `<user>.tinyhost.space`（内容主机） | 该用户的 `/<slug>/`、`/<slug>@vN/`、`/_p/<token>/`、`/_/auth`、`/_/static/` | `__Host-th_view`，只读 |
| 后台域名 | 不变，每个用户各自登录 | `__Host-th_admin` + Origin 校验 |

内容主机上的路径结构不变，App 照旧只用相对路径，「用户内容不在 `/` 和 `/_/` 下」这条不变量也照旧成立。

另外把 `tinyhost.space` 提交到 Public Suffix List（PSL）的 PRIVATE 段，让每个 `<user>.tinyhost.space` 在浏览器眼里成为独立的 site（相当于独立注册的域名）。PSL 能不能提交上去是另一回事，见下文「PSL」一节。

## 安全收益

收益分两层。第一层只要换成子域就有，靠的是浏览器的同源策略，保护力度最大。第二层要等 `tinyhost.space` 进了 PSL、并且浏览器带上新列表之后才有，属于把 site 级的边界补齐。

### 第一层：只靠子域（origin 隔离）

1. **跨用户读取私有内容被挡住。** 现在 A 的脚本用同源 `fetch('/b-secret/')` 就能读到 B 的私有 App。换成子域后，这是跨 origin 请求，服务器不发 CORS 头，浏览器不让脚本读响应。
2. **不能用 iframe 读 DOM。** 现在 A 可以 iframe 嵌入 B 的私有 App 并读 `contentDocument`；换成子域后，同源策略禁止这样做。
3. **浏览器存储按用户隔离。** localStorage、IndexedDB、Cache Storage、JS 设置的 cookie、Service Worker 注册都按 origin 划分。Agent 做的小工具常把数据存在 localStorage（待办、笔记，甚至用户填进去的 API key）。现在全站任何 App 都能读到这些数据，换成子域后只有同一用户的 App 能读。
4. **浏览器权限按用户隔离。** 摄像头、麦克风、定位、通知、剪贴板这些授权是按 origin 记的。现在给一个 App 授权，等于给了 `tinyhost.space` 上所有用户的所有 App。
5. **地址栏能看出是谁的内容。** 现在所有用户的页面地址栏都显示 `tinyhost.space`，假登录框分不出真假。换成子域后，访客能看到 `alice.tinyhost.space`，B 的页面没法冒充 A 的。
6. **查看会话天然按用户分开。** `__Host-th_view` 是 host-only cookie，每个内容主机各有一份，B 的主机上不存在 A 的会话。
7. **可以按主机封禁。** 很多拦截名单只能按主机名生效，例如 DNS 过滤（企业 DNS、NextDNS、Pi-hole）、企业代理、广告和恶意域名列表。现在一个用户发钓鱼页，这些名单只能把整个 `tinyhost.space` 拉黑，所有用户一起被封。换成子域后可以只封那一个用户。

### 第二层：PSL 生效以后才有

PSL 生效之前，`a.tinyhost.space` 和 `b.tinyhost.space` 是不同 origin，但属于同一个 site。下面这几条要等 PSL 生效才有：

1. **SameSite 开始生效。** 查看 cookie 是 `SameSite=Lax`（`internal/server/admin.go:63`）。同 site 时，A 页面发往 B 主机的子资源请求仍然会带上 B 的 cookie。脚本读不到响应，但能通过加载成功或失败、耗时这类侧信道（XS-Leaks）探测 B 有没有某个私有 App、某个路径是否存在。跨 site 以后，cookie 根本不随这类请求发出。
2. **防 cookie 注入和 cookie 轰炸。** 同 site 时，A 的脚本能写 `Domain=tinyhost.space` 的 cookie，发往所有用户的主机和 apex。`__Host-` 前缀保证我们自己的会话 cookie 不会被覆盖，但 A 可以写满大量大 cookie，让受害者的浏览器访问任何 `*.tinyhost.space` 都因请求头过大失败。PSL 生效后，浏览器会拒绝 `Domain=<公共后缀>` 的 cookie。
3. **进程隔离（防 Spectre 类侧信道）。** Chrome 的 Site Isolation 按 site 分配渲染进程。同 site 的不同用户可能共享进程，跨 site 的一定分开。
4. **信誉系统把用户当成不同域名。** Let's Encrypt 的限速、各种按「注册域名」归并的信誉系统，都用 PSL 计算注册域名。进了 PSL，一个用户惹的麻烦不会累积到 `tinyhost.space` 头上。【推测】Google Safe Browsing 对 PSL 上的共享托管域名，实践上也倾向于按子域标记，而不是整域拉黑。
5. 【推测】**密码管理器不串号。** 不少密码管理器按注册域名匹配已存的密码，PSL 生效前，A 子域上存的密码可能会被推荐到 B 的子域。

### PSL 不生效时在服务端补上的部分

PSL 很可能短期提交不上（见下文），第二层的第 1 条可以用 **Fetch Metadata 资源隔离策略** 在服务端模拟出来。只针对私有内容生效：

- 放行 `Sec-Fetch-Site` 为 `same-origin` 或 `none` 的请求、`Sec-Fetch-Mode: navigate` 的顶层导航，以及不带 `Sec-Fetch-*` 头的老浏览器。
- 拒绝 `same-site` 和 `cross-site` 的子资源请求。

这样别的用户的页面连带凭证的探测请求都发不进来。正常使用不受影响：App 加载自己的资源是 `same-origin`，从后台跳过来、`/_/auth` 回跳都是导航。公开内容不加这条，别人引用公开 App 的资源是正当需求。

第 3 条可以给内容主机发 `Origin-Agent-Cluster: ?1`，提示浏览器按 origin 隔离。【推测】Chrome 会把它当作进程隔离的提示，但不保证一定分进程。

### 解决不了的问题

- **同一用户的多个 App 之间仍然同源。** 用户自己的 Agent 产出互相信任，和现在单 Owner 的模型一样。要隔离 App 之间，得用每 App 一个子域，见下文对比。
- **运营者能看到内容。** 这是端到端加密要解决的问题，已经放弃。
- **用户的 App 骗自己的访客。** 用户的 App 仍然可以对自己的访客钓鱼。子域只能让访客看清这是谁的页面。
- **同一用户的预览和私有 App 同源。** 预览 `/_p/<token>/` 放在用户自己的主机上，和该用户的私有 App 同源。Owner 登录状态下打开自己的预览，预览脚本能读到 Owner 的私有 App，和现在一样。

## 安全以外的好处

- **自定义域名有了自然的延伸。** 有了「Host → 用户」的解析之后，自定义域名只是多一条映射，例如 `docs.acme.com` CNAME 到 `acme.tinyhost.space`，证书用 Caddy 的 on-demand TLS 加一个 `ask` 校验接口来签。这适合做成 SaaS 的付费功能。
- **在代理层就能按用户运维。** 限速、流量计量、封禁一个用户，都能在反向代理层按 Host 完成，不用改应用代码。请求日志已经记录了 `host`（`internal/server/server.go:156`），用户归属是现成的。
- **可以横向扩展。** 以后可以把不同用户分到不同的后端实例，DNS 或代理按 Host 分流就行。
- **内容主机可以收紧。** 内容主机只接收 GET 和 HEAD，上传都走 apex 的 API 或后台，所以代理层可以给通配主机设很小的请求体上限。
- **地址更好认。** `alice.tinyhost.space/report/` 一眼能看出是谁发布的。

## 代价与实现障碍

### 前提：多租户本身是大头

现在系统里没有「用户」这个概念：后台只有一个 Owner 密码，clients、settings、events、previews 都是全局的。子域只是多租户在浏览器这一侧的边界，账号、注册、数据归属、配额、计费才是主要工作量。单用户时换成子域没有任何收益，所以子域不应该单独先上，要和多租户一起上。下面各条只计算子域本身的代价。

有两步准备是无害的，可以在单 Owner 阶段先做，见文末的「落地顺序」。

### 1. `AppsURL` 一词两义，要拆开

`--apps-url` 现在同时表示两件事：服务地址（CLI、install.sh、skill.md 指向它）和内容地址（App 链接拼在它后面）。拆开后，服务地址仍然是 apex，内容地址按用户生成：

| 含义 | 调用处 |
|---|---|
| 服务地址（不变） | 根路径说明页、`/skill.md`、`/install.sh`（`internal/server/distribution.go:47/52/57`），后台 quickstart（`internal/server/console.go:365`），Agent API 和 `/_/dl/` 的路由 |
| 内容地址（改成按用户生成） | `mainURL`（`internal/server/server.go:90`），`previewURL`（`internal/server/previews.go:14`），通知里的链接（`cmd/tinyhost-server/notify.go:54`），`/view-auth` 跳转（`internal/server/admin.go:318`），`/login` 的 CSP 例外（`internal/server/admin.go:124`） |

好消息是：CLI 的 `--server` / `config.json`、install.sh、SKILL.md 里的 `{{SERVER}}` 都指向服务地址，**不用改**。App 链接由服务端返回，CLI 只负责打印，所以已经装好的 CLI 不用升级。`internal/api` 的 DTO 字段不变，只是 URL 值里的主机变了，`--json` 的 golden 快照要更新。

### 2. Host 分流

`internal/server/server.go:164` 起的分流现在是精确匹配：apps 主机走内容，admin 主机走后台，其他返回 421。要改成三类：

- apex：API、分发、旧链接 301。
- `*.<内容域>`：取第一段 label，解析成用户；用户不存在返回 404。
- admin：不变。

### 3. 新的头号不变量：Host 决定用户，所有内容查找都要按用户过滤

这是实现里最容易出事的地方，漏掉一处，隔离就形同虚设：

- `SlugOwner`（`internal/store/auth.go:147`）现在按 slug 和历史跳转做全局查找。如果不加用户过滤，`bob.tinyhost.space/<alice 的 slug>/` 会在 bob 的 origin 下执行 alice 的内容，反过来也一样，等于自己把隔离拆掉。
- `/_p/<token>/` 同理。预览 token 是全局的，必须校验预览所属的用户和 Host 对应的用户一致。
- `view_tickets` 只存了 `next_path`，`sessions` 也不记属于谁（`internal/store/auth_schema.sql:7`、`:11`）。票据和会话都要绑定用户，兑换和校验时和 Host 比对。cookie 本身是 host-only，浏览器不会把它发到别的主机，所以这一条属于纵深防御。
- Agent API 按 slug 查 App 时，也要限定在 token 所属用户的范围内。

这一组跨用户的行为测试要作为上线门槛：

- 在 B 的主机上请求 A 的 slug，返回 404。
- 在 B 的主机上请求 A 的预览 token，返回 404。
- A 的票据拿到 B 的主机上兑换，被拒绝。
- A 的会话 cookie 值放到 B 的主机上，无效。

### 4. slug 命名空间

`apps.slug` 现在是全局 UNIQUE，`slug_redirects.old_slug` 是主键，还有一个阻止复用旧 slug 的触发器（`internal/store/auth_schema.sql:12`）。有子域以后，用户会期望 slug 在自己名下独立，alice 和 bob 都能有 `report`。改成按用户唯一需要一次迁移（`user_version=4`），`CreateApp`、`RenameApp`、`SlugOwner` 和触发器都要带上用户。保留全局唯一可以少改一些，但用户体验差，而且第 3 条的用户过滤仍然一处都不能少。

### 5. 用户名规则

- 可以直接复用 slug 的规则：`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`，不允许 `--`（`internal/slug`）。这个规则本身就是合法的 DNS label；禁止 `--` 又顺带排除了 `xn--` 开头的 punycode，杜绝了同形字仿冒。
- 保留名要比 slug 的列表更长：`www`、`api`、`admin`、`mail`、`smtp`、`ftp`、`cdn`、`status`、`docs`、`blog`、`help`、`support`、`get`、`dl`、`app`、`console`、`dashboard`、`account`、`billing`、`security`、`abuse`、`postmaster`、`hostmaster`、`webmaster`、`ns1`、`ns2` 等。仿冒知名品牌的名字（paypal、google 之类）还需要注册审核或举报机制。
- **长度上限建议定在 21 左右。** slug 最长 40，`<slug>--<user>` 最长 40 + 2 + 21 = 63，正好是一个 DNS label 的上限，以后想做每 App 一个子域时不用改名。
- **改名和注销的代价。** 改名会让该用户所有已分享的链接失效；旧名如果被别人注册，等于接管了这些旧链接。所以要么不允许改名，要么像 slug 一样加一张跳转表，并永久保留旧名。注销的账号名也要永久保留。

### 6. 登录跳转和 CSP

- `/view-auth` 要跳到票据所属用户的主机。`safeNext` 只接受路径的做法不变，主机由票据决定。
- `/login` 的 CSP 现在额外放行 `AppsURL` 这个 origin（Chrome 会把登录后的整条 302 链都算作表单提交，见 `kb/sessions/2026-11-02-th-m2-fix2.md`）。渲染登录页时还不知道是哪个用户，所以只能放行 `https://*.tinyhost.space`。放宽的只是「表单提交后能导航到哪些内容主机」，可以接受。
- Owner 第一次访问自己的主机时会跳一次后台，和现在一样。

### 7. TLS 和 DNS

- DNS 加一条通配记录 `*.tinyhost.space`，apex 保留。
- **推荐用通配证书**（`tinyhost.space` 加 `*.tinyhost.space`）。通配证书只能用 DNS-01 签发，带来两个代价：
  - Caddy 要用带 DNS 服务商插件的定制构建。
  - 服务器上要放能改 DNS 的 API 凭据。缓解办法是只给最小权限的 token，或者把 `_acme-challenge` 用 CNAME 委派到一个单独的 zone（acme-dns 的做法）。
- 另一种做法是 on-demand TLS：每个主机单独走 HTTP-01，tinyhost 提供一个 `ask` 接口校验主机名。它不需要 DNS 凭据，但有三个问题：
  - 每个用户名都会出现在证书透明度日志里，任何人都能在 crt.sh 上列出全部用户。
  - 每个主机第一次访问要等签证书。
  - PSL 生效前，所有用户共用 `tinyhost.space` 在 Let's Encrypt 的限速额度。

  所以 on-demand TLS 留给自定义域名用。
- 通配证书只覆盖一层。如果以后做每 App 一个子域，只能用单层的 `<slug>--<user>`，不能用 `<slug>.<user>`。
- 具体的服务商、Caddy 模板和凭据写在 deploy 仓库，不写在这里。

### 8. PSL

按 PSL 官方 Guidelines（2025-05 版）核对的要点：

- 提交方式是向 `publicsuffix/list` 提 PR，把条目加进 PRIVATE 段；同时加 DNS TXT 记录 `_psl.tinyhost.space`，内容是 PR 链接。合并后这条记录要一直保留，缺失会被当作移除的依据。
- **域名的注册到期日必须在提交日两年以后**，并且要承诺注册期始终保持一年以上。`.space` 域名需要提前续费到位。
- 接受的理由是「把子域分发给互不信任的多方」，tinyhost SaaS 正好符合。**只为绕开 Let's Encrypt 限速的提交会被拒绝。**
- **规模门槛**：Guidelines 写明，用户规模不到数千的项目、以及 beta、测试、实验性质的申请，大概率会被婉拒。SaaS 起步阶段很可能提交不上。
- 审核没有时限承诺。合并后要等浏览器和操作系统随版本更新列表，没有加速办法，到达大部分用户要以月计。修改和删除条目比新增更难，基本可以视为不可逆。
- 【推断】`tinyhost.space` 成为公共后缀以后，apex 上不能再设置作用于子域的 cookie。apex 只提供用 Bearer 鉴权的 API 和无状态的分发，正好不需要 cookie。以后运营者自己在 `tinyhost.space` 下开的子域（比如状态页）也各自成为独立的 site，没有坏处。

结论：**设计不能依赖 PSL**。上线时靠第一层的 origin 隔离，再加上 Fetch Metadata 策略兜住 SameSite 那一块。等用户规模够了再提交 PSL，拿到剩下的 site 级收益。

### 9. 旧链接迁移

- 已经分享出去的 `tinyhost.space/<slug>/`、`@vN`、`/_p/<token>/`，由 apex 301 到原 Owner 的主机，长期保留，成本很低。新用户的 App 永远不会出现在 apex 路径下。
- `@vN` 在 apex 上曾经带着 `immutable` 缓存头返回，浏览器里已有的缓存不受影响，301 只作用于新请求。
- SKILL.md 里写的 `/slug/`、`/slug@vN/`、`/_p/token/` 路径语义不变，只需补一句：这些路径挂在用户自己的主机下。

### 10. 自部署兼容

自部署是单 Owner，不应该要求部署者配置通配 DNS 和通配证书。做法是让「Host → 用户」的解析器支持两种模式：

- 单主机模式：`--apps-url` 的主机对应唯一的用户，行为和现在完全一样。README 的自部署步骤和 `accept-selfhost.yml` 都不用改。
- 通配模式：取第一段 label 作为用户名，供 SaaS 使用。

两种模式意味着 Host 分流相关的测试要跑两遍。要把分支收敛在解析器这一处，别的代码只认「当前用户」。

### 11. 本地开发和测试

- 浏览器把 `*.localhost` 解析到本机，现在的 `admin.localhost:3471` 已经在用这一点，所以本地可以用 `alice.localhost:3471`。
- 测试本来就是直接设置 Host 头，只需加一套双用户的测试夹具。

### 12. 隐私上的小代价

- URL 里会带上用户名，想匿名发布的用户会在意。
- DNS 查询和 TLS SNI 会暴露访问的主机名。网络上的观察者（公司防火墙、ISP）原来只能看到「访问了 tinyhost.space」，现在能看到访问的是哪个用户。路径仍然只在 HTTPS 内部。

## 和「每个 App 一个子域」的对比

| | 每用户一个子域 | 每 App 一个子域（`<slug>--<user>.tinyhost.space`） |
|---|---|---|
| 隔离范围 | 用户之间 | 所有 App 之间，连同一用户的 Agent 产出也互相隔离，补上 SECURITY.md 里承认的那个缺口 |
| 路径结构 | `/<slug>/` 不变 | 内容挪到 origin 根路径 `/`。`/_/` 保留前缀会和用户内容抢路径，「用户内容不在 `/` 下」的不变量要重新论证 |
| `@vN`、预览 | 不变 | 要重新设计主机或路径形态 |
| Owner 查看 | 每个用户主机跳一次后台 | 每个 App 跳一次（流程已有，体验上只是多跳几次） |
| 对自部署的意义 | 无（单 Owner） | 有，但要求自部署者也配置通配 DNS 和证书 |
| 改动量 | 中 | 大 |

建议先做每用户子域。用户名长度上限和禁止 `--` 这两条规则，给以后做每 App 子域留了余地。

## 落地顺序

1. **准备阶段**（单 Owner 阶段就能做，没有行为变化）：
   - 把 `AppsURL` 拆成服务 URL 和按用户生成的内容 URL 两个构造函数。
   - 引入「Host → 用户」解析器，单主机模式下只有一个用户。
   - 先写好跨主机行为测试的骨架。
2. **和多租户一起上线**：
   - 通配模式。
   - 所有内容查找按用户过滤。
   - 票据和会话绑定用户。
   - slug 按用户唯一。
   - 用户名规则和保留名。
   - 私有内容的 Fetch Metadata 策略。
3. **部署**：通配 DNS 和通配证书，细节写在 deploy 仓库。
4. **旧链接**：apex 对旧路径返回 301。
5. **文档**：
   - README 的「Security and trust」和 SECURITY.md 里 App 共享 origin 的那段，改成按用户隔离，并写清同一用户的 App 之间仍然不隔离。
   - AGENTS.md 的关键约束里加上「Host 决定用户」这条不变量。
6. **用户规模够了以后**：提交 PSL，提交前确认域名注册期满足要求。

## 待定问题

- slug 按用户唯一，还是保持全局唯一？
- 用户名能不能改？如果能改，旧名永久保留，还是保留一段时间？
- 只在 SaaS 上开通配模式，还是也给自部署的多用户场景用？
- apex 的旧路径 301 保留多久（建议永久保留）？
- 以后要不要做每 App 子域？做的话，用户名长度现在就要按 21 来定。

## 相关

- `SECURITY.md`：现有的信任边界说明（Apps 共享 origin、同 site 的兄弟主机）。
- `kb/plans/2026-11-02-tinyhost.md`：总 plan，包含 URL、权限和安全决策。
- `kb/sessions/2026-11-02-th-m2-fix2.md`：`/login` CSP 例外的来由。
- 代码：`internal/server/server.go`（Host 分流、`mainURL`）、`internal/server/admin.go`（会话 cookie、`/view-auth`、`/_/auth`、登录 CSP）、`internal/server/content.go`（私有内容鉴权）、`internal/server/previews.go`、`internal/store/auth.go`（`SlugOwner`）、`internal/store/auth_schema.sql`、`internal/slug`。
- PSL Guidelines：https://github.com/publicsuffix/list/wiki/Guidelines
