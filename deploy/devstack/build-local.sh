#!/usr/bin/env bash
# 本地构建 WeKnora 生产镜像
#
# 为什么不直接 docker compose build：
#   本机访问 docker.io 的 auth.docker.io 被阻断（TCP 超时），任何基础镜像都拉不下来。
#   上游 Dockerfile 又用 @sha256 钉死基础镜像 digest，单纯把镜像预拉并打 tag 也绕不过去
#   （BuildKit 仍会回 registry 解析 digest）。
#
# 做法：不改仓库里的 Dockerfile，构建时生成一份临时副本，把基础镜像换成 daocloud 镜像源。
#   - 去掉 @sha256 摘要（镜像源不保证同一 digest 可寻址）
#   - 记录到设计债：构建结果因此不可字节级复现，与上游 CI 产物存在漂移风险
#
# 用法：bash deploy/devstack/build-local.sh [app|frontend|all]
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MIRROR="${MIRROR:-docker.m.daocloud.io/library}"
TARGET="${1:-all}"

rewrite() {
    local src="$1" dst="$2"
    sed -E \
        -e 's#^FROM ([^ ]+ )?(node|golang|debian|nginx):#FROM \1'"${MIRROR}"'/\2:#' \
        -e 's#@sha256:[0-9a-f]{64}##g' \
        "$src" > "$dst"
    echo "--- $dst 的基础镜像 ---"
    grep -E "^FROM" "$dst"
}

build_app() {
    local tmp="$ROOT/.tmp-Dockerfile.app"
    rewrite "$ROOT/docker/Dockerfile.app" "$tmp"
    docker build \
        -f "$tmp" \
        --build-arg "APK_MIRROR_ARG=mirrors.tencent.com" \
        --build-arg "GOPROXY_ARG=https://goproxy.cn,direct" \
        --build-arg "WITH_ANYDOC=${WITH_ANYDOC:-1}" \
        --build-arg "VERSION_ARG=$(cat "$ROOT/VERSION" 2>/dev/null || echo unknown)" \
        --build-arg "COMMIT_ID_ARG=$(git -C "$ROOT" rev-parse --short HEAD)" \
        --build-arg "BUILD_TIME_ARG=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        -t wechatopenai/weknora-app:latest \
        "$ROOT"
    rm -f "$tmp"
}

build_frontend() {
    local tmp="$ROOT/.tmp-Dockerfile.frontend"
    rewrite "$ROOT/frontend/Dockerfile" "$tmp"
    docker build \
        -f "$tmp" \
        --build-arg "NPM_REGISTRY=${NPM_REGISTRY:-}" \
        --build-arg "VITE_FRONTEND_COMMIT=$(git -C "$ROOT" rev-parse --short HEAD)" \
        -t wechatopenai/weknora-ui:latest \
        "$ROOT/frontend"
    rm -f "$tmp"
}

case "$TARGET" in
    app)      build_app ;;
    frontend) build_frontend ;;
    all)      build_app; build_frontend ;;
    *) echo "unknown target: $TARGET" >&2; exit 1 ;;
esac

echo "done."
