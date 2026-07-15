# Blog

这是 Blog 项目的 **Stage 0 后端基础设施交付**。当前可以构建并启动 API、Worker 占位进程、SQL migration、MySQL、Redis 和 Nginx，且提供存活/就绪健康检查。

> 当前没有可用的注册、登录、文章、分类、标签、评论、后台任务消费、AI Chat、Embedding、Milvus 或 RAG 业务功能。`/api/v1` 尚未挂载业务路由。Frontend 源码不在本工作区，将在第 2 批交付。

## 当前能力

- Go API：优雅关闭、结构化日志、request ID、CORS 和请求体上限基础。
- 健康检查：`GET /health/live`、`GET /health/ready`。
- MySQL：GORM 连接管理；SQL migration 是 schema 的唯一来源。
- Redis：软依赖，故障时以 degraded 模式继续提供基础 API。
- Worker：与 API 同镜像的独立入口；Stage 0 只维持生命周期，尚不消费任务。
- Compose：Nginx、API、Worker、一次性 migration、MySQL、Redis。

## 技术栈

- Go 1.25，Gin，GORM
- `golang-migrate` 与内嵌 SQL migrations
- MySQL 8.4
- Redis 7.4（AOF）
- Nginx 1.28
- Docker Compose v2

## 目录

```text
Blog/
├── backend/
│   ├── cmd/api/             # API 入口
│   ├── cmd/worker/          # Worker 入口（Stage 0 占位）
│   ├── cmd/migrate/         # migration CLI
│   ├── internal/            # bootstrap、配置、模块与平台适配
│   ├── migrations/          # 内嵌 SQL；schema 唯一来源
│   └── Dockerfile
├── deploy/
│   ├── compose.yaml         # 基础容器拓扑
│   ├── compose.dev.yaml     # 仅增加 loopback 调试端口
│   └── proxy/nginx.conf
├── docs/
│   ├── architecture/stage-0.md
│   └── adr/0001-modular-monolith.md
├── .env.example
└── Makefile
```

## 先决条件

### 使用 Compose（推荐）

- Docker Engine
- Docker Compose v2（`docker compose`）
- GNU Make

### 本地执行 Go 检查或二进制

- Go 1.25.x
- GNU Make

所有命令都从项目根目录 `/home/lsy/桌面/Blog` 执行。路径中包含中文不影响 Makefile；配方已使用带引号的绝对路径。

## 一键启动

1. 创建本地环境文件：

   ```bash
   cp .env.example .env
   ```

2. 编辑 `.env`，至少替换以下仅开发占位值：

   - `MYSQL_PASSWORD`
   - `MYSQL_ROOT_PASSWORD`
   - `REDIS_PASSWORD`
   - `JWT_SECRET`（随机值，至少 32 bytes）
   - `MYSQL_DSN` 中的用户名、密码和数据库名，使其与 MySQL 初始化值一致

   `MYSQL_PASSWORD` 是数据库服务初始化密码，`MYSQL_DSN` 是应用使用的 **go-sql-driver/mysql DSN**，不是 `mysql://` URL，Compose 不会自动拼接两者。密码或查询参数包含 `$`、`#`、空格及 DSN/查询串保留字符时，必须同时遵守 `.env`/Compose 插值规则和 Go MySQL DSN 的引用或转义规则。不要在终端、工单或日志中输出真实 DSN。

3. 校验配置并启动：

   ```bash
   make compose-config
   make up
   make ps
   ```

   `make up` 会构建后端镜像并后台启动基础栈。MySQL healthy 后，一次性 `migrate` 服务先执行 `migrate up`，成功后 API 与 Worker 才启动；API healthy 后 Proxy 才启动。

4. 检查入口和健康状态（示例默认 `PROXY_PORT=8080`）：

   ```bash
   curl -i http://127.0.0.1:8080/
   curl -i http://127.0.0.1:8080/health/live
   curl -i http://127.0.0.1:8080/health/ready
   ```

根路径只返回 Stage 0 说明。当前不存在可调用的博客业务 API。

## 健康检查语义

### `GET /health/live`

只说明 API 进程能够响应，不检查数据库、Redis、migration、Worker 或 AI。正常返回 HTTP 200。

### `GET /health/ready`

- MySQL 是硬依赖：ping 失败时返回 HTTP 503。
- Redis 是软依赖：失败时 `checks.redis` 为 `degraded`，只要 MySQL 正常仍返回 HTTP 200。
- AI 只显示 `AI_ENABLED` 对应的 `enabled`/`disabled`，不会探测模型服务或 Milvus。

因此，HTTP 200 且 Redis degraded 表示基础 API 可用但缓存/未来限流能力降级，并不表示所有后续业务能力可用。

## Migration

项目 CLI 只支持 `list`、`up`、`version` 和 `down`。migration SQL 经 `go:embed` 编入二进制，不使用 GORM `AutoMigrate`。

列出内嵌版本（不连接数据库）：

```bash
make migrate-list
```

通过 Compose 查询数据库版本或重跑 up：

```bash
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate version
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate up
```

仅开发环境允许回滚最新一个版本：

```bash
docker compose --env-file .env -f deploy/compose.yaml run --rm -e APP_ENV=dev migrate migrate down
```

