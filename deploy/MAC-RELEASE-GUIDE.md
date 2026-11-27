# Mac 端操作指令：WeKnora 改代码 + 发布到生产

> 适用：在 Mac 上改二开代码并发布到生产 `172.20.19.101`（Windows）。
> 最后核对：2026-11-27（dev `cec9fb198`）。本文每条命令都在 Mac 上实跑过；标「未验证」的除外。
> 分支模型 / 环境拓扑 / 回滚见 `deploy/DEV-WORKFLOW.md`；生产机侧手工流程见 `deploy/RELEASE-RUNBOOK.md`（权威）。

---

## 边界（先读，避免走错路）

- **Mac 不是生产机**。生产 = `172.20.19.101`（Windows），代码目录 `F:\WorkBuddyProjects\docker\WeKnora`，Docker daemon / volume / release-agent 全在那台机器上。
- Mac 上只做：**改代码 → push dev → 浏览器点发布面板**。发布动作由生产机 release-agent 执行。
- 禁止：
  - Mac 上 `docker compose up` 以为能发布（只影响 Mac 本地 daemon）
  - SSH 改生产机运行文件以为实时生效（生产代码在镜像里，改文件无效；`docker/Dockerfile.app` 是 `COPY . .`）
  - 开 Docker TCP API 直连生产 daemon（root 裸奔 + 与面板 `state.json` 冲突）
  - 从 Mac 手工改 `docker-compose.override.yml` 里的 digest（由生产机发布流程 commit，Mac 侧 `git pull` 即可）
- Apple Silicon 不要本地 pull/run 生产镜像：CI 只出 `linux/amd64`（`.github/workflows/build-fork-image.yml` 的 `platforms`）。
- **Mac shell 通常带代理**（Clash：`http_proxy` / `https_proxy` / `all_proxy` = `127.0.0.1:7890`）。所有打 `172.20.19.101` 的 `curl` **一律加 `--noproxy '*'`**，否则拿到的是代理的 502 / 000，不是目标机器的响应。`127.0.0.1` 不受影响。

## 第 0 步：前置验证（任一失败 → 先解决，不要继续）

```bash
git ls-remote git@github.com:LeonYe612/WeKnora.git refs/heads/dev   # 返回 dev 的 SHA = 远端可达
curl --noproxy '*' -s -o /dev/null -m 15 -w "front:%{http_code}\n" http://172.20.19.101/                                 # 200
curl --noproxy '*' -s -o /dev/null -m 15 -w "api:%{http_code}\n"   http://172.20.19.101/api/v1/system/admin/release/status  # 401（未认证 = 路由存在 = 后端在）
uname -m                                                            # arm64
```

- `ls-remote` 是只读探测，证明不了 push 权限；push 权限以第 2 步实际 `git push` 为准。不要用 `https://github.com/... HEAD`——那返回的是默认分支 `main`（官方同步镜像），和你要推的 `dev` 无关。
- `:8080` 从 Mac **不可达**（容器内端口，未发布到宿主），别用它做任何判断。
- 另需确认（Mac 上无法直接验证，只能间接）：你有生产前端 **system admin** 账号；生产机 release-agent 正常运行。间接验证 = 登录后「发布」面板能列出候选构建；列不出就是 agent 侧问题，走「降级」。

## 第 1 步：一次性环境准备

```bash
git clone git@github.com:LeonYe612/WeKnora.git
cd WeKnora && git switch dev
export REPO=$(pwd)

# 装 CI 同款检查钩子（pre-commit 空白/gofmt/lint；pre-push gofmt + vet + test + build）
./scripts/install-git-hooks.sh
git config --get core.hooksPath        # 读回：scripts/git-hooks
```

关于钩子，三件事别弄混：

1. 钩子在 `scripts/git-hooks/`（tracked）。**`deploy/git-hooks/` 不存在**，不要 `cp` 单个文件到 `.git/hooks/`——pre-push 第 9 行 `source "$ROOT/scripts/git-hooks/common.sh"` 靠的是仓库内相对路径，单独拷一份会找不到。
2. **pre-push 不校验远端**（只做 gofmt / vet / test / build）。「推错仓库」没有任何钩子会拦，防线是 `git remote -v` 只有 `origin` = fork。
3. `core.hooksPath` 只能指向一个目录。若本机之前跑过 WFGameAI 的 `doc_dedup_guard.py init`（它会把 hooksPath 指到 `.githooks`），这一步会把它顶掉；想换回去就重跑那条 init。详见 `DEV-WORKFLOW.md` §1「Git hooks」。

