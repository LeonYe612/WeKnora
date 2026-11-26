#!/usr/bin/env bash
# 把生产库的「配置 / 身份」小表导入 dev 库，用于验证二开功能。
# 不导入业务大表（chunks / embeddings / knowledge_processing_spans / task_dead_letters 等）。
#
# 前置：dev 库已由 tc-app 启动时 AUTO_MIGRATE 建好 schema。
# 用法：bash deploy/devstack/seed-config.sh
set -euo pipefail

SRC=${SRC:-WeKnora-postgres}
DST=${DST:-wktc-postgres}
DB=${DB:-WeKnora}
DUMP=/tmp/wktc-config-seed.sql

TABLES=(
  tenants
  organizations
  users
  tenant_members
  organization_tenant_members
  tenant_invitations
  models
  storage_backends
  system_settings
  knowledge_bases
  knowledge_tags
  kb_shares
  agent_prompts
  custom_agents
  agent_shares
)

args=()
for t in "${TABLES[@]}"; do args+=(-t "public.$t"); done

echo "[1/3] dumping config tables from $SRC ..."
docker exec "$SRC" pg_dump -U postgres -d "$DB" \
  --data-only --disable-triggers --no-owner --no-privileges \
  "${args[@]}" > "$DUMP"
echo "      dump size: $(wc -c < "$DUMP") bytes"

echo "[2/3] truncating target tables in $DST ..."
{
  echo "SET session_replication_role = replica;"
  for t in "${TABLES[@]}"; do
    echo "TRUNCATE TABLE public.$t CASCADE;"
  done
  echo "SET session_replication_role = origin;"
} | docker exec -i "$DST" psql -U postgres -d "$DB" -v ON_ERROR_STOP=1 -q

echo "[3/3] importing ..."
docker exec -i "$DST" psql -U postgres -d "$DB" -v ON_ERROR_STOP=1 -q < "$DUMP"

echo "done. row counts:"
for t in tenants users models knowledge_bases system_settings; do
  n=$(docker exec "$DST" psql -U postgres -d "$DB" -tAc "select count(*) from public.$t")
  echo "  $t = $n"
done
