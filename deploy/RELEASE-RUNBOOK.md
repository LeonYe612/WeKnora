# WeKnora 发布 Runbook（手工兜底流程）

> 适用：`F:\WorkBuddyProjects\docker\WeKnora` 这套部署。
> 最后更新：2026-11-27（对应 dev `5b43902bd`）
>
> **本文是手工兜底流程**。自动化面板上线后仍保留，用于面板故障时的手工恢复。
> 分支模型见 `deploy/DEV-WORKFLOW.md` 第 0 节。

---

## 0. 前置事实

| 项 | 值 |
|---|---|
| 代码仓库 | `https://github.com/LeonYe612/WeKnora.git`（唯一 remote `origin`） |
| 生产目录 | `F:\WorkBuddyProjects\docker\WeKnora` |
| 生产 compose 项目名 | `weknora`（自动读 `docker-compose.yml` + `docker-compose.override.yml`） |
| 生产容器 | `WeKnora-app`(:8080) / `WeKnora-frontend`(:80) |
| 镜像仓库 | `ghcr.io/leonye612/weknora-app` / `weknora-ui` |
| 镜像构建 | `.github/workflows/build-fork-image.yml`，push `dev` 触发，约 20-30 分钟 |
| 数据库 | postgres，库名 `WeKnora`，用户 `postgres` |
| 备份落点 | `F:\WorkBuddyProjects\docker\pg-dumps\` |

**两个环境不要混**

| | 生产 | 测试栈（本机 wktc） |
|---|---|---|
| compose 项目 | `weknora` | `wktc` |
| 代码生效方式 | **镜像**（构建时 COPY 进去） | **bind mount 实时编译**（改文件即生效） |
| 更新手段 | 换镜像 digest + `up -d` | `docker restart wktc-app` |
| 端口 | 容器内 8080（不发布到宿主）/ 前端 80，`/api/` 经 80 转发 | 8081 / 5174 |
| 是否走 CI | 是 | 否 |

**关键结论**：测试环境**不需要**走本文的镜像流程，改完代码 `docker restart wktc-app` 即可。下面 8 步只针对生产。

---

## 1. 发布 8 步

### ① 提交并推 dev

```bash
cd F:/WorkBuddyProjects/docker/WeKnora
git switch dev
git add -A && git commit -m "feat: ..."
git push origin dev
```

判定：`git log --oneline -1` 的 SHA 记住，后面要用它的前 7 位（下称 `<sha7>`）。

### ② 等 CI 出包

```bash
curl -s "https://api.github.com/repos/LeonYe612/WeKnora/actions/runs?per_page=3" \
  | grep -E '"head_sha"|"status"|"conclusion"'
```

判定：`head_sha` 前 7 位 = `<sha7>`，且 `"status": "completed"` + `"conclusion": "success"`。
失败就去 Actions 页面看日志，**不要**跳过失败的构建。

### ③ 拉镜像并取 digest

```bash
SHA=<sha7>
docker pull ghcr.io/leonye612/weknora-app:$SHA
docker pull ghcr.io/leonye612/weknora-ui:$SHA
docker inspect --format='{{index .RepoDigests 0}}' ghcr.io/leonye612/weknora-app:$SHA
docker inspect --format='{{index .RepoDigests 0}}' ghcr.io/leonye612/weknora-ui:$SHA
```

判定：输出形如 `ghcr.io/leonye612/weknora-app@sha256:xxx`，取 `@` 后面那段。

**不要用 `dev` tag**：它是浮动的，下次构建就被覆盖。必须用 `<sha7>` 定位。

### ④ 改 override 里的 digest

文件 `docker-compose.override.yml`，两行：

- 第 32 行 `app` 的 `image:` → 新 app digest，行尾注释改 `# <sha7>`
- 第 51 行 `frontend` 的 `image:` → 新 ui digest，行尾注释改 `# <sha7>`

```yaml
    image: ghcr.io/leonye612/weknora-app@sha256:<新appdigest> # <sha7>
    image: ghcr.io/leonye612/weknora-ui@sha256:<新uidigest>   # <sha7>
```

判定：`git diff docker-compose.override.yml` 只有这两行变化。

### ⑤ 备份数据库

```bash
docker exec WeKnora-postgres pg_dump -U postgres -d WeKnora -Fc \
  > F:/WorkBuddyProjects/docker/pg-dumps/weknora-pre-$SHA.dump
```

判定：文件大小 > 0 且接近历史 dump 体积。

```bash
ls -lh F:/WorkBuddyProjects/docker/pg-dumps/ | tail -3
```

**为什么必须做**：app 启动时自动跑 golang-migrate，**迁移只前进不后退**。回滚镜像不会回滚 schema，备份是唯一退路。

