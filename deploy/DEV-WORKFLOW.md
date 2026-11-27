# 二开开发与发布流程

> 面向 agent / 新接手的人：本文所有命令可直接执行。
> 文中统一用 `$REPO` 指代仓库根目录，**不出现任何硬编码绝对路径** —— 第一件事是把 `$REPO` 设成你自己的 clone 位置。

---

## 阅读前：本文按 Windows 编写，macOS 该怎么读

这份文档诞生在 Windows + WSL2 + Docker Desktop 环境，部分内容和 macOS 不通用。**先读这张表，再动手**。

### 一、立刻要替换的东西

| 文档里出现 | Windows 含义 | macOS 怎么处理 |
|---|---|---|
| `$REPO` 的示例值 `F:\WorkBuddyProjects\docker\WeKnora` | 仓库根（盘符写法） | 换成 `~/code/WeKnora` 之类，例如 `export REPO=~/code/WeKnora` |
| `/f/WorkBuddyProjects/...` | Git Bash 的盘符映射 | 不存在，用 `/Users/<you>/...` |
| `pwsh -File xxx.ps1` | PowerShell 脚本 | macOS 无 PowerShell，见下方「Windows 专属」 |
| `C:\Program Files\...` | 应用安装路径 | `/Applications/...` |
| LAN IP `172.20.19.101` | 本机所在网段的固定 IP | 换成你 Mac 的 IP（`ipconfig getifaddr en0`） |
| 防火墙规则 `dev-<端口>` | Windows 防火墙入站规则 | macOS 没有这套；Docker Desktop 默认已放行本地端口，跨机访问用 `pf` 或临时关防火墙 |

### 二、必须设一次的环境变量

```bash
# Git Bash (Windows) / WSL
export REPO=/f/WorkBuddyProjects/docker/WeKnora

# macOS
export REPO=~/code/WeKnora

cd "$REPO"
```
后文所有命令都假设 `$REPO` 已设好。

### 三、行尾（最容易踩的一脚）

仓库在 Windows 上可能带着 CRLF。macOS 上执行 `.sh` 会报：

```
bash: ./xxx.sh: /bin/bash^M: bad interpreter: No such file or directory
```

处理任选一种：

```bash
# 方式 1：就地清掉 CRLF
sed -i '' 's/\r$//' deploy/devstack/*.sh

# 方式 2：让 git 按平台转换后重新取出
git config core.autocrlf input && git rm --cached -r . && git reset --hard
```

### 四、Windows 专属内容（macOS 直接跳过）

| 章节 / 内容 | 为什么 Mac 用不了 | Mac 替代做法 |
|---|---|---|
| 第 2 节「9p 覆盖写不同步 mtime」 | WSL2 的 9p 文件系统问题，Docker Desktop for Mac 同样有 mtime 延迟但表现不同 | 改完代码直接 `docker restart`，别指望热重载 |
| `deploy/devstack/devstack-watch.ps1` | PowerShell 脚本 | 用 `fswatch -o src | xargs -n1 -I{} docker restart wktc-app`，或手动重启 |
| release-agent 的 nssm 托管 | nssm 是 Windows 服务管理器 | macOS 用 `launchd`（plist）或 `supervisord` |

### 五、macOS 反而是好事的地方

- `host.docker.internal` 在 Docker Desktop for Mac 上**原生可用**，不需要额外加 `--add-host`。
- 构建缓存和 apt 带宽问题不存在（Apple Silicon 记得把 CI 的 `platforms` 改成 `linux/arm64`，否则镜像架构不匹配）。

---

## 0. 目录与项目约定

| 项 | 值 |
|---|---|
| 仓库根 | `$REPO` |
| 唯一 remote | `origin` = `https://github.com/LeonYe612/WeKnora.git` |
| 生产 compose 工作目录 | `$REPO` |
| 宿主工具目录 | `$REPO/..`（与仓库平级，如 `../tools`、`../weknora-release-agent`） |

`docker cp`、`pg_dump` 之类需要绝对路径时，一律写成 `"$REPO/..."`，不要写字面量。

---

## 1. 分支模型（2026-11-27 定）

| 分支 | 谁能写 | 职责 | 规则 |
|---|---|---|---|
| `main` | 只有同步脚本 | 官方 `Tencent/WeKnora` 的纯净镜像 | 只允许 ff-only，禁止任何本地 commit |
| `dev` | 你 | 唯一开发线，全部二开提交 | `git push origin dev`，正常无需 force |

