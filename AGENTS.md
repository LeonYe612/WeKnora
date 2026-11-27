# Repository Guidelines

## Project Structure & Module Organization

- `cmd/server/` + `internal/` — Go backend (Gin); handlers, services, repositories, agent tools under `internal/`.
- `frontend/` — Vue 3 + Vite + TDesign SPA.
- `docreader/` — Python gRPC document parser (deps via `uv`).
- `cli/`, `client/`, `mcp-server/` — CLI (separate Go module), Go SDK, MCP server.
- `migrations/`, `config/` — SQL migrations and runtime config.
- `deploy/`, `docker/`, `scripts/` — deployment, Dockerfiles, entrypoints.

Tests are colocated: `*_test.go` beside the code, `frontend/src/**/*.test.ts`, `docreader/tests/test_*.py`.

## Build, Test, and Development Commands

```bash
make deps && make build        # go mod download, then build ./cmd/server
make test                      # entrypoint tests + go test ./...
make fmt && make lint          # gofmt/gofumpt; golangci-lint (lll: 120 cols)
make migrate-up                # apply SQL migrations
make dev-start                 # infra in Docker (needs .env); then make dev-app / dev-frontend
cd frontend && npm run type-check && npm test
uv run --project docreader python -m unittest discover -s docreader/tests -p "test_*.py"
```

`make fmt` rewrites the whole repo — run it only on a clean worktree.

## Coding Style & Naming Conventions

- **Go:** `gofmt`/`gofumpt`, tabs, 120-column limit (Swagger annotations exempt); `revive` is enabled.
- **Vue SFCs:** `<script setup lang="ts">` → `<template>` → `<style scoped>`; props via `defineProps<{...}>()`.
- **Frontend:** TypeScript via `vue-tsc`; tests run with `tsx --test`.
- **Python:** stdlib `unittest`, no pytest config in `pyproject.toml`.
- Handlers are methods on a `*Handler` receiver; routes are grouped in `internal/router/routes_*.go`.

## Testing Guidelines

Go tests combine `testing` with `testify` (`require`/`assert`); name them `TestSubjectBehaviour`. Full suites need live infrastructure — when unavailable, run targeted tests (`go test ./internal/<pkg> -count=1`) and record the exact failing command in the PR.

## Commit & Pull Request Guidelines

Conventional Commits with a scope: `feat(release): ...`, `fix(parser): ...`, `docs(deploy): ...`, `ci:`, `release: ...`. Imperative subject; body uses `-` bullets, splitting backend/frontend/host impact.

Complete `.github/pull_request_template.md`: Type of Change, `Fixes #<issue>`, Testing steps, checklist. `git diff --check origin/main...HEAD` and `golangci-lint run --new-from-rev=origin/main ./...` must pass. Attach screenshots for UI changes.

## Fork Branch Model

Commit only to `dev`; `main` mirrors `Tencent/WeKnora` and accepts fast-forwards only. Never open a PR against upstream. Prefer runtime switches and `docker-compose.override.yml` over editing upstream defaults. `VITE_*` values are build-time — rebuild, not restart.

## Security & Configuration

`.env*` is gitignored by the blanket `.*` rule; keep secrets out of commits and `deploy/devstack/.env.devstack`. Mounting `/var/run/docker.sock` is root-equivalent — private single-node deployments only.

## Agent-Specific Instructions

Read `cli/AGENTS.md` before touching the CLI: it defines the stable JSON/exit-code contract consumed by AI agents. `deploy/DEV-WORKFLOW.md` and `deploy/RELEASE-RUNBOOK.md` are authoritative for environment and release procedure.
