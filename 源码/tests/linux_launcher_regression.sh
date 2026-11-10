#!/usr/bin/env bash
# Execute the real launchers with simulated OS commands. No ELF, mount or UID change.
set -euo pipefail
PROJECT=${1:?project directory required}
WORK=${2:?E-drive output directory required}
ORIGINAL_PATH=$PATH
RUN_ROOT="$WORK/launcher-regression-$(date +%Y%m%d-%H%M%S)-$$"
mkdir -p "$RUN_ROOT"
make_mocks() {
    local mock_dir="$1/mockbin"
    mkdir -p "$mock_dir"
    cat > "$mock_dir/command-mock" <<'MOCK'
#!/bin/sh
set -eu
command_name=${0##*/}
{ printf '%s|' "$command_name"; printf '%s|' "$@"; printf '\n'; } >> "$MOCK_LOG"
case "$command_name" in
    uname)
        case "${1:-}" in
            -s) echo Linux ;;
            -m) echo x86_64 ;;
            -r) echo 5.10.134-19.2.al8.x86_64 ;;
        esac ;;
    id) echo 0 ;;
    chmod)
        if [ "${1:-}" = 755 ] && [ "${2:-}" = "$MOCK_ROOT" ]; then
            : > "$MOCK_ROOT/.root-is-755"
        fi ;;
    chown|tar|sha256sum|mount|umount) : ;;
    mknod) shift 2; : > "$1" ;;
    env)
        while [ "$#" -gt 0 ]; do
            case "$1" in -i|*=*) shift ;; *) break ;; esac
        done
        exec "$@" ;;
    chroot)
        [ "${1:-}" = '--userspec=65534:65534' ] || exit 76
        [ "${2:-}" = "$MOCK_ROOT" ] || exit 76
        [ -f "$MOCK_ROOT/.root-is-755" ] || exit 77 ;;
    *) exit 2 ;;
esac
MOCK
    chmod +x "$mock_dir/command-mock"
    for name in uname id chmod chown tar sha256sum mknod mount umount env chroot; do
        cp "$mock_dir/command-mock" "$mock_dir/$name"
        chmod +x "$mock_dir/$name"
    done
}
test_runtime_permissions() {
    local scenario=$1
    local case_dir="$RUN_ROOT/runtime-$scenario"
    local app_dir="$case_dir/app"
    local runtime_root="$app_dir/runtime-amd64"
    mkdir -p "$app_dir/离线运行环境"
    make_mocks "$case_dir"
    cp "$PROJECT/exe/信创Linux/start-offline.sh" "$app_dir/start-offline.sh"
    echo 'fixture executable' > "$app_dir/pdf-tool-linux-amd64"
    : > "$app_dir/离线运行环境/runtime-amd64.tar.gz"
    : > "$app_dir/离线运行环境/runtime-amd64.sha256"
    : > "$case_dir/events.log"
    if [ "$scenario" = ready ]; then
        mkdir -p "$runtime_root/opt/pdf-tool" "$runtime_root/home/pdftool" "$runtime_root/tmp" "$runtime_root/dev" "$runtime_root/proc"
        : > "$runtime_root/.pdf-runtime-ready"
    fi
    (
        export PATH="$case_dir/mockbin:$ORIGINAL_PATH"
        export MOCK_LOG="$case_dir/events.log" MOCK_ROOT="$runtime_root"
        umask 077
        sh "$app_dir/start-offline.sh" --listen 127.0.0.1:8787
    ) > "$case_dir/stdout.log" 2> "$case_dir/stderr.log"
    grep -F "chmod|755|$runtime_root|" "$case_dir/events.log" >/dev/null
    grep -F "chroot|--userspec=65534:65534|$runtime_root|" "$case_dir/events.log" >/dev/null
    if [ "$scenario" = fresh ]; then
        grep -F 'tar|' "$case_dir/events.log" >/dev/null
    elif grep -F 'tar|' "$case_dir/events.log" >/dev/null; then
        echo 'FAIL: existing runtime unexpectedly re-extracted' >&2
        exit 1
    fi
    echo "PASS: root traversal permission, $scenario branch (simulated OS)"
}
test_readiness_order() {
    local scenario=$1
    local case_dir="$RUN_ROOT/readiness-$scenario"
    local app_dir="$case_dir/app"
    local linux_dir="$app_dir/exe/信创Linux"
    local deployment_dir="$app_dir/html/部署配置"
    mkdir -p "$linux_dir/离线运行环境" "$deployment_dir" "$case_dir/state"
    make_mocks "$case_dir"
    cp "$PROJECT/html/部署配置/启动Word服务-Linux.sh" "$deployment_dir/start.sh"
    : > "$linux_dir/离线运行环境/runtime-amd64.tar.gz"
    echo 'PRETTY_NAME="Alibaba Cloud Linux 3"' > "$case_dir/os-release"
    : > "$case_dir/events.log"
    cat > "$linux_dir/pdf-tool-linux-amd64" <<'PROGRAM'
#!/bin/sh
set -eu
case "${1:-}" in
    --check-service)
        echo check-service >> "$MOCK_LOG"
        [ "$MOCK_SCENARIO" != failure ] || exit 1
        [ -f "$MOCK_CASE/service-started" ] || exit 1
        : > "$MOCK_CASE/check-succeeded"
        echo check-ready >> "$MOCK_LOG" ;;
    --setup-nginx)
        echo setup-nginx >> "$MOCK_LOG"
        [ -f "$MOCK_CASE/check-succeeded" ] || exit 78
        : > "$MOCK_CASE/setup-finished" ;;
    *) exit 2 ;;
