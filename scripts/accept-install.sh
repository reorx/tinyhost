#!/bin/sh
# Run against a disposable test server. A pending authorization request is created.
# Usage: sh scripts/accept-install.sh SERVER EXPECTED_VERSION [NEW_WORK_DIR]
set -eu
server=${1:?usage: accept-install.sh SERVER EXPECTED_VERSION [NEW_WORK_DIR]}
expected=${2:?expected server version required}
work=${3:-$(mktemp -d)}
mkdir -p "$work"
work=$(cd "$work" && pwd)
home_dir="$work/home"
if [ -e "$home_dir" ]; then echo "验收 HOME 必须全新：$home_dir" >&2; exit 1; fi
mkdir "$home_dir"
# Clean child environment: no TINYHOST_SERVER/TOKEN/INSTALL_DIR, no real config.
# Same install command as the skill, including its retries for CDN 52x.
env -i HOME="$home_dir" PATH="$PATH" sh -c 'curl -fsSL --retry 3 --retry-all-errors "$1/install.sh" | sh' sh "${server%/}"
cli="$home_dir/.local/bin/tinyhost"
# The pipe hides a failed download (for example a Cloudflare 52x): sh just reads nothing.
if [ ! -x "$cli" ]; then echo "安装失败：没有生成 ${cli}，见上面 curl 或 install.sh 的输出" >&2; exit 1; fi
actual=$(env -i HOME="$home_dir" PATH="$PATH" "$cli" version)
if [ "$actual" != "$expected" ]; then echo "版本不一致：$actual != $expected" >&2; exit 1; fi
printf 'PASS version=%s\n' "$actual"
# Every Agent API response carries the server build; an unauthenticated whoami is enough.
curl -sS -D "$work/whoami-headers.txt" -o /dev/null "${server%/}/_/api/v1/whoami"
server_version=$(awk 'tolower($1) == "x-tinyhost-version:" {print $2}' "$work/whoami-headers.txt" | tr -d '\r')
if [ "$server_version" != "$expected" ]; then echo "服务器版本不一致：X-Tinyhost-Version=${server_version:-缺失} != $expected" >&2; exit 1; fi
printf 'PASS server X-Tinyhost-Version=%s\n' "$server_version"
env -i HOME="$home_dir" PATH="$PATH" "$cli" skill > "$work/cli-skill.md"
curl -fsSL --retry 3 --retry-all-errors "${server%/}/skill.md" > "$work/http-skill.md"
diff -u "$work/http-skill.md" "$work/cli-skill.md"
printf 'PASS skill HTTP/CLI identical\n'
set +e
env -i HOME="$home_dir" PATH="$PATH" "$cli" login --name install-acceptance --wait 1 --json > "$work/login.json" 2> "$work/login-stderr.txt"
status=$?
set -e
if [ "$status" -ne 4 ]; then cat "$work/login-stderr.txt" >&2; echo "预期等待授权退出码 4，得到 $status" >&2; exit 1; fi
if ! rg_available=$(command -v rg); then
  grep -q '"status": "pending"' "$work/login.json"
else
  "$rg_available" -q '"status": "pending"' "$work/login.json"
fi
printf 'PASS login created authorization without environment configuration (exit=4)\nEvidence: %s\n' "$work"
