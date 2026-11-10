#!/bin/sh
set -eu
HTML_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec sh "$HTML_DIR/部署配置/启动Word服务-Linux.sh" "$@"