脚本若报 `^M`：`sed -i '' 's/\r$//' deploy/devstack/*.sh scripts/git-hooks/*`。
本地开发 / 验证按 `deploy/DEV-WORKFLOW.md` 流程 B（`make dev-start` + `make dev-app` + `make dev-frontend`，或直接用已有的 wktc 测试栈，见流程 A）；**不要用生产库做写操作**。

## 第 2 步：日常改代码 + 发布

```bash
cd "$REPO"
git switch dev
git pull --ff-only origin dev          # Mac 只是副本之一，先同步再改
# 改代码 ...

# 本地先过一遍（可选但推荐）：格式 + 受影响包的测试
gofmt -l ./internal ./cmd
go test ./internal/<你改的包>/ -count=1

git add -A && git commit -m "feat(<scope>): ..."
git remote -v                         # 确认只有 origin = fork
git push origin dev                   # 触发 CI 出镜像，约 10-30 分钟

# 查 CI：head_sha 前 7 位 = 你的 commit，status=completed + conclusion=success
curl -s "https://api.github.com/repos/LeonYe612/WeKnora/actions/runs?per_page=3" \
  | grep -oE '"head_branch": "[^"]*"|"head_sha": "[a-f0-9]*"|"status": "[a-z_]*"|"conclusion": "[a-z]*"'
```

- 仓库是 public，GitHub API 不带 token 可读。
- **纯文档 / 部署提交不会出镜像**：workflow `paths-ignore` 含 `**.md`、`deploy/**`、`docker-compose.override.yml`。推了这类提交，面板里找不到对应构建是正常的。
- pre-push 会跑 `go vet` / `go test` / `go build`，慢是正常的；只跳测试用 `HOOK_SKIP_TEST=1 git push`，整体跳过用 `SKIP_HOOKS=1`（别养成习惯）。

CI 绿后 → 浏览器打开 `http://172.20.19.101/` → 左下角账号菜单 →「发布」（仅 system admin 可见）→ 选对应构建 → 发布。面板自动完成 pull / 备份 / 改 digest / 重建 / 健康检查，失败自动回滚。

## 第 3 步：验证

```bash
# Mac 侧只能做只读探测
curl --noproxy '*' -s -o /dev/null -w "front:%{http_code}\n" http://172.20.19.101/                                        # 200 = 前端在
curl --noproxy '*' -s -o /dev/null -w "route:%{http_code}\n"  http://172.20.19.101/api/v1/system/admin/release/status       # 401 = 后端在
curl --noproxy '*' -s http://172.20.19.101/api/v1/<你这次改动涉及且无需登录的接口>                                       # 200 + 新行为才算生效
```

- **不要用 `http://172.20.19.101/health`**：前端 nginx `location /` 是 `try_files … /index.html`（`frontend/nginx.conf:88-91`），任何不存在的路径都回 `200` + HTML，证明不了后端活着。
- 后端**版本**是否换对，只有生产机能查：`docker exec WeKnora-app printenv COMMIT_ID`（在 101 上执行）。Mac 侧看不到。
- 不要只看前端页面，必须确认 `/api/...` 路由有响应。

## 降级

面板不可用 → **不要从 Mac 手工发布**。让生产机侧按 `deploy/RELEASE-RUNBOOK.md` 8 步执行（pull → 取 digest → 改 override.yml 两行 → pg_dump 备份 → `compose up -d` → 健康检查 → 提交 digest）。原因：compose 文件、volume、数据库、daemon 都在生产机，Mac 远程操作会和面板 `state.json` 打架。

## 同步纪律

Mac 是第三份工作副本（另有 Windows 桌面 + 工作区）。Mac 上 commit/push 后，Windows 侧先 `git pull` 再改，避免 dev 分叉。镜像 digest 由生产机发布流程 commit，Mac 侧 `pull` 即可，**不要在 Mac 上改 digest**。