官方仓库**不是** remote；`git remote -v` 只应出现 `origin` = fork。**没有钩子会拦这一步** —— 仓库自带的 `scripts/git-hooks/pre-push` 只做 gofmt / `go vet` / `go test` / `go build`（对齐 CI），不含任何远端校验；防误推靠的是「根本没配 Tencent remote」，新增 remote 前先想清楚。

### 日常（99% 的时间）

```bash
git switch dev
# 改代码
git add -A && git commit -m "..."
git push origin dev        # 触发 build-fork-image.yml 出镜像
```

### 吸收官方更新（决定升级基线时才做）

```bash
# 脚本在仓库的上级目录，路径随 $REPO 走
bash "$REPO/../weknora-sync-upstream.sh"
git switch dev
git merge main             # 用 merge，不用 rebase
git push origin dev
```

**为什么 merge 不用 rebase**：rebase 会重写全部二开提交的 SHA，每次同步都要 `--force-with-lease`，且 CI 因 SHA 全变而重建全部镜像。merge 只多一个合并提交，SHA 稳定，出包可追溯。

### 三条红线

- 不在 `main` 上提交 —— 一旦有，同步脚本的 `ff-only` 会失败并报错。
- 不用 `dev/local` 这类子路径分支名 —— 会占用 `refs/heads/dev` 命名空间，导致 `git branch -m X dev` 失败。
- 不在 GitHub 页面向 `Tencent/WeKnora` 提 PR —— 那是向官方提交，本地任何钩子都管不到。

### Git hooks：装哪个、为什么只能装一个

`core.hooksPath` 只能指向**一个**目录，仓库里有两套互斥的钩子，别混：

| 目录 | 来源 | 内容 | 装法 |
|---|---|---|---|
| `scripts/git-hooks/`（tracked，上游自带） | `website-docs/06-development/01-dev-guide.md` | pre-commit：空白字符 / gofmt 自动修 / 已装则跑 golangci-lint；**pre-push：对齐 CI 的 gofmt + `go vet` / `go test` / `go build`** | `./scripts/install-git-hooks.sh`（= `git config core.hooksPath scripts/git-hooks`） |
| `.githooks/`（git-ignored，`doc_dedup_guard.py init` 部署） | WFGameAI 文档治理工具，非本仓库契约 | 只有 pre-commit：docs/deliverables 双源碰撞、文件命名、头部日期等 | `python3 scripts/doc_dedup_guard.py init` |

- 推 `dev` 前想要 CI 同款检查 → 装第一套。`SKIP_HOOKS=1` 跳过一次，`HOOK_SKIP_TEST=1` 只跳 pre-push 的测试。
- 两套不能同时生效：装第二套会把第一套顶掉（`core.hooksPath` 被改写），要换回来就重跑对应安装命令。
- **`scripts/git-hooks/pre-push` 不校验远端**。别指望它拦「推错仓库」。
- `scripts/test-git-hooks.sh` 由 CI（`.github/workflows/app.yml`）执行；本机改钩子后手动跑一遍。

---

## 2. 环境拓扑

| | 生产 | 测试栈（同机） | 开发机（另一台） |
|---|---|---|---|
| 前端 | `http://$HOST/` (:80) | `http://$HOST:5174/` | `http://localhost:5173/`（vite 跑宿主机） |
| API | 容器内 `:8080`，**不发布到宿主**；局域网经前端 nginx `:80` 转发 `/api/` | `:8081`（Mac 本机 devstack 为 `:8091`，见第 4 节） | `:8080`（Go 跑宿主机，`APP_PORT` 可改） |
| 代码来源 | **镜像**（构建时 COPY） | **bind mount 实时编译** | 宿主机直跑（`make dev-app`，air 热重载） |
| 数据库 | `WeKnora-postgres`（宿主 :5432） | `wktc-postgres`（宿主 :5433） | 自己的一套（`*-dev` 容器），见流程 B / 第 11 节 |
| 图库 | `WeKnora-neo4j`（:7474/7687） | `wktc-neo4j`（:7475/7688） | 自己的一套 `*-dev` 容器，见第 12 节 |
| Compose | `docker-compose.yml` + override | `deploy/devstack/docker-compose.devstack.yml` | `docker-compose.dev.yml`（基础设施容器）+ app/前端跑宿主机 |
| 项目名 | `weknora` | `wktc`（容器前缀 `wktc-`） | `weknora`（容器后缀 `-dev`） |

