---
created: 2026-11-02
tags:
  - tinyhost
  - release
  - ci
  - m4
---

## 协调者摘要（th-m4-release）

- 结果：本任务六条完成标准全部满足；公开仓库 https://github.com/reorx/tinyhost，SSH origin，master 已推送。
- 实现提交：`167a9d1733daca887452c35d751476b56e086f55`；总结独立提交，准确 hash 用 `git log -1 --format=%H -- kb/sessions/2026-11-02-th-m4-release.md` 获取。
- 本地：`mise exec -- go test -json ./...` 通过 215 项（140 顶层、75 子测试），测试无失败或跳过；三个无测试包不计入数量。
- 检查：`go vet ./...`、`gofmt -l internal cmd`、额外的 skill/scripts 格式检查、actionlint 和 `git diff --check` 均通过。
- 构建：`make dist VERSION=0.0.0-test` 得到 6 个 CLI 包、3 个服务端包和 checksums，9 个 SHA-256 全部通过。
- master [CI 全绿](https://github.com/reorx/tinyhost/actions/runs/41455609700)；[rc Release 流水线全绿](https://github.com/reorx/tinyhost/actions/runs/41455649216)，未重试、未移动或删除 tag。
- [v0.1.0-rc.1](https://github.com/reorx/tinyhost/releases/tag/v0.1.0-rc.1) 是已发布的 prerelease；10 个资产已全部下载，9 个包校验通过，本机 CLI/服务端都输出 `0.1.0-rc.1`。
- 镜像：匿名 registry token 可拉取 `ghcr.io/reorx/tinyhost-server:0.1.0-rc.1` manifest，含 linux/amd64、linux/arm64；`:latest` HTTP 404，无需调整包可见性。
- 安装：下载的 rc 服务端不加 `--dl-dir`，全新 HOME 经下载代理访问 GitHub 安装成功；版本一致、HTTP/CLI skill 一致、login 正常产生待批准申请。
- 决定：Windows 按 M4 spec 使用 zip，其余 tar.gz；mise 精确锁定本机实际的 Go 1.27.3；镜像复用 Release 二进制，不二次编译。
- 审计：首次 push 前 gitleaks 扫描全部 17 个历史提交无密值；文档历史候选人工核对无真实 IP、bucket 名或已确认的其他服务弱点，无需改写历史。
- 证据：`tmp/tinyhost-v1/work/th-m4-release/`；无截图、未启动浏览器或模拟器；本地服务已退出，临时 HOME 的 pending-login.json 已删除。
- 未验证：Linux 干净容器安装、生产部署、真实云端 Agent、真实通知；它们归后续任务，本次未操作生产、DNS、Cloudflare 或 deploy 仓库。
- 交给协调者：合入下方 AGENTS.md 建议；可签收 release 并按 brief 推进 deploy、verify、oss，本次未改共享文件或协调记录。

## 实现说明

`scripts/dist.sh` 的统一构建函数生成六个平台的 CLI（linux/darwin/windows × amd64/arm64）和三个服务端包（linux amd64/arm64、darwin arm64）。Windows 使用 zip，其他目标使用 tar.gz。文件名去掉版本开头的 v，`-ldflags -X github.com/reorx/tinyhost/internal/version.Version=…` 保留传入的完整构建版本；流水线传入去掉 v 的 tag 版本，使安装脚本、CLI、服务端、镜像一致。所有包均 `CGO_ENABLED=0`。

新增的 `scripts/dist_test.go` 先红后绿，使用假编译器验证平台矩阵、二进制名称、版本注入、包内只有目标二进制及 SHA-256 清单。红灯还暴露 macOS tar 自动加入 `._tinyhost` 的问题，现通过 `COPYFILE_DISABLE=1` 禁用元数据。真实构建和下载验收另行验证了实际二进制。

消费方核对：`skill/install.sh:23` 读取 `tinyhost_${version}_${os}_${arch}.tar.gz`，仅支持 Linux/macOS，与这四个平台资产一致；Windows 是手动下载，不经过安装脚本。`internal/distribution/proxy.go:204` 使用 `https://github.com/<repo>/releases/download/v<version>/<name>`，文件白名单由 checksums 决定，新增服务端包也符合单段文件名约束。未修改这些消费者及 API/JSON 契约。

CI 在 push 和 PR 上运行格式检查、全量测试及 vet。Release 在 `v*` tag 上构建并校验包，然后 `gh release create` 上传全部包和 checksums，最后推送双架构 GHCR 镜像并分别运行 `version`。版本中带 `-` 时同时设置 `--prerelease --latest=false`，镜像只推版本标签；正式版本才额外推镜像 `:latest`。当前尚未打正式版。

Dockerfile 从已有的服务端 tar.gz 解包，最终使用 `gcr.io/distroless/static:latest`，只复制服务端二进制，声明 `/data` volume，默认 `serve --data-dir /data --listen :3471`。构建上下文由 `.dockerignore` 限定为服务端包，镜像构建和运行均在 CI 完成，本机没有运行 Docker。MIT 许可证版权人为 Reorx，年份 2026。

## 验证与证据

证据目录：`tmp/tinyhost-v1/work/th-m4-release/`，全部未跟踪、不提交。

| 验证 | 证据及结果 |
|---|---|
| BDD 红灯 | `red-dist.log`；旧脚本不满足干净归档契约 |
| 本地全量测试 | `tests.jsonl`；140 + 75 = 215 项通过 |
| 本地检查 | `vet.log`、`gofmt.log` 均为空；actionlint 与 diff 检查通过 |
| 本地 dist | `dist.log`、`local-checksums.log`、`local-assets.log`；9 包、9 校验和、两个本机 version 正确 |
| 历史审计 | `gitleaks.json` 为 `[]`，`gitleaks.log`、`doc-history.patch`、`doc-history-candidates.txt`；密值扫描及文档人工核对通过 |
| GitHub | `repo.json`、`ci.json`、`release-run.json`、`release-run.log`、`watch-ci.log`、`watch-release.log` |
| Release | `release.json`、`anonymous-release.json`、`rc.1-assets/`、`rc.1-assets.log`；匿名 API 也确认 10 个资产、非 draft、prerelease |
| 匿名镜像 | `registry-check.sh`、`registry.log`、`registry/manifest.json`、`registry/manifest-headers.txt`、`registry/latest.json`；两个 Linux 平台及 latest 404 |
| 真实安装链 | `install.log`、`rc.1-install/`、`rc.1-bin/`、`data/dl/0.1.0-rc.1/`；安装脚本三条 PASS；缓存中的 manifest 和 CLI 包与 Release 下载逐字节一致 |
| 服务端版本 | `whoami-headers.txt`；未登录 whoami 为预期 401，`X-Tinyhost-Version: 0.1.0-rc.1` |

可重复的资产核对命令：

```sh
sh tmp/tinyhost-v1/work/th-m4-release/verify-assets.sh \
  0.1.0-rc.1 tmp/tinyhost-v1/work/th-m4-release/rc.1-assets
sh tmp/tinyhost-v1/work/th-m4-release/registry-check.sh \
  0.1.0-rc.1 tmp/tinyhost-v1/work/th-m4-release/registry
```

安装验收使用下载的 `rc.1-bin/tinyhost-server`，以清空继承环境的方式启动，参数为 `serve --data-dir tmp/tinyhost-v1/work/th-m4-release/data --listen localhost:3471 --apps-url http://localhost:3471 --admin-url http://admin.localhost:3471 --insecure-cookies`。没有 `--dl-dir`，没有通知配置。执行 `scripts/accept-install.sh` 后，终止服务并等待退出，删除测试 HOME 中的待授权秘密文件。未创建真实 token。

## AGENTS.md 建议

以下交给协调者合入，本任务没有修改 AGENTS.md：

- `scripts/dist.sh`（`make dist VERSION=…`）：生成六平台 CLI（Windows zip，其余 tar.gz）、linux amd64/arm64 与 darwin arm64 服务端包及统一 checksums；包文件名不带版本前缀 v，二进制注入传入的 VERSION。macOS 打包使用 `COPYFILE_DISABLE=1`，避免 AppleDouble 元数据混入包。
- `.github/workflows/`：CI 在 push/PR 上执行格式检查、测试和 vet，Go 与 mise 精确锁定 1.27.3。`v*` tag 触发 Release 和 GHCR 双架构镜像；带连字符的版本为 prerelease，镜像不推 latest；已推 tag 不移动或删除，修复后递增 rc。
- `Dockerfile`：先运行同版本 `make dist`，镜像直接使用 dist 中的服务端包，最终基于 distroless/static；只包含服务端，数据在 `/data`，默认监听 `:3471`。本机不运行 Docker，镜像构建和双架构运行验证在 CI。

## 交给协调者

- release 的六条完成标准全部满足，可按 brief 派后续任务；Linux amd64/arm64 干净环境的安装验收仍留给 verify，不用镜像中的 version 检查代替。
- README、SECURITY.md 和正式版 v0.1.0 未开始，归 oss；共享 plan/spec 的现有未提交改动均未暂存。
- 没有发现本任务范围内尚待处理的产品问题；未修改共享 known-issues、next-up、计划勾选框。

## 相关文档

- [总 plan](../plans/2026-11-02-tinyhost.md) — 依据：架构与权限约束。
- [M4 spec](../plans/2026-11-02-tinyhost-production-spec.md) — 依据：产物、流水线与发版验收。
- [M3 分发总结](2026-11-02-th-m3-dist.md) — 参考：安装与下载代理消费契约、全新 HOME 验收脚本。