esac
PROGRAM
    cat > "$linux_dir/start.sh" <<'SERVICE'
#!/bin/sh
set -eu
echo service-start >> "$MOCK_LOG"
[ "$MOCK_SCENARIO" != failure ] || exit 42
: > "$MOCK_CASE/service-started"
attempt=0
while [ "$attempt" -lt 120 ]; do
    [ ! -f "$MOCK_CASE/setup-finished" ] || exit 0
    attempt=$((attempt + 1))
    sleep 0.05
done
exit 43
SERVICE
    chmod +x "$linux_dir/pdf-tool-linux-amd64" "$linux_dir/start.sh"
    local exit_code=0
    (
        export PATH="$case_dir/mockbin:$ORIGINAL_PATH"
        export MOCK_LOG="$case_dir/events.log" MOCK_ROOT="$case_dir/unused-runtime-root"
        export MOCK_CASE="$case_dir" MOCK_SCENARIO="$scenario"
        export PDF_APP_ROOT="$app_dir" PDF_WORD_STATE_DIR="$case_dir/state"
        export PDF_OS_RELEASE_FILE="$case_dir/os-release" PDF_TOOL_TOKEN='mock-regression-token-20261109'
        sh "$deployment_dir/start.sh"
    ) > "$case_dir/stdout.log" 2> "$case_dir/stderr.log" || exit_code=$?
    if [ "$scenario" = failure ]; then
        [ "$exit_code" -ne 0 ] || { echo 'FAIL: unsuccessful service accepted'; exit 1; }
        if grep -Fx setup-nginx "$case_dir/events.log" >/dev/null; then
            echo 'FAIL: failed service still configured Nginx' >&2
            exit 1
        fi
    else
        if [ "$exit_code" -ne 0 ]; then cat "$case_dir/stderr.log" >&2; exit "$exit_code"; fi
        grep -Fx check-ready "$case_dir/events.log" >/dev/null
        grep -Fx setup-nginx "$case_dir/events.log" >/dev/null
    fi
    echo "PASS: service readiness ordering, $scenario branch (simulated OS)"
}
test_runtime_permissions fresh
test_runtime_permissions ready
test_readiness_order failure
test_readiness_order success
echo "Results saved in $RUN_ROOT"