`$HOST` 指部署机的 LAN IP（Windows 侧示例为 `172.20.19.101`）。

**核心事实**：生产与测试**共用同一份源码目录**。改文件只影响测试；生产必须重新构建镜像。

---

## 3. 三条硬约束（踩过的坑，别再试）

| 约束 | 现象 | 应对 |
|---|---|---|
| bind mount 上 inotify/9p 不可靠 | 容器内 `air` / `chokidar` 看不见宿主机保存，改代码不重编译 | 手动 `docker restart wktc-app`；或跑文件监听脚本（Windows：`pwsh -File deploy/devstack/devstack-watch.ps1`；macOS：`fswatch`） |
| `VITE_*` 是构建期内联 | 运行时改 `.env` 对前端无效 | 必须 `--build-arg` 传入后重新构建前端 |
| 出海带宽差（Windows 侧实测 apt 117–402 KB/s） | `docker compose build app` 必然超时（runtime 阶段要 250MB+） | **app 镜像一律走 CI**；前端可本地构建 |

---

## 4. 流程 A：本机二开自测

```bash
cd "$REPO"

bash deploy/devstack/devstack.sh ps                # 已在跑就跳过 up

# 1. 起测试栈（若未运行；首次会构建 wktc/app-dev 镜像）
bash deploy/devstack/devstack.sh up

# 2. 改代码（分支 dev）

# 3. 后端生效：容器内 dev-run.sh 每 2s 轮询 internal/ cmd/ 下的 *.go，保存后自动重编译
#    （整仓冷编译要几十秒到数分钟，看 build cache；编译失败会保留旧进程继续服务，日志里有 build failed）
docker logs -f wktc-app 2>&1 | grep -E "dev-run"
#    只改 config/ migrations/ compose 等 .go 之外的文件不会触发，手动重启：
bash deploy/devstack/devstack.sh reload

# 4. 前端：vite 1s 轮询自动生效，不用重启

# 5. 验证（端口见 deploy/devstack/.env.devstack 的 DEVSTACK_APP_PORT，本机当前 8091）
curl -s http://127.0.0.1:8091/health                # {"status":"ok"}
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:5174/
```

> Mac 上如果 shell 里有 `http_proxy` / `all_proxy`（Clash 等），访问 `172.20.19.101` 这类局域网地址的 `curl` **必须加 `--noproxy '*'`**，否则拿到的是代理返回的 502 / 000，不是目标机器的响应。`127.0.0.1` 不受影响。

**docreader 不在测试栈里**：`docker-compose.devstack.yml` 把 `DOCREADER_ADDR` 指到 `docreader:50051`，经 external 网络 `prod-net` 解析——那是**生产 compose 项目的 docreader 容器**。它停了，测试栈上传/解析就失败，而 app 启动时又只连一次（之后日志反复出现 `gRPC ListEngines failed: ... no children to pick from`，`/api/v1/system/parser-engines` 却照常 200，很容易漏看）。处置：

```bash
# 在生产 compose 目录里只拉起 docreader 这一个服务（不动 app/frontend/postgres）
cd <生产 compose 目录> && docker compose up -d docreader        # Mac 本机：~/.local/share/wfgameai-weknora
docker inspect WeKnora-docreader --format '{{.State.Status}} {{.State.Health.Status}}'   # running healthy
bash deploy/devstack/devstack.sh restart tc-app                   # 让 app 重连（启动日志出现 Connected to docreader）
```

测试数据隔离：dev 库独立（`wktc-postgres`），造队列数据用 `../tools/asynq-seed`：

```bash
docker cp "$REPO/../tools/asynq-seed/asynq-seed.linux" wktc-app:/tmp/
docker exec wktc-app /tmp/asynq-seed -redis wktc-redis:6379 -password devpass -mode all -count 3
```

---

## 5. 流程 B：本地独立开发环境（推荐，2026-11-27 定）

