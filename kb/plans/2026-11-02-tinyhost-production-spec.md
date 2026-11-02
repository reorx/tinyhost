---
created: 2026-11-02
tags:
  - tinyhost
  - deployment
  - release
  - backup
  - open-source
---

# tinyhost M4：上线

这是 [tinyhost 总 plan](2026-11-02-tinyhost.md) 的第四期，依赖 M1–M3 全部完成。本期同时修改两个仓库：tinyhost 本仓库（发版流水线、开源材料）和 deploy 仓库（`~/Library/Mobile Documents/com~apple~CloudDocs/deploy`，部署、备份、文档）。

## PRD

### 背景

前三期都在本地验证。这一期要让 tinyhost 真正跑在 `th.reorx.com` 和 `th-admin.reorx.com` 上，有备份、有版本化的发布，并以开源项目的形式公开；最后用真实的云端 Agent 走完全程。

### 目标

- 任何一个云端 Agent 照着总 plan 的验收方式，就能在线上用起来。
- 服务器丢了能恢复。
- 打一个版本 tag 就能产出所有平台的二进制，并完成部署升级。
- 别人能照着 README 自己部署一套。

### 功能与交互

**线上服务**
- `https://th.reorx.com`：App、预览、Agent 接口、安装脚本、skill。
- `https://th-admin.reorx.com`：后台。
- 授权申请和发布通知推到我的手机（经 ntfy → Bark）。

**发版**
- 推送 `v*` tag 后，自动构建并发布：
  - CLI：linux / macOS / Windows × amd64 / arm64
  - 服务端：linux amd64 / arm64、macOS arm64
  - 校验文件，以及服务端的容器镜像
- 线上升级：在 deploy 仓库改版本号和校验值，执行一次 playbook。升级后，Agent 重新运行安装命令即可拿到同版本的 CLI。

**备份与恢复**
- 每 6 小时把数据库快照和新增的文件备份到 OSS。
- 有一份写好的恢复步骤，并在一台空机器（或临时目录）上演练过一次。

**开源**
- GitHub 公开仓库，包括：README（是什么、截图、快速开始、自部署指南、安全说明）、许可证、安全说明文档。
- 仓库在发布前保持私有，公开前我再确认一次。

### 不做的事

- 多台服务器、高可用。
- 自动化的端到端测试流水线（真实 Agent 的验收是手动的）。
- Homebrew、apt 等包管理器的分发。

### 验收标准

1. 推一个 tag，GitHub Release 上出现全部二进制和 `checksums.txt`，容器镜像推送成功。
2. 线上 `https://th.reorx.com/install.sh` 在 Linux amd64、Linux arm64 和 macOS 上都能装好与服务器同版本的 CLI。
3. 一个真实的 ChatGPT Agent 和一个 Grok 云端环境，只拿到"运行安装命令，然后读 tinyhost skill"这一句话，就能完成授权（我在手机上批准）、预览、发布，并把两个链接告诉我。
4. 一个 Agent 发布看板 App，另一个 Agent 推数据，我在手机上打开主入口能看到带数据的看板。
5. 私有 App 在手机浏览器里需要先在 `th-admin.reorx.com` 登录，登录后能看。
6. 在一台空机器或临时目录里，按恢复步骤从 OSS 恢复，所有 App、版本、Agent 都在，链接能打开。
7. README 按"快速开始"一节走一遍，能在一台新的 Linux 机器上跑起一套。
8. 上线前确认 reorx.com 下其他用 cookie 的服务不受 tinyhost 上不可信内容的影响（见总 plan 的风险）。

## Tech Design

### 现状

- tinyhost 仓库：M1–M3 完成后，有 `Makefile`（test / build），没有发版流水线、README、许可证。
- deploy 仓库：
  - ali-hk-01 的 Caddy 由 caddy role 整个文件接管，主配置用 `import /etc/caddy/sites/*.caddy` 引入各 role 自己的站点文件（vibench-relay 就是这么做的）。
  - `*.reorx.com` 用方案 A（CF Origin Cert + 橙云），证书在 `{{ caddy_certs_dir }}/origin-cert.pem`。
  - 已占用的端口有 3460–3467 和 3470，**3471 空闲**。
  - Go 二进制服务的发布方式参考 hookploy role：`group_vars/all.yml` 里写 `*_version` / `*_release_sha256` / `*_release_url`，用 `get_url` 带 checksum 下载，再 `unarchive`。
  - systemd 加固写法参考 `roles/vibench-relay`：专用系统用户、`StateDirectory`、`ProtectSystem=strict`、`Restart=on-failure` + `StartLimit*`、`LogNamespace`。
  - 备份参考 `roles/backup/templates/backup-vibench-relay.sh.j2`：先 `sqlite3 .backup` 做快照，gzip 后用 rclone 传到 `oss:${OSS_BUCKET}/backups/<app>`，按 `BACKUP_RETENTION` 清理；配套 service + timer。
  - ntfy 的话题清单是 playbook 里的 `ntfy_bridge_topics`（ntfy 在 tc-sg-01）。

