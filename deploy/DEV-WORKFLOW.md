# 二开开发与发布流程

> 面向 agent / 新接手的人：本文所有命令均可直接执行，路径基于本机 `F:\WorkBuddyProjects\docker\WeKnora`。
> 另一台机器只需把路径换成自己的 clone 位置。

---

## 1. 环境拓扑

| | 生产 | 测试栈（本机） | 开发机（另一台） |
|---|---|---|---|
| 前端 | `http://172.20.19.101/` (:80) | `http://172.20.19.101:5174/` | `http://<开发机IP>:5174/` |
| API | `:8080` | `:8081` | `:<端口>` |
| 代码来源 | **镜像**（构建时 COPY） | **bind mount 实时编译** | bind mount 实时编译 |
| 数据库 | `WeKnora-postgres` 卷 | `wktc-postgres` 卷 | 自己的一套 |
| Compose | `docker-compose.yml` + override | `deploy/devstack/docker-compose.devstack.yml` | 同左 |
| 项目名 | `weknora` | `wktc`（容器前缀 `wktc-`） | `wktc` |

**核心事实**：生产与测试**共用同一份源码目录**。改文件只影响测试；生产必须重新构建镜像。

---

## 2. 三条硬约束（踩过的坑，别再试）

| 约束 | 现象 | 应对 |
|---|---|---|
| 9p 覆盖写不同步 mtime | 容器内 `air` / `chokidar` / `find -newer` 看不见宿主机保存，改代码不重编译 | 手动 `docker restart wktc-app`；或常驻 `pwsh -File deploy/devstack/devstack-watch.ps1` |
| `VITE_*` 是构建期内联 | 运行时改 `.env` 对前端无效 | 必须 `--build-arg` 传入后重新构建前端 |
| 本机 apt 带宽 117–402 KB/s | `docker compose build app` 必然超时（runtime 阶段要 250MB+） | **app 镜像走 CI**；前端可本地构建（2 分钟） |

---

## 3. 流程 A：本机二开自测

```bash
cd /f/WorkBuddyProjects/docker/WeKnora

# 1. 起测试栈（若未运行）
docker compose -f deploy/devstack/docker-compose.devstack.yml up -d

# 2. 改代码（分支 main-synced）

# 3. 后端生效（约 40-60s 重编译）
docker restart wktc-app
docker logs -f wktc-app | grep "restarted pid="

# 4. 前端生效（约 5-8s）
docker restart wktc-frontend

# 5. 验证
curl -s http://127.0.0.1:8081/health            # {"status":"ok"}
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:5174/
```

测试数据隔离：dev 库独立（`wktc-postgres`），造队列数据用 `tools/asynq-seed`：

```bash
docker cp "F:/WorkBuddyProjects/docker/tools/asynq-seed/asynq-seed.linux" wktc-app:/tmp/
docker exec wktc-app /tmp/asynq-seed -redis wktc-redis:6379 -password devpass -mode all -count 3
```

---

## 4. 流程 B：跨机开发

开发机前置：Docker Desktop、Git、Go 1.26、Node 22。

```bash
git clone git@github.com:LeonYe612/WeKnora.git
cd WeKnora && git checkout main-synced

# .env.devstack 被 .git/info/exclude 忽略，不随仓库走，需自己建
cp .env.example deploy/devstack/.env.devstack
# 至少改这几项指向本机生产机的模型服务：
#   EMBEDDING_BASE_URL=http://172.20.19.101:7997/v1
#   RERANK_BASE_URL=http://172.20.19.101:7997/v1
#   OLLAMA_BASE_URL=http://172.20.19.101:11434
#   DEFAULT_GRAPH_ENABLED=true

docker compose -f deploy/devstack/docker-compose.devstack.yml up -d
# 访问 http://<开发机IP>:5174
```

模型服务不装在开发机，走 LAN 到 172.20.19.101（防火墙规则 `dev-7997` / `dev-11434` 已放行）。

**只改前端时的省事做法**：开发机只跑 `npm run dev`（vite :5173），`vite.config.ts` 的 proxy 指向 `http://172.20.19.101:8081`。代价：改后端逻辑无效。

---

## 5. 流程 C：发布到生产

```bash
# ── 开发机 ──
git add -A && git commit -m "feat: ..."
git push origin main-synced
# 自动触发 .github/workflows/build-fork-image.yml，约 20-30 分钟
# 产物：ghcr.io/leonye612/weknora-app:<sha7> 和 weknora-ui:<sha7>
```

查 CI 状态（仓库 public，需带认证避免限流）：

