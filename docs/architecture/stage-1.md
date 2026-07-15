# Stage 1 架构说明

## 1. 目标与交付边界

Stage 1 在 Stage 0 基础设施上交付认证、博客领域 API、Redis 限流和 MySQL 后台任务闭环。Frontend 仍不在本工作区；AI Chat、Embedding、Milvus 与 RAG 仍属于 Stage 3/4。

本阶段保留 Stage 0 的部署约束：MySQL 是硬依赖，Redis 是可降级软依赖，SQL migration 是 schema 唯一来源，API、Worker 和 migration 使用同一镜像。

## 2. HTTP API

所有业务路由位于 `/api/v1`。

### 认证

| 方法 | 路径 | 权限 | 限流 |
|---|---|---|---|
| `POST` | `/auth/register` | 公开 | IP |
| `POST` | `/auth/login` | 公开 | IP |
| `POST` | `/auth/refresh` | Refresh Cookie | IP |
| `POST` | `/auth/logout` | Refresh Cookie（幂等） | 无 |
| `GET` | `/auth/me` | Access Token | 无 |

密码使用 Argon2id。Access Token 是短期 HS256 JWT；Refresh Token 是随机值，仅以 SHA-256 hash 存入数据库，并通过 HttpOnly、SameSite=Lax Cookie 传输。刷新采用 token family 内原子轮换。

每个受保护请求都会读取当前用户，要求账号 `active` 且 JWT 的 `token_version` 与数据库一致；角色也以数据库当前值为准。因此封禁、撤销和降权不必等待 JWT 过期。

### 文章、分类与标签

| 方法 | 路径 | 权限 |
|---|---|---|
| `GET` | `/posts` | 公开，仅 published/public |
| `GET` | `/posts/:slug` | 公开文章无需登录；非公开内容仅作者/管理员 |
| `POST` | `/posts` | 登录用户 |
| `PUT` / `DELETE` | `/posts/:slug` | 作者或管理员 |
| `GET` | `/categories`, `/tags` | 公开 |
| `POST` / `PUT` / `DELETE` | `/categories...`, `/tags...` | 管理员 |

文章 Markdown 由 Goldmark 渲染，再由 Bluemonday 清洗。`status` 只允许 `draft|published|archived`，`visibility` 只允许 `public|private`。`category_ids` 与 `tag_ids` 使用列表接口返回的 `public_id`，会去重，空值或不存在的 ID 返回 400。文章 slug 最大 200 bytes，并与数据库包含软删除行的唯一约束保持一致。

### 评论

| 方法 | 路径 | 权限 |
|---|---|---|
| `GET` | `/posts/:slug/comments` | 公开，仅 approved |
| `POST` | `/posts/:slug/comments` | 登录用户，按用户限流 |
| `PUT` | `/comments/:id` | 作者或管理员，按用户限流 |
| `DELETE` | `/comments/:id` | 作者或管理员 |

评论仅支持顶层评论和一层回复。创建或编辑后状态为 `pending`，同时在同一 MySQL 事务中写入 `comment_moderation` 任务。Stage 1 Worker 对已经过 Markdown 清洗且非空的评论执行确定性批准，使其变为 `approved`；该机制是任务闭环，不是 AI 或人工内容安全审核。

## 3. 限流与降级

注册、登录、刷新按客户端 IP 限流；评论写入按当前用户限流。Redis Lua 脚本原子执行计数与窗口过期，超过额度返回 HTTP 429。

Redis 延续 Stage 0 的软依赖策略：Redis 不可用时限流 fail-open，并记录降级；MySQL 正常时 readiness 仍返回 200。需要强制 fail-closed 的部署必须在后续阶段单独修改该契约。

## 4. 后台任务语义

`background_jobs` 提供至少一次投递：

1. Producer 可独立入队，也可使用业务 GORM transaction 原子入队。
2. Worker 使用 MySQL 8 `FOR UPDATE SKIP LOCKED` 按优先级、创建时间领取批次。
3. 领取时原子增加 `attempts` 并记录唯一 `locked_by` 与 `locked_at`。
4. 成功任务进入 `completed`；失败任务按尝试次数退避并回到 `pending`，耗尽预算后进入 `dead`。
5. Worker 启动时及运行中周期性回收 stale lock；已耗尽尝试的 stale job 直接进入 `dead`。
6. Handler 必须幂等。评论 moderation 通过状态条件更新实现重复执行安全。

当前只注册 `comment_moderation`。文章向量索引等任务仍属于 Stage 3。

## 5. 运行拓扑

```text
Client -> Nginx -> Go API -> MySQL
                    |          ^
                    v          |
                  Redis     Go Worker
                             claim/handle
```

- API：健康检查、业务路由、限流、任务生产。
- Worker：评论 moderation、重试、死信与 stale-lock 恢复。
- MySQL：领域数据、Refresh Token、后台队列的持久真相。
- Redis：限流与 readiness 降级信号，不承载可靠任务。

## 6. 验收标准

Stage 1 完成要求：

- `make check` 和 `go test -race ./...` 通过，三个命令可构建。
- Compose 配置可静态解析，migration 可重复执行。
- 注册、登录、刷新轮换、注销和 `/me` 可用。
- 撤销、封禁和角色变更立即影响受保护请求。
- 文章、分类、标签、评论的公开与权限路径符合上表。
- Redis 正常时超限返回 429，Redis 故障时按 fail-open 契约降级。
- 评论写入与任务入队原子；Worker 可批准评论，并能处理重试、dead job 和 stale lock。
- Stage 0 的 live/ready、优雅关闭和 Compose 生命周期不回归。

真实数据库并发领取与完整 HTTP 闭环应在 Compose 集成环境中验收；单元测试不能替代 MySQL 8 `SKIP LOCKED` 行为验证。

## 7. 后续阶段

- 第 2 批：Frontend 页面与 API 集成。
- Stage 3：Embedding、Milvus、文章索引任务。
- Stage 4：Chat、检索与 RAG 问答。
