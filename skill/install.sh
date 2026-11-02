#!/bin/sh
set -eu
server={{SERVER}}
version={{VERSION}}
case "$(uname -s)" in
  Linux) os=linux; config_dir=${XDG_CONFIG_HOME:-$HOME/.config}/tinyhost ;;
  Darwin) os=darwin; config_dir="$HOME/Library/Application Support/tinyhost" ;;
  *) echo '不支持的系统：需要 Linux 或 macOS' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo '不支持的架构：需要 amd64 或 arm64' >&2; exit 1 ;;
esac
fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else echo '需要 curl 或 wget' >&2; exit 1
  fi
}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
file="tinyhost_${version}_${os}_${arch}.tar.gz"
fetch "$server/_/dl/checksums.txt" "$work/checksums.txt"
fetch "$server/_/dl/$file" "$work/$file"
expected=$(awk -v file="$file" '$2 == file || $2 == "*" file {print $1}' "$work/checksums.txt")
if [ ${#expected} -ne 64 ]; then echo '找不到唯一的 SHA-256 校验值' >&2; exit 1; fi
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$work/$file" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$work/$file" | awk '{print $1}')
else echo '需要 sha256sum 或 shasum' >&2; exit 1
fi
if [ "$expected" != "$actual" ]; then echo 'SHA-256 校验失败' >&2; exit 1; fi
tar -xzf "$work/$file" -C "$work" tinyhost
install_dir=${TINYHOST_INSTALL_DIR:-$HOME/.local/bin}
mkdir -p "$install_dir" "$config_dir"
chmod +x "$work/tinyhost"
# Rename within the install filesystem so upgrading a running CLI stays safe.
staged=$(mktemp "$install_dir/.tinyhost-XXXXXX")
cp "$work/tinyhost" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$install_dir/tinyhost"
umask 077
config_tmp=$(mktemp "$config_dir/.config-XXXXXX")
printf '%s\n' {{CONFIG}} > "$config_tmp"
mv -f "$config_tmp" "$config_dir/config.json"
printf '已安装 tinyhost %s 到 %s/tinyhost\n下一步：tinyhost skill\n' "$version" "$install_dir"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf '请加入 PATH（按你的实际安装路径）：\n  export PATH="%s:$PATH"\n' "$install_dir" ;;
esac
