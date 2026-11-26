#!/usr/bin/env bash
# 本地构建 WeKnora 生产镜像
#
# 本机两个环境限制决定了这个脚本的存在：
#
# 1) docker.io 的 auth.docker.io 被阻断（TCP 超时），基础镜像拉不下来；
#    上游 Dockerfile 又用 @sha256 钉死 digest，预拉 tag 也绕不过去
#    （BuildKit 仍会回 registry 解析 digest）。
#    → 构建时生成临时 Dockerfile，基础镜像换成 daocloud 镜像源并去掉 digest。
#
# 2) Clash TUN 会把 buildkit 的出网劫持到 fake-ip（198.18.x.x）并返 502，apt 全挂。
#    运行中的容器不受影响（实测同源 curl 返回 200），只有构建期出问题。
#    → 通过 HTTP(S)_PROXY 走 Docker Desktop 内置代理（http.docker.internal:3128）；
#      国内源（goproxy.cn / aliyun / npm）用 NO_PROXY 排除，保持直连。
#
# 代价：构建结果不可与上游 CI 产物字节级复现（基础镜像 digest 被去掉）。
#
# 用法：bash deploy/devstack/build-local.sh [app|frontend|all]
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MIRROR="${MIRROR:-docker.m.daocloud.io/library}"
TARGET="${1:-all}"
BUILD_PROXY="${BUILD_PROXY:-http://http.docker.internal:3128}"
NO_PROXY_LIST="localhost,127.0.0.1,docker.internal,goproxy.cn,mirrors.aliyun.com,registry.npmjs.org,registry.npmmirror.com"
# 精简 runtime 依赖：本机到 apt 源实测 117-402 KB/s，上游要下 250MB+，
# 必然超时。SLIM=1 时去掉 vim/wget/nodejs/npm/ffmpeg/mysql-client，
# 只保留运行时必需项（tzdata/curl/bash/gosu/libsqlite3/python3/build-essential）。
# 代价：镜像不含 ffmpeg/nodejs，音视频解析与 JS 工具链不可用。
SLIM="${SLIM:-1}"

rewrite() {
    local src="$1" dst="$2"
    sed -E \
        -e 's#^FROM ([^ ]+ )?(node|golang|debian|nginx):#FROM \1'"${MIRROR}"'/\2:#' \
        -e 's#@sha256:[0-9a-f]{64}##g' \
        -e 's#apt-get install#apt-get -o Acquire::Retries=6 -o Acquire::http::Timeout=30 install#g' \
        -e 's#^RUN if #RUN --mount=type=cache,target=/var/cache/apt/archives if #' \
        -e 's#apt-get clean#true#g' \
        -e 's#go run cmd/download/duckdb/duckdb.go#go run cmd/download/duckdb/duckdb.go || echo "[build-local] duckdb ext preload skipped"#' \
        "$src" > "$dst"

    if [ "$SLIM" = "1" ]; then
        python - "$dst" <<'PYEOF'
import io, sys
p = sys.argv[1]
lines = io.open(p, encoding='utf-8').read().split('\n')
out, hit = [], 0
for line in lines:
    s = line.strip()
    if 'postgresql-client default-mysql-client' in line and 'build-essential' in line:
        out.append('        build-essential tzdata sed curl bash \\')
        hit += 1
        continue
    if s == 'nodejs npm \\':
        hit += 1
        continue
    if s == 'gosu \\':
        continue
    if s == 'ffmpeg qpdf && \\':
        out.append('        gosu && \\')
        hit += 1
        continue
    out.append(line)
if hit >= 2:
    io.open(p, 'w', encoding='utf-8').write('\n'.join(out))
    print("--- runtime apt list slimmed (%d edits) ---" % hit)
else:
    print("--- WARN: runtime apt list not matched (%d edits), keeping full set ---" % hit)
PYEOF
    fi

    echo "--- $dst 的基础镜像 ---"
    grep -E "^FROM" "$dst"
}

build_app() {
    local tmp="$ROOT/.tmp-Dockerfile.app"
    rewrite "$ROOT/docker/Dockerfile.app" "$tmp"
    docker build \
        -f "$tmp" \
        --build-arg "HTTP_PROXY=${BUILD_PROXY}" \
        --build-arg "HTTPS_PROXY=${BUILD_PROXY}" \
        --build-arg "NO_PROXY=${NO_PROXY_LIST}" \
        --build-arg "APK_MIRROR_ARG=mirrors.aliyun.com" \
        --build-arg "GOPROXY_ARG=https://goproxy.cn,direct" \
        --build-arg "WITH_ANYDOC=${WITH_ANYDOC:-1}" \
        --build-arg "VERSION_ARG=$(tr -d '\r\n' < "$ROOT/VERSION" 2>/dev/null || echo unknown)" \
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
        --build-arg "HTTP_PROXY=${BUILD_PROXY}" \
        --build-arg "HTTPS_PROXY=${BUILD_PROXY}" \
        --build-arg "NO_PROXY=localhost,127.0.0.1,docker.internal,registry.npmjs.org,registry.npmmirror.com" \
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
