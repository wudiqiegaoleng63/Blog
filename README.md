# Blog

这是 Blog 项目的 **Stage 2 全栈交付**：React Frontend、认证、文章/分类/标签/评论 API、Redis 限流、MySQL 后台任务，以及健康检查、migration 和 Compose 基础设施。

> Frontend 已由 Compose 同源交付。AI Chat、Embedding、Milvus 与 RAG 尚未实现，分别属于 Stage 3/4。

## 当前能力

- Frontend：React 19 SPA，公开阅读、认证恢复、写作/评论与管理员 taxonomy 页面。
- 认证：注册、登录、Refresh Token 轮换、注销、当前用户；Argon2id + 短期 JWT + HttpOnly Refresh Cookie。
- 博客领域：文章、分类、标签和一层回复评论；Markdown 渲染后进行 HTML 清洗。
- 授权：所有受保护路由实时校验账号状态、`token_version` 和数据库当前角色。
- 限流：注册、登录、刷新按 IP，评论写入按用户；Redis 故障时按 Stage 0 软依赖契约 fail-open。
- Worker：评论写入与 moderation 任务原子提交；领取、重试、dead job、stale-lock 恢复。
- 运维：`GET /health/live`、`GET /health/ready`，SQL migration、Nginx 和 Docker Compose。

Frontend 契约见 [`docs/architecture/stage-2.md`](docs/architecture/stage-2.md)，后端契约见 [`docs/architecture/stage-1.md`](docs/architecture/stage-1.md)。Stage 0 基线见 [`docs/architecture/stage-0.md`](docs/architecture/stage-0.md)，架构决策见 [`docs/adr/0001-modular-monolith.md`](docs/adr/0001-modular-monolith.md)。

## 技术栈

- React 19、TypeScript、Vite 8、React Router 7
- Go 1.25、Gin、GORM
- Argon2id、JWT、Goldmark、Bluemonday
- `golang-migrate` 与内嵌 SQL migrations
- MySQL 8.4、Redis 7.4（AOF）、Nginx 1.28
- Docker Compose v2

## 目录

```text
Blog/
├── frontend/             # React SPA、测试与 production Dockerfile
├── backend/
│   ├── cmd/{api,worker,migrate}/
│   ├── internal/modules/{auth,posts,comments,operations}/
│   ├── internal/platform/{jobs,ratelimit,markdown,...}/
│   ├── migrations/
│   └── Dockerfile
├── deploy/
│   ├── compose.yaml
│   ├── compose.dev.yaml
│   └── proxy/nginx.conf
├── docs/architecture/{stage-0,stage-1}.md
├── .env.example
└── Makefile
```

## 启动

需要 Docker Engine、Docker Compose v2 和 GNU Make。所有命令从项目根目录执行。

1. 创建配置：

   ```bash
   cp .env.example .env
   ```

2. 至少替换 `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`REDIS_PASSWORD`、`JWT_SECRET`，并使 `MYSQL_DSN` 与 MySQL 初始化值一致。`MYSQL_DSN` 是 `go-sql-driver/mysql` DSN，不是 `mysql://` URL；不要在日志或工单中输出真实值。

3. 验证并启动：

   ```bash
   make compose-config
   make up
   make ps
   ```

4. 验证入口：

   ```bash
   curl -i http://127.0.0.1:8080/
   curl -i http://127.0.0.1:8080/health/live
   curl -i http://127.0.0.1:8080/health/ready
   ```

`make up` 会先等待 MySQL/Redis 健康，再执行一次性 migration，随后启动 API 和 Worker；API ready 后才启动 Proxy。

## API 概览

基础路径：`/api/v1`。

```text
POST   /auth/register
POST   /auth/login
POST   /auth/refresh
POST   /auth/logout
GET    /auth/me

GET    /posts
GET    /posts/:slug
POST   /posts
PUT    /posts/:slug
DELETE /posts/:slug

GET    /categories
POST   /categories
PUT    /categories/:slug
DELETE /categories/:slug

GET    /tags
POST   /tags
PUT    /tags/:slug
DELETE /tags/:slug

GET    /posts/:slug/comments
POST   /posts/:slug/comments
PUT    /comments/:id
DELETE /comments/:id
```

写文章和评论需要 `Authorization: Bearer <access_token>`。分类与标签写入需要管理员。Refresh Token 只通过配置路径下的 HttpOnly Cookie 发送。

新评论先返回 `pending`；同一事务会写入 `comment_moderation` 任务，Worker 完成后评论变为 `approved` 并出现在公开列表中。Stage 1 的自动批准只验证经过清洗后的内容非空，不等同于内容安全审核。

## 健康与降级

- `/health/live`：仅表示 API 进程可响应。
- `/health/ready`：MySQL 失败返回 503；Redis 失败标记 `degraded`，MySQL 正常时仍返回 200。
- Redis 不可用时 Stage 1 限流 fail-open。该行为保持基础 API 可用，但不适合要求严格限流的威胁模型。
- Worker 不监听 HTTP；Compose 通过进程存活检查其生命周期。

## Migration

SQL migration 是 schema 唯一来源，不使用 GORM `AutoMigrate`。

```bash
make migrate-list
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate version
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate up
```

仅 `APP_ENV=dev` 允许回滚最新版本：

```bash
docker compose --env-file .env -f deploy/compose.yaml run --rm -e APP_ENV=dev migrate migrate down
```

当前 `0001` 包含整个核心 schema，一次 `down` 会删除这些表。生产和 staging 禁止 down；dirty 状态必须先调查失败 SQL，不能盲目修改版本表。

## 开发与质量检查

```bash
make help
make fmt
make test
make vet
make build
make check
go -C backend test -race ./...
```

开发端口覆盖：

```bash
make dev-up
```

默认额外发布 `127.0.0.1:8081`（API）、`:3306`（MySQL）、`:6379`（Redis）。它不会挂载源码、启用热重载或自动改变 Secret。

## 停机

```bash
make down
```

该命令保留 MySQL/Redis 数据卷。以下命令会不可恢复地删除数据，执行前必须备份：

```bash
docker compose --env-file .env -f deploy/compose.yaml down --volumes
```

## 安全与部署注意

- `.env` 已忽略；`.env.example` 只能保存无害占位值。
- Production 要求 `AUTH_COOKIE_SECURE=true`，并应在可信入口终止 TLS。
- 启用 credentials 时 CORS origin 不能是 `*`。
- 正确设置 `HTTP_TRUSTED_PROXIES`，否则按 IP 限流可能使用错误地址。
- Nginx 与应用请求体上限应一起调整；当前代理上限为 2 MiB。
- MySQL 是可靠任务和业务状态的共同真相；Redis 不承载任务。
- `AI_ENABLED=false` 是 Stage 1 默认且必需的能力边界。

## 后续路线

1. 第 2 批：Stage 2：Frontend 页面与 API 集成（已完成）。
2. Stage 3：OpenAI-compatible Embedding、Milvus 与文章索引。
3. Stage 4：OpenAI-compatible Chat、检索与 RAG 问答。
