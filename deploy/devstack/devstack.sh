#!/usr/bin/env bash
# WeKnora 二开 dev stack 控制脚本
# 用法: bash deploy/devstack/devstack.sh <command>
#   up       启动全部（pg/redis/neo4j/app/frontend）
#   infra    只启动中间件
#   down     停止（保留数据卷）
#   ps       查看状态
#   logs [svc] 跟随日志
#   restart [svc]
#   reload   重新编译后端（air 收不到文件事件时的兜底，约 10-20s）
#   sh       进入 app 容器
#   seed     导入生产配置小表
#   build    重建 dev 镜像
#   destroy  删除栈与全部 dev 卷（不可逆）
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FILE="$HERE/docker-compose.devstack.yml"
DC=(docker compose -f "$FILE")

cmd="${1:-up}"
shift || true

case "$cmd" in
  up)       "${DC[@]}" up -d ;;
  infra)    "${DC[@]}" up -d tc-postgres tc-redis tc-neo4j ;;
  down)     "${DC[@]}" down ;;
  ps)       "${DC[@]}" ps ;;
  logs)     "${DC[@]}" logs -f --tail=80 "${@:-}" ;;
  restart)  "${DC[@]}" restart "${@:-}" ;;
  reload)   "${DC[@]}" restart tc-app ;;
  sh)       docker exec -it wktc-app bash ;;
  seed)     bash "$HERE/seed-config.sh" ;;
  build)    "${DC[@]}" build tc-app ;;
  destroy)  "${DC[@]}" down -v ;;
  *)
    echo "unknown command: $cmd" >&2
    sed -n '2,16p' "${BASH_SOURCE[0]}"
    exit 1
    ;;
esac
