#!/bin/sh
# Detect the host before entering the bundled runtime. Never downloads packages.
set -eu
umask 077
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
APP_ROOT=${PDF_APP_ROOT:-$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)}
STATE_DIR=${PDF_WORD_STATE_DIR:-$SCRIPT_DIR}
SYSTEM=$(uname -s)
[ "$SYSTEM" = Linux ] || { echo "This launcher requires Linux, found: $SYSTEM" >&2; exit 1; }
CPU=$(uname -m)
KERNEL=$(uname -r)
case "$CPU" in
  x86_64|amd64) ARCH=amd64; LABEL='Linux x86_64' ;;
  aarch64|arm64) ARCH=arm64; LABEL='Linux ARM64' ;;
  loongarch64|loong64) ARCH=loong64; LABEL='Linux LoongArch64' ;;
  *) echo "Unsupported CPU: $CPU. Supported: x86_64, ARM64, LoongArch64." >&2; exit 1 ;;
esac
if [ "$ARCH" = loong64 ]; then
  major=${KERNEL%%.*}; rest=${KERNEL#*.}; minor=${rest%%.*}; minor=${minor%%-*}
  case "$major:$minor" in *[!0-9:]*|:*) echo 'Cannot identify kernel version' >&2; exit 1 ;; esac
  if [ "$major" -lt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -lt 19 ]; }; then
    echo 'LoongArch64 requires upstream ABI and Linux 5.19 or newer.' >&2; exit 1
  fi
fi
OS_NAME=Linux
RELEASE_FILE=${PDF_OS_RELEASE_FILE:-/etc/os-release}
if [ -r "$RELEASE_FILE" ]; then
  while IFS='=' read -r key value; do
    case "$key" in
      PRETTY_NAME) value=${value#\"}; value=${value%\"}; value=${value#\'}; value=${value%\'}; OS_NAME=$value ;;
    esac
  done < "$RELEASE_FILE"
fi
[ -f "$APP_ROOT/exe/信创Linux/pdf-tool-linux-$ARCH" ] || { echo "Missing matching executable: $ARCH" >&2; exit 1; }
[ -f "$APP_ROOT/exe/信创Linux/离线运行环境/runtime-$ARCH.tar.gz" ] || { echo "Missing offline runtime: $ARCH" >&2; exit 1; }
printf 'Detected system: %s / %s / kernel %s\nSelected offline package: %s\n' "$OS_NAME" "$CPU" "$KERNEL" "$LABEL"
if [ "${1:-}" = '--check' ]; then echo 'Check only. No service started or configuration written.'; exit 0; fi
[ "$#" -eq 0 ] || { echo 'Usage: sh start-linux.sh [--check]' >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo 'Run as root: sudo sh start-linux.sh' >&2; exit 1; }
PORT=${PDF_WORD_PORT:-8787}
case "$PORT" in *[!0-9]*|'') echo 'Invalid PDF_WORD_PORT' >&2; exit 1 ;; esac
[ "$PORT" -ge 1024 ] && [ "$PORT" -le 65535 ] || { echo 'Port must be 1024..65535' >&2; exit 1; }
mkdir -p "$STATE_DIR"
TOKEN_FILE="$STATE_DIR/service-token.txt"
if [ -z "${PDF_TOOL_TOKEN:-}" ] && [ -f "$TOKEN_FILE" ]; then PDF_TOOL_TOKEN=$(cat "$TOKEN_FILE"); fi
if [ -z "${PDF_TOOL_TOKEN:-}" ]; then PDF_TOOL_TOKEN=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n'); fi
case "$PDF_TOOL_TOKEN" in *[!A-Za-z0-9_-]*|'') echo 'Invalid PDF_TOOL_TOKEN characters' >&2; exit 1 ;; esac
[ "${#PDF_TOOL_TOKEN}" -ge 24 ] && [ "${#PDF_TOOL_TOKEN}" -le 128 ] || { echo 'Token must be 24..128 characters' >&2; exit 1; }
# Do not rewrite a running instance's credentials or proxy configuration.
[ ! -d "$APP_ROOT/exe/信创Linux/.running-$ARCH" ] || { echo 'Already running, or a stale lock remains. Check the existing instance first.' >&2; exit 1; }
printf '%s' "$PDF_TOOL_TOKEN" > "$TOKEN_FILE"
export PDF_TOOL_TOKEN
PROGRAM="$APP_ROOT/exe/信创Linux/pdf-tool-linux-$ARCH"
chmod 755 "$PROGRAM"
set -- --setup-nginx --nginx-template "$SCRIPT_DIR/nginx-word.conf.template" --nginx-backups "$STATE_DIR/nginx-backups" --word-port "$PORT"
[ -z "${PDF_NGINX_BIN:-}" ] || set -- "$@" --nginx "$PDF_NGINX_BIN"
[ -z "${PDF_NGINX_PREFIX:-}" ] || set -- "$@" --nginx-prefix "$PDF_NGINX_PREFIX"
[ -z "${PDF_NGINX_CONFIG:-}" ] || set -- "$@" --nginx-config "$PDF_NGINX_CONFIG"
[ -z "${PDF_SITE_PATH:-}" ] || set -- "$@" --site-path "$PDF_SITE_PATH"
PDF_HOST_OS=$OS_NAME
PDF_HOST_ARCH=$CPU
PDF_ENGINE_VARIANT=$LABEL
export PDF_HOST_OS PDF_HOST_ARCH PDF_ENGINE_VARIANT

# Wait for our private runtime before publishing an Nginx route to it.
SERVICE_PID=''
cleanup_service() {
  if [ -n "$SERVICE_PID" ]; then
    kill -TERM "$SERVICE_PID" 2>/dev/null || true
    wait "$SERVICE_PID" 2>/dev/null || true
  fi
}
trap cleanup_service EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
sh "$APP_ROOT/exe/信创Linux/start.sh" --listen "127.0.0.1:$PORT" &
SERVICE_PID=$!
ready=0
attempt=0
while kill -0 "$SERVICE_PID" 2>/dev/null; do
  if "$PROGRAM" --check-service --listen "127.0.0.1:$PORT" > "$STATE_DIR/service-check.log" 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 300 ]; then break; fi
  sleep 1
done
if [ "$ready" != 1 ]; then
  echo 'Word service failed to become ready. Nginx configuration has not been changed.' >&2
  if [ -f "$STATE_DIR/service-check.log" ]; then cat "$STATE_DIR/service-check.log" >&2; fi
  exit 1
fi
"$PROGRAM" "$@"
echo 'Word service is ready. Nginx configuration has been checked and reload requested.'
status=0
wait "$SERVICE_PID" || status=$?
SERVICE_PID=''
exit "$status"