开发机（如 Mac）跑**自己的一套**：基础设施用 `docker-compose.dev.yml` 起容器（容器后缀 `-dev`），Go app 和前端直接跑宿主机。**不要**把 `deploy/devstack/` 照搬到开发机——它依赖部署机上生产栈的外部网络 `weknora_WeKnora-network`（`docker-compose.devstack.yml:170`），开发机上不存在；其 `.env.devstack` 也是部署机本地私有的生产 .env 副本，不随仓库走。

开发机前置：Docker Desktop、Git、Go 1.26、Node 22（可选 `air` 热重载）。

### B-1. 起自己的环境

```bash
git clone https://github.com/LeonYe612/WeKnora.git
cd WeKnora && git switch dev
export REPO=$(pwd)

cp .env.example .env
# .env 至少核对这几项（模型服务不装本地，走 LAN 到 $HOST）：
#   DB_DRIVER=postgres  DB_PORT=5432  DB_USER/DB_PASSWORD/DB_NAME 与本地容器一致
#   RETRIEVE_DRIVER=postgres        ← 向量存 PG（ParadeDB），无需 Qdrant/Milvus
#   NEO4J_ENABLE=true
#   EMBEDDING_BASE_URL=http://$HOST:7997/v1
#   RERANK_BASE_URL=http://$HOST:7997/v1
#   OLLAMA_BASE_URL=http://$HOST:11434
#   DEFAULT_GRAPH_ENABLED=true

make dev-start DEV_ARGS="--neo4j --no-langfuse"   # postgres/redis/docreader + neo4j 容器
make dev-app        # 宿主机跑 Go（scripts/dev.sh 自动把容器服务名映射到 127.0.0.1）
make dev-frontend   # vite :5173，代理默认指向本地 :8080
```

说明：
- `dev.sh` 会自动把 `LOCAL_STORAGE_BASE_DIR=/data/files` 改写为仓库内 `.local-data/files`（`scripts/dev.sh:501`），Mac 上无需手动建 `/data`。
- 101 若用 MinIO 存储（看部署机 `.env` 的 `STORAGE_TYPE`），本地加 `--minio` 并配齐 `MINIO_*`；默认 `STORAGE_TYPE=local` 则不用。
- 想省本地资源、直接复用部署机基础设施时，`dev.sh` 支持 `DEV_REMOTE_HOST=$HOST`（`scripts/dev.sh:469`）：跳过本地容器、DB/Redis/Neo4j/docreader 全走 LAN。**代价见下方「直连共享库的三条代价」，日常开发不推荐。**
- 只改前端的省事做法不变：只跑 `npm run dev`，`VITE_DEV_PROXY_TARGET=http://$HOST:8081` 指向测试栈。代价：改后端逻辑无效。

### B-2. 导入 101 测试库的真实数据（快照，非实时）

三样东西要分开搬：**PG 库（含向量）、Neo4j 图谱、原始文件**。`pg_dump` 只覆盖第一样。

```powershell
# —— 101 上（RDP / PowerShell）——
docker exec wktc-postgres pg_dump -U postgres -d WeKnora -Fc > F:\pg-dumps\wktc-test.dump
# 图谱（可选，不搬则本地重建或关闭图谱）：
docker exec wktc-neo4j neo4j-admin database dump neo4j --to-path=/data
# 原始文件（STORAGE_TYPE=local 时在 tc-files 卷；MinIO 则搬对应 bucket）：
docker run --rm -v wktc_tc-files:/src -v F:\pg-dumps:/out alpine tar czf /out/tc-files.tgz -C /src .
```

把三个产物拷回开发机（SMB / RDP 剪贴板均可），然后：

```bash
# —— 开发机 ——
pg_restore -h 127.0.0.1 -p 5432 -U postgres -d WeKnora --no-owner --clean --if-exists wktc-test.dump
tar xzf tc-files.tgz -C "$REPO/.local-data/files"
# Neo4j dump 恢复见 neo4j-admin database load（先停 neo4j 容器内库），或对文档批量重解析重建图谱
```

**不搬文件/图谱的后果**：文档列表和检索（chunks/向量在 PG）正常，但「预览原文/下载」404、图谱检索为空——都不报错，是最容易误以为迁完整的两个坑。

验收：本地登录 → 目标 KB 文档数与 101 测试库一致 → 抽一篇能预览原文、能检索命中、（若搬了图谱）图谱页有数据。

### B-3. 直连共享库的三条代价（选 `DEV_REMOTE_HOST` 前必读）

