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
env -i HOME="$home_dir" PATH="$PATH" sh -c 'curl -fsSL "$1/install.sh" | sh' sh "${server%/}"
cli="$home_dir/.local/bin/tinyhost"
actual=$(env -i HOME="$home_dir" PATH="$PATH" "$cli" version)
if [ "$actual" != "$expected" ]; then echo "版本不一致：$actual != $expected" >&2; exit 1; fi
printf 'PASS version=%s\n' "$actual"
env -i HOME="$home_dir" PATH="$PATH" "$cli" skill > "$work/cli-skill.md"
curl -fsSL "${server%/}/skill.md" > "$work/http-skill.md"
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