### ⑥ 切生产

```bash
cd F:/WorkBuddyProjects/docker/WeKnora
docker compose pull app frontend
docker compose up -d app frontend
```

只重建 app + frontend。postgres / redis / neo4j / docreader 不受影响 —— 除非你同时改了它们的配置。

判定：`docker compose ps` 里 app 变 healthy。

### ⑦ 验证

```bash
# 以下在生产机上执行（8080 只在容器网内 / 生产机本地可达，局域网直连 8080 不通）
docker exec WeKnora-app curl -fsS http://127.0.0.1:8080/health   # {"status":"ok"}
docker logs --tail 80 WeKnora-app | grep -iE "migrat|error|panic"
docker exec WeKnora-app printenv COMMIT_ID                       # 应等于 <sha7> 对应的完整 SHA
```

人工过一遍：`http://172.20.19.101/` → 登录 → 知识库列表 → 检索一次 → 上传一个小文档。

从 Mac / 局域网侧只能做只读探测（2026-11-27 实测 `:8080` 不可达）：

```bash
curl --noproxy '*' -s -o /dev/null -w "%{http_code}\n" http://172.20.19.101/                                # 200 = 前端在
curl --noproxy '*' -s -o /dev/null -w "%{http_code}\n" http://172.20.19.101/api/v1/system/admin/release/status  # 401 = 后端在
```

- shell 里有 `http_proxy` / `all_proxy`（Clash 等）时**必须加 `--noproxy '*'`**，否则拿到的是代理的 502 / 000。
- 不要用 `http://172.20.19.101/health`：前端 nginx 对未知路径回 SPA `index.html`，恒为 `200` + HTML，证明不了后端活着。

判定：`COMMIT_ID` 与新镜像一致，日志无 migration 错误，核心链路可用。

### ⑧ 提交 digest 变更

```bash
git add docker-compose.override.yml
git commit -m "release: 生产升级到 $SHA"
git push origin dev
```

`docker-compose.override.yml` 已在 CI 的 `paths-ignore` 里，这次 push **不会**触发镜像重建（`.github/workflows/build-fork-image.yml` 的 `paths-ignore` 还含 `**.md` 与 `deploy/**`：纯文档 / 部署脚本提交不会出镜像，发布面板里找不到对应构建是正常的）。

提交前 `git remote -v` 确认只有 `origin` = fork。仓库自带的 `scripts/git-hooks/pre-push` 只跑 gofmt / vet / test / build，**不校验远端**，推错仓库没有任何钩子会拦。

---

## 2. 回滚

```bash
# 把 override 两行 digest 改回旧值（行尾注释也改回去）
docker compose up -d app frontend
```

已知旧 digest（2e8e91b）：

| 服务 | digest |
|---|---|
| app | `sha256:27e2e74b9527bcd26296507a0d1d1dc8f23483245f321f402780306fca29bc8c` |
| ui | `sha256:0852411b98eca3aabb851d1e85aabe2ffad7f2c8e60993e4004163b6263e95c9` |

本地镜像还在，`up -d` 不会重新 pull。

**schema 已被迁移污染时**：需要 `pg_restore` 回灌 dump：

```bash
docker compose stop app
docker exec -i WeKnora-postgres pg_restore -U postgres -d WeKnora --clean --if-exists \
  < F:/WorkBuddyProjects/docker/pg-dumps/weknora-pre-$SHA.dump
docker compose up -d app
```

---

## 3. 故障速查

| 现象 | 原因 | 处理 |
|---|---|---|
| CI 一直是 `queued` | GitHub Actions 并发限制 | 等前一个跑完，或取消队列里的旧 run |
| `docker pull` denied | ghcr 凭据过期 | Docker Desktop 重新登录 GitHub 账号 |
| app 起不来，日志 `migration failed` | 新版本迁移脚本有问题 | 立即回滚镜像 + `pg_restore` |
| app healthy 但前端白屏 | ui 镜像 digest 没改 / 前端静态资源版本不匹配 | 检查第 51 行 |
| 检索报 404 / `MinerU` 相关 | 镜像退回 v0.9.0（走错 tag） | 确认 digest 是自建 ghcr，不是 `wechatopenai/*:latest` |
| `COMMIT_ID` 与预期不符 | compose 没真正重建 | `docker compose up -d --force-recreate app frontend` |

---

## 4. 状态速查（一眼看当前跑什么）

```bash
cd F:/WorkBuddyProjects/docker/WeKnora
docker compose ps app frontend
docker exec WeKnora-app printenv COMMIT_ID BUILD_TIME VERSION
docker inspect WeKnora-app --format='{{.Config.Image}}'
```

三条输出应对应同一个 `<sha7>`。不一致 = 部署没对齐。