当前只有 `0001`，所以执行一次 `down` 会删除当前全部 Stage 0 schema。生产、staging 或未设置 `APP_ENV=dev` 时 CLI 会拒绝 down。项目没有 `force`/`goto`/`drop` 命令；dirty 状态需要先调查失败 SQL 与数据库状态，不要盲目手工修改版本表。

## 开发覆盖

```bash
make dev-up
```

该命令叠加 `deploy/compose.dev.yaml`，额外发布：

- API：`127.0.0.1:8081`（默认，直连 API）
- MySQL：`127.0.0.1:3306`
- Redis：`127.0.0.1:6379`

它只增加 loopback 端口，不挂载源码、不热重载、不自动设置 `APP_ENV=dev`，也不改变 Secret 或 migration 规则。直连 `:8081` 会绕过 Nginx；`/health` 到 `/health/` 的重定向只存在于 Nginx，API 实际路由是 `/health/live` 和 `/health/ready`。

## 常用 Make 命令

```bash
make help             # 查看全部目标和变量
make fmt              # gofmt -w
make test             # go test ./...
make vet              # go vet ./...
make build            # 构建到 ./bin/{api,worker,migrate}
make check            # 格式检查、vet、test、build
make migrate-list     # 列出内嵌 migration
make compose-config   # 静默校验 Compose，不打印展开后的 secrets
make compose-build    # 构建应用镜像
make up               # 基础栈后台启动
make dev-up           # 加载开发端口覆盖
make ps               # 查看容器状态
make logs             # 跟随全部日志
make down             # 停机，保留数据卷
```

默认读取根目录 `.env`。如需使用其他环境文件：

```bash
make ENV_FILE=/absolute/path/to/env compose-config
```

## 停机与数据卷

正常停机保留 MySQL/Redis 数据：

```bash
make down
```

查看命名卷：

```bash
docker volume ls --filter name=blog
```

彻底删除容器和 `mysql_data`、`redis_data`（不可恢复，务必先备份）：

```bash
docker compose --env-file .env -f deploy/compose.yaml down --volumes
```

## 故障排查

### Compose 提示缺少变量

确认已复制 `.env.example`，且 `MYSQL_DSN`、`MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`REDIS_PASSWORD`、`JWT_SECRET` 非空：

```bash
make compose-config
```

该目标使用 `docker compose config --quiet`，不会把展开后的 Secret 打到终端。不要改用会打印完整配置的命令粘贴到公开日志。

### migration 失败或 API 未启动

```bash
docker compose --env-file .env -f deploy/compose.yaml ps -a
docker compose --env-file .env -f deploy/compose.yaml logs migrate mysql api
```

检查 DSN 是否使用容器内地址 `mysql:3306`，并与 `MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE` 一致。API/Worker 自身不会自动迁移；基础 Compose 依赖一次性 migrate 服务。

### Readiness 显示 Redis degraded

```bash
docker compose --env-file .env -f deploy/compose.yaml logs redis api
docker compose --env-file .env -f deploy/compose.yaml exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping'
```

Redis 故障不应使 readiness 变成 503；MySQL 故障才会。Redis 恢复后重新请求 readiness 确认状态。

### 端口被占用

修改 `.env` 中的 `PROXY_PORT`；开发覆盖的冲突则修改 `API_DEBUG_PORT`、`MYSQL_DEBUG_PORT` 或 `REDIS_DEBUG_PORT`。基础 Compose 只发布 Proxy，数据库和直连 API 端口仅由开发覆盖发布。

### 根路径正常但 API 不健康

Proxy 自身健康检查只访问根路径，不能替代 API readiness。应分别请求 `/health/ready` 并查看 `api`、`migrate`、`mysql` 日志。

## 安全注意

- `.env` 已被 `.gitignore` 忽略；`.env.example` 只能保存无害占位值。
- 部署前生成独立强随机 MySQL、Redis、JWT Secret；不要复用，JWT Secret 至少 32 bytes。
- `MYSQL_ROOT_PASSWORD`、普通用户 `MYSQL_PASSWORD`、应用 `MYSQL_DSN` 是不同配置职责；保持凭据一致不等于把它们混成一个变量。
- 当前 Nginx 没有 TLS。`.env.example` 默认绑定 `0.0.0.0`；对外暴露前应在可信入口终止 TLS、限制网络访问并正确设置 trusted proxies。
- Production 必须使用 secure cookie；开启 credentials 时不能将 CORS origin 设为 `*`。
- Nginx 固定请求体上限为 2 MiB；应用配置与代理限制应一起评估。
- `AI_ENABLED=false` 是 Stage 0 安全默认。阶段 3/4 的 API key 与 Milvus 配置只是模板，当前 Compose 也未完整透传这些变量。

## 后续路线

1. **阶段 1**：认证与博客领域 API、实际限流、后台任务生产/消费。
2. **第 2 批**：Frontend 源码、页面与 API 集成。
3. **阶段 3**：OpenAI-compatible Embedding、Milvus 与文章索引能力。
4. **阶段 4**：OpenAI-compatible Chat、检索与 RAG 问答。

路线只表示计划，不代表对应功能已经实现。详细 Stage 0 边界见 [`docs/architecture/stage-0.md`](docs/architecture/stage-0.md)，关键架构决策见 [`docs/adr/0001-modular-monolith.md`](docs/adr/0001-modular-monolith.md)。
