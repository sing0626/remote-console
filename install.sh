#!/usr/bin/env bash
# Generic bootstrap: no personal deployment, credentials or bundled phone identities.
set -Eeuo pipefail
rc_release=v0.4.2
[[ $(id -u) = 0 ]] || { printf '請用 root 或 sudo bash 執行。\n' >&2; exit 1; }
[[ $(uname -s) = Linux ]] || { printf '此安裝器需要 Linux。\n' >&2; exit 1; }
if [[ "${1:-}" = upgrade ]]; then
  shift
  for rc_tool in curl python3 openssl docker; do command -v "$rc_tool" >/dev/null || { printf '缺少工具：%s\n' "$rc_tool" >&2; exit 1; }; done
  rc_tmp=$(mktemp -d /tmp/remote-console-bootstrap.XXXXXXXX)
  trap 'rm -rf -- "$rc_tmp"' EXIT
  curl -fL --retry 4 --retry-all-errors --connect-timeout 15 --max-time 120 --progress-bar "https://raw.githubusercontent.com/sing0626/remote-console/$rc_release/scripts/manage.py" -o "$rc_tmp/manage.py"
  if [[ -f /opt/remote-console-source/.remote-console-managed ]]; then
    python3 "$rc_tmp/manage.py" update "$@"
  else
    python3 "$rc_tmp/manage.py" migrate "$@"
  fi
  exit
fi
if [[ -f /opt/remote-console-source/.remote-console-managed ]]; then
  exec python3 /opt/remote-console-source/scripts/manage.py "$@"
fi
for rc_existing in /opt/remote-console-source /var/lib/remote-console-generic /usr/local/bin/remote-console; do
  [[ ! -e "$rc_existing" && ! -L "$rc_existing" ]] || { printf '現有檔案已保留：%s\n' "$rc_existing" >&2; exit 1; }
done
if [[ "${1:-}" = migrate ]]; then
  # Existing deployments already have dependencies. Never alter their system packages.
  for rc_tool in curl python3 openssl docker; do command -v "$rc_tool" >/dev/null || { printf '缺少工具：%s\n' "$rc_tool" >&2; exit 1; }; done
  rc_tmp=$(mktemp -d /tmp/remote-console-bootstrap.XXXXXXXX)
  trap 'rm -rf -- "$rc_tmp"' EXIT
  curl -fL --retry 4 --retry-all-errors --connect-timeout 15 --max-time 120 --progress-bar "https://raw.githubusercontent.com/sing0626/remote-console/$rc_release/scripts/manage.py" -o "$rc_tmp/manage.py"
  python3 "$rc_tmp/manage.py" "$@"
  exit
fi
command -v apt-get >/dev/null || { printf '自動安裝依賴支援 Ubuntu / Debian；其他 Linux 請先安裝 Docker Compose、Python 3、curl、OpenSSL，然後執行 scripts/manage.py install。\n' >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl python3 openssl
if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
  # Official Docker repository. Existing Docker installations are never removed.
  . /etc/os-release
  [[ "$ID" = ubuntu || "$ID" = debian ]] || { printf '只支援 Ubuntu / Debian 自動安裝 Docker。\n' >&2; exit 1; }
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' "$(dpkg --print-architecture)" "$ID" "$VERSION_CODENAME" > /etc/apt/sources.list.d/remote-console-docker.list
  apt-get update
  if command -v docker >/dev/null; then
    apt-get install -y docker-compose-plugin
  else
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    systemctl enable --now docker
  fi
fi
docker info >/dev/null
rc_tmp=$(mktemp -d /tmp/remote-console-bootstrap.XXXXXXXX)
trap 'rm -rf -- "$rc_tmp"' EXIT
curl -fL --retry 4 --retry-all-errors --connect-timeout 15 --max-time 120 --progress-bar "https://raw.githubusercontent.com/sing0626/remote-console/$rc_release/scripts/manage.py" -o "$rc_tmp/manage.py"
# The manager downloads an immutable commit of the latest server release.
if [[ $# = 0 ]]; then
  python3 "$rc_tmp/manage.py" install
else
  case "$1" in
    --*) python3 "$rc_tmp/manage.py" install "$@" ;;
    *) python3 "$rc_tmp/manage.py" "$@" ;;
  esac
fi