### 方案

> **2026-11-02 变更（用户决定）**：上线前核实发现 reorx.com 下的其他服务和 apps 同站时有风险，所以 apps 改用独立注册域名 `https://tinyhost.space`，后台改为 `https://th.reorx.com`；服务器改为 tc-sg-01。本 spec 下文的 `th.reorx.com`（apps）、`th-admin.reorx.com`、ali-hk-01 都按这条理解。仓库一开始就公开（MIT）。运行方式也从 systemd + Release 二进制改为 Docker Compose 跑公开发布的 ghcr 镜像，和服务器上其他业务应用一致；升级就是改镜像版本、跑 playbook。

照 deploy 仓库现有的套路，新增一个 `tinyhost` role，部署在 ali-hk-01，以 systemd 服务的形式运行（不用容器），二进制从 GitHub Release 下载。备份在 backup role 里加一组 `tinyhost` 的 timer。tinyhost 仓库新增 release workflow，产出物的命名要和服务端 `/_/dl/` 代理、安装脚本的预期一致。

### 逻辑变动

**tinyhost 仓库**
- `Makefile`：
  - `dist` 构建所有目标，每个打成 tar.gz（Windows 打成 zip），并生成 `checksums.txt`。
  - 命名：`tinyhost_<ver>_<os>_<arch>.tar.gz`、`tinyhost-server_<ver>_<os>_<arch>.tar.gz`（`<ver>` 不带 `v`）。必须和 M3 的 `install.sh`、`/_/dl/` 一致。
  - 版本号用 `-ldflags -X github.com/reorx/tinyhost/internal/version.Version=…` 注入。
- `.github/workflows/ci.yml`：push 和 PR 时跑 `go test ./...`、`go vet ./...`。
- `.github/workflows/release.yml`：`v*` tag 触发。依次 `make dist`，`gh release create`，然后构建服务端镜像并推到 `ghcr.io/reorx/tinyhost-server:<ver>` 和 `:latest`。
- `Dockerfile`：基础镜像用 `gcr.io/distroless/static`（自带 CA 证书，代理 GitHub 下载和发 ntfy 时需要），只放 `tinyhost-server`；`VOLUME /data`；默认参数 `serve --data-dir /data --listen :3471`。
- README（英文）：写之前先读 `~/.claude/docs/GITHUB_README.md`。内容：是什么、截图（后台、批准页、一个示例看板）、Agent 视角的快速开始、自部署（二进制 / 容器 / Caddy 示例配置 / 两个域名的要求）、安全模型与已接受的同站风险。
- `LICENSE`：默认 MIT，公开前确认。
- `SECURITY.md`：安全模型，以及如何报告问题。
- 中文的详细文档保留在 `kb/docs/`。

**deploy 仓库**
- `group_vars/all.yml`：`tinyhost_version`、`tinyhost_release_sha256`、`tinyhost_release_url`（指向 `tinyhost-server_<ver>_linux_amd64.tar.gz`）。
- `roles/tinyhost/`（结构参考 vibench-relay 和 hookploy）：
  - 系统用户 `tinyhost`；程序放在 `/opt/tinyhost/`（root 所有）；数据在 `/var/lib/tinyhost`（`StateDirectory`，0700）。
  - 下载、校验、解压 release；`templates/env.j2` 生成环境变量文件，0600，只在文件不存在时写入：
    - `TINYHOST_APPS_URL=https://th.reorx.com`
    - `TINYHOST_ADMIN_URL=https://th-admin.reorx.com`
    - `TINYHOST_DATA_DIR=/var/lib/tinyhost`
    - `TINYHOST_LISTEN=127.0.0.1:3471`
    - `TINYHOST_TRUSTED_PROXY_HEADER=CF-Connecting-IP`
    - `TINYHOST_NTFY_URL=https://ntfy.reorx.com/tinyhost`
    - `# TINYHOST_NTFY_TOKEN=`（密值，人工在服务器上填写）
  - `tinyhost.service.j2`：`EnvironmentFile=`，`ExecStart=/opt/tinyhost/tinyhost-server serve`，`Restart=on-failure`，加固项照 vibench-relay，`MemoryMax=512M`。
  - `site.caddy.j2` 写到 `/etc/caddy/sites/tinyhost.caddy`，两个站点块都 `reverse_proxy 127.0.0.1:3471` 并使用 Origin Cert。apps 站点加 `request_body { max_size 60MB }`。访问日志写到 `/var/log/caddy/tinyhost.log`（由 vector 采集）。
  - 按 `enabled_apps` 控制，只在 ali-hk-01 启用。
