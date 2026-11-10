#!/usr/bin/env bash
set -Eeuo pipefail
rc_release=v0.5.0
rc_release=${HALFCLOUD_RELAY_REF:-$rc_release}
[[ "$rc_release" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ || "$rc_release" =~ ^[0-9a-f]{40}$ ]] || { printf '版本格式無效。\n' >&2; exit 1; }
[[ $(id -u) = 0 && $(uname -s) = Linux ]] || { printf '請喺 Linux 用 root 或 sudo bash 執行。\n' >&2; exit 1; }
rc_command=${1:-install}
if [[ $# -gt 0 ]]; then shift; fi
if ! command -v python3 >/dev/null; then
  [[ "$rc_command" = install ]] || { printf '需要 Python 3。\n' >&2; exit 1; }
  apt-get update
  apt-get install -y --no-install-recommends python3 ca-certificates
fi
rc_tmp=$(mktemp -d /tmp/halfcloud-relay.XXXXXXXX)
trap 'rm -rf -- "$rc_tmp"' EXIT
curl -fL --retry 4 --retry-all-errors --connect-timeout 15 --max-time 120 --progress-bar \
  "https://raw.githubusercontent.com/sing0626/remote-console/$rc_release/scripts/relay.py" -o "$rc_tmp/relay.py"
python3 "$rc_tmp/relay.py" "$rc_command" "$@"
