# Stage 0 架构说明

## 1. 目标与交付边界

Stage 0 只交付可构建、可迁移、可观测存活状态的后端基础设施：Go API/Worker/Migration 三个入口、MySQL、可降级 Redis、Nginx 入口和 Docker Compose 编排。

当前已实现的 HTTP 端点只有：

- `GET /health/live`
- `GET /health/ready`

`/api/v1` 只是尚未挂载业务路由的空分组。注册、登录、文章、分类、标签、评论、任务消费、AI Chat、Embedding、RAG 与 Milvus 集成都不属于本阶段。Frontend 不在当前工作区，将在第 2 批交付。

## 2. 依赖与请求图

```text
Host :${PROXY_PORT:-8080}
          |
          v
+-------------------+
| Nginx proxy :8080 |---- GET / ----------------> Stage 0 JSON
+-------------------+
          |
          | /health/*, /api/*
          v
+-------------------+          hard dependency          +----------------+
| Go API :8080      |----------------------------------->| MySQL 8.4     |
| /health/live      |                                    | schema + data  |
| /health/ready     |                                    |                |
+-------------------+                                    +----------------+
          |                                                       ^
          | soft dependency                                      |
          v                                                       | hard dependency
+-------------------+                                    +----------------+
| Redis 7.4         |                                    | migrate (once) |
| cache/rate-ready  |                                    | embedded SQL   |
+-------------------+                                    +----------------+
                                                                  ^
+-------------------+          hard dependency                    |
| Go Worker         |---------------------------------------------+
| Stage 0 placeholder|-----------------------> MySQL / Redis clients
+-------------------+

同一个 blog-backend 镜像：
  command api     -> /app/api
  command worker  -> /app/worker
  command migrate -> /app/migrate
```

Nginx 只代理 `/health/` 与 `/api/`；根路径返回阶段说明，其他路径返回本地 404 JSON。它没有 TLS，也没有静态 Frontend。

## 3. 启动与关闭生命周期

基础 Compose 的启动顺序是：

1. MySQL 和 Redis 启动并执行各自健康检查。
2. 一次性 `migrate` 服务在 MySQL healthy 后执行 `migrate up`。
3. API 与 Worker 等待 migration 成功；MySQL 是硬依赖，Redis 在应用语义上可降级。
4. API 的 `/health/ready` 成功后，Proxy 才启动。
5. `migrate` 正常退出并保持 exited(0)；其余长期服务采用 `unless-stopped`。

API 收到 `SIGINT`/`SIGTERM` 后，在 `HTTP_SHUTDOWN_TIMEOUT`（默认 `15s`）内尝试优雅关闭 HTTP server，再释放 Redis/MySQL 客户端。Worker 在 Stage 0 只等待终止信号，没有领取或执行任务。直接运行 API/Worker 不会自动迁移；自动顺序是 Compose 的编排行为。

## 4. 硬依赖与软依赖

### 硬依赖

- **MySQL**：API/Worker 启动时创建连接并 ping；失败会阻止启动。Readiness 中 MySQL 失败返回 HTTP 503。
- **Migration 完成**：Compose 中 API/Worker 依赖一次性 migration 成功退出。
- **有效基础配置**：完整应用加载要求 `MYSQL_DSN`、至少 32 bytes 的 `JWT_SECRET`，以及合法的 service mode。JWT 虽然尚无业务路由使用，当前校验仍是启动前置条件。

### 软依赖

- **Redis**：连接或 ping 失败时记录 warning，应用继续启动。`/health/ready` 把 Redis 标记为 `degraded`，只要 MySQL 正常仍返回 HTTP 200。
- **AI**：默认关闭。Readiness 只报告配置开关 `enabled/disabled`，不探测供应商或 Milvus；Stage 0 不应开启。

Compose 自带的 Redis 使用 `requirepass`，所以当前部署要求 `REDIS_PASSWORD`；这属于 Compose 服务约束，不代表 Go 配置层不允许无密码 Redis。

## 5. 配置边界

`config.Load` 只读取进程环境变量，不主动加载根目录 `.env`。配置边界如下：