- `roles/backup/`：
  - 新增 `backup-tinyhost.{sh,service,timer}.j2`，每 6 小时运行。
  - 先对 `/var/lib/tinyhost/tinyhost.db` 做 `sqlite3 .backup` 快照，gzip 后传到 `backups/tinyhost/db/`，按保留期清理。
  - 再 `rclone copy /var/lib/tinyhost/blobs oss:…/backups/tinyhost/blobs`：只增不删，文件块不可变，所以天然是增量。
  - `dl/` 缓存不备份。
  - 时间错开，先备份 db 再复制 blobs，这样快照里引用的文件块一定已经在 OSS 上（GC 有 1 小时的 mtime 保护，blob 复制紧接着运行即可）。
- playbook 的 `ntfy_bridge_topics` 增加 `{ topic: tinyhost }`（全部推送到手机）。publisher token 复用现有的。
- Cloudflare：`th`、`th-admin` 两条 A 记录指向 ali-hk-01，开橙云。
- 文档：deploy 仓库新增 `kb/docs/tinyhost.md`（概况、域名、端口、环境变量核对、升级、备份与恢复 SOP、日志查询），并在 AGENTS.md 的应用表里加一行。
- 首次部署后，在服务器上执行 `tinyhost-server passwd`（读同一份 env，以 `tinyhost` 用户身份运行）。

### 实施步骤

- [x] tinyhost 仓库：`make dist`、ci.yml、release.yml、Dockerfile；先用 `v0.1.0-rc.1` 打一次，检查产物命名（2026-11-02 `167a9d1`；仓库一开始就公开，MIT 许可证随首次推送加入，见 `kb/sessions/2026-11-02-th-m4-release.md`）
- [x] **上线前核实**：reorx.com zone 的 Bot Fight Mode 状态；CF 缓存规则（`private` 和 `no-cache` 的响应不能被边缘缓存）；列出 reorx.com 下用 cookie 会话的服务，逐个确认 CSRF 防护（2026-11-02：核实结果让用户决定 apps 改用独立注册域名，记录在 deploy 仓库 `kb/docs/tinyhost.md`；两个主机都加了 bypass 缓存规则）
- [x] deploy 仓库：`tinyhost` role、group_vars、Caddy 站点、DNS 记录，`--limit ali-hk-01 --tags tinyhost` 下发（2026-11-02 改为下发到 tc-sg-01，DNS 由用户添加）
- [ ] 服务器上设置密码、填 ntfy token、重启；从外网 curl 验证两个域名（curl 验证已完成；密码和 token 等用户设置）
- [x] backup role 的 tinyhost 任务，手动触发一次并确认 OSS 上有文件（2026-11-02，空实例；带数据的备份在恢复演练里再做一次）
- [x] 恢复演练（验收 6）（2026-11-02 先在 systemd 布局上演练两次，迁到 compose 后又演练一次，3 个测试 App 的内容 hash 全部一致；SOP 在 deploy 仓库 `kb/docs/tinyhost.md`）
- [ ] 真实 Agent 端到端验收（验收 3–5），截图存到 `tmp/<日期>-m4-production-e2e/`
- [ ] README、LICENSE、SECURITY.md；**公开仓库前请用户确认**
- [ ] 打正式版 `v0.1.0` tag，更新 deploy 仓库的版本号
- [ ] deploy 仓库文档；tinyhost 仓库更新 `AGENTS.md`

### 测试与验证

| 验收 | 怎么验 |
|---|---|
| 1 | 看 GitHub Release 页面和 ghcr；`checksums.txt` 与文件逐一核对 |
| 2 | 服务器上（或 CI 里）用 amd64 / arm64 的干净容器跑安装命令；本机 macOS 跑一次；`tinyhost version` 与 `/_/api/v1/whoami` 的 server_version 一致 |
| 3–5 | 手动：真实 ChatGPT Agent、Grok 各走一遍；手机上用 Safari 操作 |
| 6 | 新建临时数据目录，用 rclone 拉回最新的 db 快照和 blobs，用 `--listen` 换个端口起服务，抽查 3 个 App 的历史版本 |
| 7 | 在一台临时 Linux 机器上照 README 部署 |
| 8 | 上面"上线前核实"一步的记录写进 deploy 仓库的 `kb/docs/tinyhost.md` |

### 风险与待定

- **Bot Fight Mode**：如果 reorx.com 开着它，而且免费套餐无法按路径豁免，有两个办法：关掉 zone 级的开关，或者把 `th` 改成灰云 + Let's Encrypt（方案 B，会暴露源站 IP）。核实之后由用户决定。
- **UptimeFlare 名额已满**：按 deploy 仓库的规矩，新域名要加站外监控，但每轮探测名额用完了。可以让出一个名额，或者像 vibench 一样明确不加。上线时请用户决定。
- **许可证**：默认 MIT，公开前确认。
- **ali-hk-01 磁盘**：总共 50G。tinyhost 的文件块只增不减（直到 GC），需要在 `kb/docs/tinyhost.md` 里写上查看占用的命令，并定一个告警阈值（比如数据目录超过 10G）。
- **OSS 上的文件块只增不删**，长期会积累 GC 删掉的文件。量小时忽略；以后需要时再写一个按 db 快照对账清理的脚本。