1. **迁移互踩**：本地 app `AUTO_MIGRATE=true` 且代码比测试栈新时，会直接把共享库 schema 推前进，101 上的旧代码可能立刻报错。共用期间本地设 `AUTO_MIGRATE=false`。
2. **双 worker 抢任务**：101 的 `wktc-app` 和本地 app 消费同一个 Redis 队列。要么停 `wktc-app`，要么本地不启 worker。
3. **数据面不完整**：只连 PG 时图谱（Neo4j）、异步任务（Redis）、文件（101 卷/MinIO）都不在本地，功能残缺但不报错。

### B-4. 与 101 测试栈的分工

- 本地环境：日常开发、破坏性实验、删数据演练（如清理数据源残留）。
- 101 测试栈：对外可访问的联调环境，走流程 A（改文件 + `docker restart wktc-app`）。
- 两边数据独立后，「先在本地演练 → 确认清单 → 再到测试/生产执行」成为默认顺序。

---

## 6. 流程 C：发布到生产

> **首选走 UI 面板**：左下角账号菜单 →「发布」（仅 system admin 可见）→ 选构建 → 发布。
> 面板自动完成 pull / 备份 / 改 digest / 重建 / 健康检查，失败自动回滚。
> 面板依赖宿主 `release-agent`（Windows 下由 nssm 托管为 `WeKnoraReleaseAgent` 服务）。
> 面板不可用时按 `deploy/RELEASE-RUNBOOK.md` 手工执行 —— 那份是权威，本节只列主干。
> 从 Mac 上操作的全流程（前置验证 / 装钩子 / 推送 / 只读验证 / 代理陷阱）见 `deploy/MAC-RELEASE-GUIDE.md`。

```bash
# 1. 推代码，触发 CI（约 10-30 分钟）
cd "$REPO"
git push origin dev

# 2. 查 CI
curl -s "https://api.github.com/repos/LeonYe612/WeKnora/actions/runs?per_page=3" \
  | grep -oE '"head_sha": "[a-f0-9]*"|"status": "[a-z_]*"|"conclusion": "[a-z]*"'
```

拿到 `<sha7>` 后：

- **有面板**：面板里选该构建，点发布。不用记命令。
- **没面板**：按 `deploy/RELEASE-RUNBOOK.md` 走 8 步（pull → 取 digest → 改 override 两行 → 备份 DB → `compose up -d` → 健康检查 → 提交 digest）。

**镜像固定用 digest，不要用 tag**：`dev` 是浮动 tag，会被下次构建覆盖。

**若改动含数据库迁移**：先在测试栈验证，再切生产（app 启动自动执行迁移，`AUTO_MIGRATE=true`）。回滚镜像不会回滚 schema —— 生产发布前必须 `pg_dump`。

---

## 7. 验证清单

| 检查项 | 命令 | 预期 |
|---|---|---|
| 容器健康 | `docker ps --filter name=WeKnora` | app `(healthy)` |
| 当前版本 | `docker exec WeKnora-app grep -ac "$(git rev-parse HEAD)" /app/WeKnora` | `>0`（ldflags 已注入） |
| 二开代码在镜像内 | `docker exec WeKnora-app grep -c "system/admin/runtime/tasks" /app/WeKnora` | `6` |
| schema 版本 | `docker exec WeKnora-postgres psql -U postgres -d WeKnora -tAc "select version,dirty from schema_migrations"` | 与迁移文件数一致，`dirty=f` |
| 前端 | `curl -o /dev/null -w "%{http_code}" http://127.0.0.1/` | `200` |
| 任务台路由 | `curl -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/api/v1/system/admin/runtime/tasks` | `401`（未认证=路由存在） |
| 发布面板路由 | `curl -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/api/v1/system/admin/release/status` | `401`（同上） |
| 局域网 / Mac 侧只读探测 | `curl --noproxy '*' -s -o /dev/null -w "%{http_code}" http://$HOST/api/v1/system/admin/release/status` | `401`（经前端 `:80` 转发；**`:8080` 从局域网不可达**，别用它判断） |
| 局域网 / Mac 侧前端存活 | `curl --noproxy '*' -s -o /dev/null -w "%{http_code}" http://$HOST/` | `200` |

