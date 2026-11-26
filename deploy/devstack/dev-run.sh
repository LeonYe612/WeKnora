#!/bin/sh
# 后端热重载循环（轮询实现）
#
# 为什么不用 air：本机 Docker Desktop 的 bind mount 走 9p/virtiofs，
# inotify 事件不会从 Windows 侧传出（实测容器内 touch 也不触发）。
# 因此改成按 mtime 轮询 —— 只 scan internal/ 与 cmd/，2s 间隔，开销可控。
# 与 gowatch 的差别：编译失败时保留旧进程继续服务，方便边调边测。
set -u

APP_BIN=/tmp/air/WeKnora
WATCH_DIRS="/app/internal /app/cmd"
POLL_SECONDS=${POLL_SECONDS:-2}
# 宿主机 watcher 探测到 .go 变化后 touch 此文件。
# 原因：Windows→容器的 bind mount(9p/virtiofs) 覆盖写只同步数据、不同步 mtime，
# 所以容器内 stat/find 看不到宿主机编辑器的保存；inotify 也完全不触发。
# 由 devstack-watch.ps1 在 Windows 侧用 FileSystemWatcher 捕获事件后显式 touch。
TRIGGER=/tmp/reload-trigger

mkdir -p /tmp/air

build_and_start() {
    if ! go build -o "$APP_BIN.new" ./cmd/server; then
        echo "[dev-run] build failed, keeping previous binary running" >&2
        rm -f "$APP_BIN.new"
        return 1
    fi
    mv "$APP_BIN.new" "$APP_BIN"
    if [ -n "${APP_PID:-}" ]; then
        kill "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
    "$APP_BIN" &
    APP_PID=$!
    echo "[dev-run] restarted pid=$APP_PID at $(date +%T)"
}

needs_rebuild() {
    if [ -f "$TRIGGER" ] && [ "$TRIGGER" -nt "$APP_BIN" ]; then
        return 0
    fi
    for d in $WATCH_DIRS; do
        if [ -n "$(find "$d" -name '*.go' -newer "$APP_BIN" -print -quit 2>/dev/null)" ]; then
            return 0
        fi
    done
    return 1
}

if [ ! -x "$APP_BIN" ] || needs_rebuild; then
    build_and_start
else
    "$APP_BIN" &
    APP_PID=$!
    echo "[dev-run] started existing binary pid=$APP_PID"
fi

while true; do
    sleep "$POLL_SECONDS"
    if needs_rebuild; then
        echo "[dev-run] change detected, rebuilding ..."
        build_and_start || true
    fi
done
