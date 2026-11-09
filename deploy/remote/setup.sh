#!/usr/bin/env bash
set -Eeuo pipefail
# Resolve a supplied public-key path before entering the source directory.
rc_args=()
while [[ $# -gt 0 ]]; do
  if [[ "$1" == --owner-key ]]; then
    [[ $# -ge 2 ]] || { printf 'Missing public-key path\n' >&2; exit 1; }
    rc_args+=(--owner-key "$(realpath -- "$2")"); shift 2
  else rc_args+=("$1"); shift; fi
done
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
for tool in python3 openssl docker ss; do command -v "$tool" >/dev/null; done
docker compose version
[[ -z "$(docker ps -aq --filter label=com.docker.compose.project=remote-console-generic)" ]] || { printf 'Existing generic deployment preserved. Use its own upgrade process.\n' >&2; exit 1; }
if ss -H -ltn | awk '$4 ~ /:(7000|3478|18079|18081)$/ {found=1} END {exit !found}'; then
  printf 'Required port is in use; existing services preserved. Choose another host or configure ports first.\n' >&2; exit 1
fi
if ss -H -lun | awk '$4 ~ /:3478$/ {found=1} END {exit !found}'; then
  printf 'TURN UDP port is in use; existing services preserved.\n' >&2; exit 1
fi
python3 initialize.py "${rc_args[@]}"
docker compose config --quiet
docker compose up -d --build
printf '\nServices started; configure your own HTTPS reverse proxy and validate your phone.\n'