> **别用 `http://$HOST/health` 判断后端存活。** 前端 nginx 的 `location /` 是 `try_files $uri $uri/ /index.html`（`frontend/nginx.conf:88-91`），任何不存在的路径都回 `200` + HTML，`/health` 也不例外（2026-11-27 实测）。后端是否在，看 `/api/...` 路由返回的 `401`；后端**版本**是否换对，看 `docker exec WeKnora-app printenv COMMIT_ID`。
| KG 默认开关 | `docker exec WeKnora-app env \| grep DEFAULT_GRAPH` | `=true` |

---

## 8. 回滚

**首选**：面板 →「回滚上一版」，自动带出上一个成功发布的 digest。

手工：

```bash
cd "$REPO"
# 把 docker-compose.override.yml 里 app / frontend 两行 digest 改回旧值
docker compose up -d app frontend
```

旧 digest 记录在 `docker-compose.override.yml` 的行尾注释（`# <sha7>`）和 release-agent 的 `state.json` 的 `previous` 字段里。

**若已发生迁移**，旧镜像只认旧 schema，必须连库一起回：

```bash
docker compose stop app
docker exec -i WeKnora-postgres pg_restore -U postgres -d WeKnora --clean --if-exists \
  < "$REPO/../pg-dumps/weknora-pre-<sha7>.dump"
docker compose up -d app
```

---

## 9. 本地定制文件索引（合并上游时重点检查）

| 文件 | 改动性质 |
|---|---|
| `internal/types/indexing_strategy.go` | 新增 `atomic.Bool` + `SetDefaultGraphEnabled()`，默认值仍为 false |
| `cmd/server/main.go` | 启动时读 `DEFAULT_GRAPH_ENABLED` 注入 |
| `frontend/src/views/knowledge/KnowledgeBaseEditorModal.vue` | 默认值改为读 `VITE_DEFAULT_GRAPH_ENABLED` |
| `frontend/Dockerfile` | 暴露 `ARG VITE_DEFAULT_GRAPH_ENABLED` |
| `frontend/src/views/system/RuntimeQueues.vue` | 新增任务台 tab |
| `frontend/src/views/system/RuntimeTaskConsole.vue` | 新增文件（任务台组件） |
| `internal/handler/release.go` | 发布面板后端（4 个接口，转发 release-agent） |
| `frontend/src/components/settings/ReleasePanel.vue` | 发布面板 UI |
| `frontend/src/components/UserMenu.vue` | GitHub 项 → 环境感知的发布入口 |
| `docker-compose.override.yml` | docker.sock、builtin_models 挂载、GOPROXY_ARG、镜像 digest 钉死 |
| `deploy/devstack/**` | 独立测试栈（上游无此目录） |
| `config/builtin_models.yaml` | 声明 Embedding/Rerank，不含 gpt-4o-mini |
| `.github/workflows/build-fork-image.yml` | fork 专用 CI（ghcr + amd64） |

原则：**不修改上游函数的默认语义，只加运行时开关**，把冲突面从"函数体"降到"一行调用"。

---

## 10. 相关脚本与文档

| 路径 | 用途 |
|---|---|
| `deploy/RELEASE-RUNBOOK.md` | 发布 / 回滚的权威手工流程（含故障速查） |
| `docker-compose.dev.yml` + `scripts/dev.sh`（`make dev-start/dev-app/dev-frontend`） | 开发机本地环境（流程 B），容器后缀 `-dev`，app/前端跑宿主机 |
| `deploy/devstack/build-local.sh` | 本地构建（`app` / `frontend` / `all`，`SLIM=1` 精简 runtime 依赖） |
| `deploy/devstack/devstack-watch.ps1` | 宿主机文件监听（Windows） |
| `deploy/devstack/docker-compose.devstack.yml` | 测试栈编排 |
| `../weknora-sync-upstream.sh` | 拉官方更新到 `main` |
| `../weknora-release-agent/` | 发布执行器（宿主进程，非仓库内） |
| `../tools/asynq-seed/` | 造任务台测试数据 |

---

## 11. 数据库连接（生产 / 测试）

两套库**完全隔离**：各自独立数据卷，互不可见，共用同一个镜像 `paradedb/paradedb:v0.25.11-pg17`（PG 17 + pgvector + pg_search）。

> 开发机自己的第三套库（`WeKnora-postgres-dev`，`docker-compose.dev.yml`）不在本节对照内：它只发布在本机，参数随 `.env`，见流程 B。