```bash
TOKEN=$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill | grep '^password=' | cut -d= -f2)
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://api.github.com/repos/LeonYe612/WeKnora/actions/runs?per_page=3" \
  | grep -oE '"status": "[a-z_]*"|"conclusion": [a-z"]*'
```

```bash
# ── 生产机（172.20.19.101）──
SHA=<CI 的 sha7>
docker pull ghcr.io/leonye612/weknora-app:$SHA
docker pull ghcr.io/leonye612/weknora-ui:$SHA
docker tag  ghcr.io/leonye612/weknora-app:$SHA wechatopenai/weknora-app:latest
docker tag  ghcr.io/leonye612/weknora-ui:$SHA  wechatopenai/weknora-ui:latest

cd /f/WorkBuddyProjects/docker/WeKnora
# 前后端必须一起切，否则版本错配
docker compose up -d --force-recreate app frontend
```

镜像 package 已设 public，生产机匿名可拉，无需凭据。

**若改动含数据库迁移**：先在测试栈验证，再切生产（app 启动时 `AUTO_MIGRATE=true` 自动执行）。历史上 134→137 三个迁移实测共 42ms。

---

## 6. 验证清单

| 检查项 | 命令 | 预期 |
|---|---|---|
| 容器健康 | `docker ps --filter name=WeKnora` | app `(healthy)` |
| 二开代码在镜像内 | `docker exec WeKnora-app grep -c "system/admin/runtime/tasks" /app/WeKnora` | `6` |
| schema 版本 | `docker exec WeKnora-postgres psql -U postgres -d WeKnora -tAc "select version,dirty from schema_migrations"` | `137\|f` |
| 前端 | `curl -o /dev/null -w "%{http_code}" http://127.0.0.1/` | `200` |
| 任务台路由 | `curl -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/api/v1/system/admin/runtime/tasks` | `401`（未认证=路由存在） |
| KG 默认开关 | `docker exec WeKnora-app env \| grep DEFAULT_GRAPH` | `=true` |

---

## 7. 回滚

```bash
docker tag wechatopenai/weknora-app:pre-upgrade wechatopenai/weknora-app:latest
docker tag wechatopenai/weknora-ui:pre-upgrade  wechatopenai/weknora-ui:latest
docker compose up -d --force-recreate app frontend
```

**若已发生迁移**，旧镜像只认旧 schema，必须连库一起回：

```bash
docker exec -i WeKnora-postgres pg_restore -U postgres -d WeKnora \
  --clean --if-exists /tmp/pre_upgrade_134.dump
```

`pre-upgrade` 锚点由 `docker tag`（app）与 `docker commit WeKnora-frontend`（ui，因为旧 tag 已被新构建覆盖）建立。

---

## 8. 本地定制文件索引（合并上游时重点检查）

| 文件 | 改动性质 |
|---|---|
| `internal/types/indexing_strategy.go` | 新增 `atomic.Bool` + `SetDefaultGraphEnabled()`，默认值仍为 false |
| `cmd/server/main.go` | 启动时读 `DEFAULT_GRAPH_ENABLED` 注入 |
| `frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue` | 默认值改为读 `VITE_DEFAULT_GRAPH_ENABLED` |
| `frontend/Dockerfile` | 暴露 `ARG VITE_DEFAULT_GRAPH_ENABLED` |
| `frontend/src/views/system/RuntimeQueues.vue` | 新增任务台 tab |
| `frontend/src/views/system/RuntimeTaskConsole.vue` | 新增文件（任务台组件） |
| `internal/handler/*` + `api/v1/system/admin/runtime/tasks` | 跨队列任务聚合 API |
| `docker-compose.override.yml` | docker.sock、builtin_models 挂载、GOPROXY_ARG |
| `deploy/devstack/**` | 独立测试栈（上游无此目录） |
| `config/builtin_models.yaml` | 声明 Embedding/Rerank，不含 gpt-4o-mini |
| `.github/workflows/build-fork-image.yml` | fork 专用 CI（ghcr + amd64） |

原则：**不修改上游函数的默认语义，只加运行时开关**，把冲突面从"函数体"降到"一行调用"。

---

## 9. 相关脚本

| 路径 | 用途 |
|---|---|
| `deploy/devstack/build-local.sh` | 本地构建（`app` / `frontend` / `all`，`SLIM=1` 精简 runtime 依赖） |
| `deploy/devstack/devstack-watch.ps1` | 宿主机文件监听，触发容器内重编译 |
| `deploy/devstack/docker-compose.devstack.yml` | 测试栈编排 |
| `tools/asynq-seed/` | 造任务台测试数据 |
| `deploy/UPGRADE-RUNBOOK-v0.9.1.html` | 本次升级的一页式记录（可打印） |