```text
.env / shell / deployment secret
              |
              | Compose interpolation + explicit environment allow-list
              v
      API / Worker process environment
              |
              v
          config.Load
```

- `.env.example` 同时列出 Compose 初始化变量和当前 Go 配置的重要变量，但它只是模板。
- `deploy/compose.yaml` 向容器透传 Stage 0 使用的配置；AI、Embedding 与 Milvus/RAG 变量仍是后续阶段模板。
- `MYSQL_PASSWORD`/`MYSQL_ROOT_PASSWORD` 用于初始化数据库服务；`MYSQL_DSN` 是应用使用的 `go-sql-driver/mysql` DSN，Compose 不会自动拼接，三者必须由部署者保持一致。
- 显式提供但无法解析的整数、布尔、浮点或 duration 会在启动时失败；默认值只用于变量缺失或为空。
- Nginx 固定 `client_max_body_size 2m`；提高应用 `HTTP_MAX_BODY_BYTES` 不会自动提高代理限制。
- `REQUEST_ID_HEADER` 可在应用侧配置，但当前 Nginx 固定使用 `X-Request-ID`。
- `APP_REQUEST_TIMEOUT` 当前只被读取，Stage 0 尚未安装请求总时长中间件，因此不产生运行时限制。
- AI、Embedding、Milvus/RAG 配置是阶段 3/4 预留，不代表运行时能力已经存在。

敏感值应来自本地 `.env` 或部署平台 Secret，不能写入镜像、文档、日志或源码。

## 6. Migration 单一来源

Schema 的唯一真相来源是：

```text
backend/migrations/*.sql
          |
          | //go:embed
          v
backend/migrations/embed.go
          |
          v
backend/internal/platform/migrations
          |
          v
backend/cmd/migrate
```

当前只有版本 `0001`。CLI 只支持：

- `migrate list`：列出二进制内嵌版本，不连接数据库。
- `migrate up`：应用全部待执行版本，可重复调用。
- `migrate version`：显示当前版本和 dirty 状态。
- `migrate down`：仅在 `APP_ENV=dev` 时回滚最新一个版本。

禁止用 GORM `AutoMigrate` 代替 SQL migration。当前只有一个版本，因此一次 `down` 会删除现有的全部 Stage 0 schema；项目也没有 `force` 或自动 dirty 修复命令。

## 7. 容器拓扑与数据

| 服务 | 镜像/构建 | 对外端口 | 持久化 | 说明 |
|---|---|---|---|---|
| `proxy` | Nginx 1.28 | `${PROXY_BIND_ADDRESS}:${PROXY_PORT}`，示例为 `0.0.0.0:8080` | 无 | 唯一基础入口 |
| `api` | `backend/Dockerfile` | 基础配置不发布 | 无 | 容器内 `:8080` |
| `worker` | 与 API 同镜像 | 无 | 无 | Stage 0 占位进程 |
| `migrate` | 与 API 同镜像 | 无 | 无 | 一次性任务 |
| `mysql` | MySQL 8.4 | 基础配置不发布 | `mysql_data` | UTC、utf8mb4 |
| `redis` | Redis 7.4 | 基础配置不发布 | `redis_data` | AOF + requirepass |

所有服务位于 `172.28.0.0/16` bridge 网络。开发覆盖文件只把 API、MySQL、Redis 额外绑定到宿主机 loopback；不挂载源码、不热重载，也不改变应用环境或 migration 行为。

## 8. 阶段边界

- **Stage 0（当前）**：配置、日志、request ID/CORS/请求体限制基础、MySQL/Redis 客户端、SQL migration、健康检查、容器编排。
- **阶段 1（未实现）**：认证与博客领域 API、真正的限流和任务生产/消费。
- **第 2 批（未交付）**：Frontend 源码与页面集成。
- **阶段 3（未实现）**：OpenAI-compatible Embedding、Milvus 与文章索引能力。
- **阶段 4（未实现）**：OpenAI-compatible Chat 与 RAG 检索问答。

数据库表、配置字段或空目录的存在只表示为后续阶段预留，不构成对应产品功能已经可用的承诺。