### 连接参数对照

| | 生产 | 测试（devstack） |
|---|---|---|
| 容器名 | `WeKnora-postgres` | `wktc-postgres` |
| **宿主端口（局域网用这个）** | **5432** | **5433** |
| 库名 | `WeKnora` | `WeKnora` |
| 用户 | `postgres` | `postgres` |
| 密码 | `.env` 的 `DB_PASSWORD`（不落文档） | `devpass` |
| 数据卷 | `postgres-data` | `tc-pgdata` |
| 容器内地址（app 用） | `postgres:5432` | `tc-postgres:5432` |
| 端口声明位置 | `docker-compose.override.yml` → `postgres.ports` | `deploy/devstack/docker-compose.devstack.yml` → `tc-postgres.ports` |

### 局域网连接

两个端口都已发布到 `0.0.0.0`，同网段任意机器可直连。

```bash
# 生产
psql "host=$HOST port=5432 dbname=WeKnora user=postgres password=<DB_PASSWORD> sslmode=disable"

# 测试
psql "host=$HOST port=5433 dbname=WeKnora user=postgres password=devpass sslmode=disable"
```

DBeaver / JDBC：

| 项 | 生产 | 测试 |
|---|---|---|
| Host | `$HOST` | `$HOST` |
| Port | `5432` | `5433` |
| Database | `WeKnora` | `WeKnora` |
| Username | `postgres` | `postgres` |
| JDBC URL | `jdbc:postgresql://$HOST:5432/WeKnora` | `jdbc:postgresql://$HOST:5433/WeKnora` |

防火墙：`dev-5432` / `dev-5433`（Inbound Allow，Profile=Domain,Private）已放行。**换机器或重装系统后必须重建这两条规则**，否则端口通、连接被拒。

### 三条必读

1. **地址一律用 LAN IP，禁止写 `localhost` / `127.0.0.1`** —— 容器视角的 127.0.0.1 指向容器自身，且 WeKnora 的 SSRF 校验会拒回环地址。
2. **宿主上自测自己的 LAN IP 可能不通**（NAT hairpin）。在部署机本机验证用 `127.0.0.1:5432` / `127.0.0.1:5433`；局域网其他机器才用 `$HOST`。
3. **两库端口不同，别连错**：生产 5432、测试 5433。在对的库上做迁移/清洗才不会污染生产。

### 常用核对

```bash
# 表数量（当前两库均为 90）
docker exec WeKnora-postgres psql -U postgres -d WeKnora -tAc \
  "select count(*) from information_schema.tables where table_schema='public'"

# 迁移版本与 dirty 标记
docker exec WeKnora-postgres psql -U postgres -d WeKnora -tAc \
  "select version, dirty from schema_migrations"

# 谁在连（排查连接泄漏）
docker exec WeKnora-postgres psql -U postgres -d WeKnora -tAc \
  "select usename, client_addr, count(*) from pg_stat_activity group by 1,2 order by 3 desc"
```

**改端口后必须 `docker compose up -d postgres` 重建容器**——`docker restart` 不会重新应用 `ports` 配置。

---

## 12. 其他中间件：开发机能用什么

判断依据：**容器监听 `0.0.0.0` + 防火墙 `dev-<端口>` 放行** = 局域网可达。
部署机自测用 `127.0.0.1`（LAN IP 撞 hairpin 返回 000），其他机器一律用 `$HOST`。

### Neo4j（图数据库）

| | 生产 | 测试（devstack） |
|---|---|---|
| 容器 | `WeKnora-neo4j` | `wktc-neo4j` |
| **宿主端口** | **7474**(HTTP) / **7687**(Bolt) | **7475** / **7688** |
| 用户 | `neo4j` | `neo4j` |
| 密码 | `.env` 的 `NEO4J_PASSWORD` | `devpass123` |
| 端口声明 | `docker-compose.override.yml` → `neo4j.ports`（`!override` 整体替换） | devstack compose → `tc-neo4j.ports` |

```bash
# HTTP 事务接口（不需要装 cypher-shell）
curl -u "neo4j:$NEO4J_PASSWORD" -H "Content-Type: application/json" \
  -X POST http://$HOST:7474/db/neo4j/tx \
  -d '{"statements":[{"statement":"MATCH (n) RETURN count(n)"}]}'

# Bolt（Neo4j Browser / Bloom / 驱动）
bolt://$HOST:7687      # 生产
bolt://$HOST:7688      # 测试
```

