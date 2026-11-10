#!/bin/sh
# Offline deployment only. No apt/dnf/curl/wget/pip/docker or external downloads.
set -eu
APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  loongarch64|loong64) ARCH=loong64 ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
if [ "$(id -u)" != 0 ]; then
  echo "Offline runtime needs root for chroot setup; the PDF service runs as UID 65534." >&2
  echo "Run: sudo sh start-offline.sh" >&2
  exit 1
fi
for CMD in tar sha256sum chroot mount umount mknod; do
  command -v "$CMD" >/dev/null 2>&1 || { echo "Required OS command missing: $CMD" >&2; exit 1; }
done
CHROOT_COMMAND=$(command -v chroot)
RUNTIME_DIR="$APP_DIR/离线运行环境"
ROOT="$APP_DIR/runtime-$ARCH"
LOCK="$APP_DIR/.running-$ARCH"
if ! mkdir "$LOCK" 2>/dev/null; then
  echo "Already running, or a stale lock exists: $LOCK" >&2
  echo "Confirm the old process is stopped before removing that empty lock directory." >&2
  exit 1
fi
CHILD_PID=''
MOUNTED=0
cleanup() {
  if [ -n "$CHILD_PID" ]; then kill -TERM "$CHILD_PID" 2>/dev/null || true; wait "$CHILD_PID" 2>/dev/null || true; fi
  if [ "$MOUNTED" = 1 ]; then umount "$ROOT/proc" 2>/dev/null || true; fi
  rmdir "$LOCK" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ ! -f "$ROOT/.pdf-runtime-ready" ]; then
  (cd "$RUNTIME_DIR" && sha256sum -c "runtime-$ARCH.sha256")
  mkdir -p "$ROOT"
  # Extraction is confined to this private directory; no host package database is touched.
  tar -xzf "$RUNTIME_DIR/runtime-$ARCH.tar.gz" -C "$ROOT" --no-same-owner
  mkdir -p "$ROOT/tmp" "$ROOT/home/pdftool" "$ROOT/opt/pdf-tool" "$ROOT/proc" "$ROOT/dev"
  chmod 1777 "$ROOT/tmp"
  chown 65534:65534 "$ROOT/home/pdftool"
  [ -e "$ROOT/dev/null" ] || mknod -m 666 "$ROOT/dev/null" c 1 3
  [ -e "$ROOT/dev/zero" ] || mknod -m 666 "$ROOT/dev/zero" c 1 5
  [ -e "$ROOT/dev/random" ] || mknod -m 666 "$ROOT/dev/random" c 1 8
  [ -e "$ROOT/dev/urandom" ] || mknod -m 666 "$ROOT/dev/urandom" c 1 9
  touch "$ROOT/.pdf-runtime-ready"
fi
# The HTML launcher and systemd use umask 077. The archive does not contain
# the root directory itself, so repair its traversal mode on every launch,
# including runtimes already marked ready by an older version.
chmod 755 "$ROOT"
cp "$APP_DIR/pdf-tool-linux-$ARCH" "$ROOT/opt/pdf-tool/pdf-tool"
chmod 755 "$ROOT/opt/pdf-tool/pdf-tool"
mount -t proc proc "$ROOT/proc" -o nosuid,nodev,noexec,ro
MOUNTED=1
if [ "$#" -eq 0 ]; then set -- --listen 0.0.0.0:8080; fi
echo "Offline runtime: $ARCH. LibreOffice and fonts are bundled."
env -i PATH=/usr/bin:/bin HOME=/home/pdftool LANG=C.UTF-8 TMPDIR=/tmp \
  SAL_USE_VCLPLUGIN=svp PDF_TOOL_TOKEN="${PDF_TOOL_TOKEN:-}" \
  PDF_HOST_OS="${PDF_HOST_OS:-Linux}" PDF_HOST_ARCH="${PDF_HOST_ARCH:-$(uname -m)}" \
  PDF_ENGINE_VARIANT="${PDF_ENGINE_VARIANT:-Linux $ARCH}" \
  "$CHROOT_COMMAND" --userspec=65534:65534 "$ROOT" /opt/pdf-tool/pdf-tool \
  --server --office /usr/lib/libreoffice/program/soffice.bin "$@" &
CHILD_PID=$!
STATUS=0
wait "$CHILD_PID" || STATUS=$?
CHILD_PID=''
exit "$STATUS"
