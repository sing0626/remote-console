#!/usr/bin/env bash
# New installations only. No bundled phone identities or deployment credentials.
set -Eeuo pipefail
rc_dest=/opt/remote-console-source
for rc_tool in curl tar python3 openssl docker ss realpath; do command -v "$rc_tool" >/dev/null || { printf 'Install required tool: %s\n' "$rc_tool" >&2; exit 1; }; done
[[ ! -e "$rc_dest" ]] || { printf 'Existing destination preserved: %s\nClone the source separately for upgrades.\n' "$rc_dest" >&2; exit 1; }
[[ $# -gt 0 ]] || { printf 'Usage: bash install.sh --domain YOUR_DOMAIN --public-ip YOUR_IPV4 --owner-key /path/to/YOUR_PHONE_PUBLIC_KEY\n' >&2; exit 1; }
rc_tmp=$(mktemp -d /tmp/remote-console-source.XXXXXXXX)
trap 'rm -rf -- "$rc_tmp"' EXIT
curl -fL --retry 3 https://github.com/sing0626/remote-console/archive/refs/heads/main.tar.gz -o "$rc_tmp/source.tar.gz"
mkdir -m 700 "$rc_dest"
tar -xzf "$rc_tmp/source.tar.gz" --strip-components=1 -C "$rc_dest"
bash "$rc_dest/deploy/remote/setup.sh" "$@"