当前生产图库 3190 个节点。**生产图库只读**，写入会污染知识图谱实体。

### 开箱即用（无鉴权 / 公开凭据）

| 服务 | 地址 | 能力 | 备注 |
|---|---|---|---|
| **Ollama** | `$HOST:11434` | `qwen3.5:9b`(6.6G)、`tev1:latest`(4.6G) | `GET /api/tags` 列模型 |
| **Qdrant**（test 实例） | `$HOST:6335` REST / `6336` gRPC | 8 个 collection | 含 `sixlines-project-knowledgebase-WeKnora` 等真实数据 |
| **SearXNG** | `$HOST:8888` | 联网搜索 | `?format=json` 已开启，返回 JSON |
| **MinerU** | `$HOST:8000` | 文档解析 | 4.x 只有 Tasks API：`POST /v1/tasks`；`/docs` 有 Swagger |
| **测试前端** | `$HOST:5174` | 改代码即生效 | |
| **测试 API** | `$HOST:8081` | 接口调试 | |
| **测试 PG** | `$HOST:5433` | 自由读写 | 见第 11 节 |

### 需要凭据

| 服务 | 地址 | 凭据 | 说明 |
|---|---|---|---|
| **Infinity RAG** | `$HOST:7997` | `Authorization: Bearer 67b73c603d9b9474b6066a05d9fb4101f27cf92d15886849` | `bge-m3` 1024 维 + `bge-reranker-v2-m3`。key 来自容器启动参数 `--api-key`，换部署时要重新取：`docker inspect infinity-rag --format '{{json .Config.Cmd}}'` |
| **生产 API / 前端** | 前端 `:80`；API 只在容器内 `:8080`，局域网经 `:80` 的 `/api/` 转发 | 用户登录 | 只读体验，不要用它跑写操作 |
| **生产 PG** | `$HOST:5432` | `.env` 的 `DB_PASSWORD` | **只读** |
| **生产 Neo4j** | `$HOST:7474/7687` | `.env` 的 `NEO4J_PASSWORD` | **只读** |
| **config-sync MCP** | `$HOST:8090` / `:8091` | `MCP_AUTH_TOKEN`（`docker exec weknora-config-sync-mcp printenv MCP_AUTH_TOKEN`） | 绑的是 LAN IP，本机用 `127.0.0.1` 连不上属正常 |

### 局域网拿不到

| 服务 | 原因 | 要用的话 |
|---|---|---|
| Redis（生产 / 测试） | 无宿主端口映射 | 不建议开：测试库 `devpass` 裸奔，生产虽有密码但属数据面 |
| docreader | 仅容器网内 `50051` | 通过 app 间接使用 |
| searxng-valkey | 仅容器网内 | 同上 |

### 开发机 `.env.devstack` 必填

```bash
EMBEDDING_BASE_URL=http://$HOST:7997/v1     # Infinity，provider 里还要填 api_key
RERANK_BASE_URL=http://$HOST:7997/v1
OLLAMA_BASE_URL=http://$HOST:11434
WEKNORA_DEPLOY_ENV=devstack
```

### 连通性自检（开发机上跑）

```bash
curl -s http://$HOST:11434/api/tags | head -c 200
curl -s -H "Authorization: Bearer 67b73c603d9b9474b6066a05d9fb4101f27cf92d15886849" \
  -X POST http://$HOST:7997/embeddings -H "Content-Type: application/json" \
  -d '{"model":"BAAI/bge-m3","input":["x"]}' | head -c 120
curl -s "http://$HOST:8888/search?q=test&format=json" | head -c 120
curl -s -o /dev/null -w "%{http_code}\n" http://$HOST:8000/docs
curl -s http://$HOST:6335/collections | head -c 200
curl -s -u "neo4j:$NEO4J_PASSWORD" -o /dev/null -w "%{http_code}\n" http://$HOST:7474/
```

### 端口分配总表（避免混连）

| 服务 | 生产 | 测试 |
|---|---|---|
| 前端 | 80 | 5174 |
| API | 8080 | 8081 |
| PostgreSQL | 5432 | 5433 |
| Neo4j HTTP | 7474 | 7475 |
| Neo4j Bolt | 7687 | 7688 |
