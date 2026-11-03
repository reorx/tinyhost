#!/bin/sh
# Second half of the self-host acceptance (.github/workflows/accept-selfhost.yml).
# Expects a server already running with APPS_URL / ADMIN_URL, and VERSION in the environment.
# Usage: sh scripts/accept-selfhost.sh "SERVER_COMMAND" DATA_DIR
# SERVER_COMMAND is the tinyhost-server invocation that reaches the data directory,
# e.g. "./tinyhost-server" or "docker exec tinyhost /tinyhost-server" (word-split on purpose).
set -eu
server_cmd=${1:?server command required}
data_dir=${2:?data dir required}
: "${APPS_URL:?}" "${ADMIN_URL:?}" "${VERSION:?}"
work=$(mktemp -d)

# shellcheck disable=SC2086
token=$($server_cmd token create --name selfhost-acceptance --data-dir "$data_dir")
test -n "$token"
echo "PASS token create"

home_dir="$work/home"
mkdir "$home_dir"
env -i HOME="$home_dir" PATH="$PATH" sh -c 'curl -fsSL --retry 3 --retry-all-errors "$1/install.sh" | sh' sh "$APPS_URL"
cli="$home_dir/.local/bin/tinyhost"
test "$("$cli" version)" = "$VERSION"
echo "PASS cli installed from the instance, version=$VERSION"

mkdir "$work/site"
marker="selfhost-$$-$(date +%s)"
printf '<!doctype html><title>t</title><p>%s</p>\n' "$marker" > "$work/site/index.html"
export TINYHOST_SERVER="$APPS_URL" TINYHOST_TOKEN="$token" HOME="$home_dir"
"$cli" deploy "$work/site" --app acceptance --create --public --json > "$work/deploy.json"
cat "$work/deploy.json"
echo "PASS deploy"

body=$(curl -fsS "$APPS_URL/acceptance/")
case "$body" in
  *"$marker"*) echo "PASS apps hostname serves the published content" ;;
  *) echo "content mismatch: $body" >&2; exit 1 ;;
esac

code=$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/login")
test "$code" = 200
echo "PASS admin /login returns 200"
