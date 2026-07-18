# Stage 5 生产运维手册

本文是 Blog Stage 5 的最小生产操作手册。所有生产操作都必须记录操作者、时间、目标环境、变更前后版本和验证结果。

## 1. 发布前检查

1. 确认 PR 的 `Quality` 和 `MySQL integration` 均为绿色。
2. 在 staging 使用与生产相同的镜像 tag、migration 和 secret 注入方式。
3. 执行：

   ```bash
   make verify
   make verify-integration
   docker compose --env-file .env -f deploy/compose.yaml config --quiet
   ```

4. 确认数据库有可用备份，最近一次恢复演练未超过约定周期。
5. 确认 `AUTH_COOKIE_SECURE=true`、CORS 白名单、trusted proxies 和所有生产密钥已从模板值替换。
6. 发布前记录当前镜像 tag、migration version、队列 `pending/running/dead` 和最老任务年龄。

## 2. 正常发布

1. 构建并标记不可变镜像 tag，不要复用 `latest`：

   ```bash
   export BLOG_BACKEND_IMAGE_TAG=2026-07-18-${GIT_SHA}
   export BLOG_FRONTEND_IMAGE_TAG=2026-07-18-${GIT_SHA}
   docker compose --env-file .env -f deploy/compose.yaml build
   ```

2. 先执行只向前的 migration：

   ```bash
   docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate up
   docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate version
   ```

3. 启动或滚动更新 API、Worker、Frontend 和 Proxy：

   ```bash
   docker compose --env-file .env -f deploy/compose.yaml up -d api worker frontend proxy
   ```

4. 验证：

   ```bash
   curl -fsS http://127.0.0.1:${PROXY_PORT:-8080}/health/live
   curl -fsS http://127.0.0.1:${PROXY_PORT:-8080}/health/ready
   docker compose --env-file .env -f deploy/compose.yaml ps
   ```

5. 观察至少一个完整 worker poll/reap 周期，确认 heartbeat 未过期、队列没有异常增长、dead 数没有突增。

生产禁止自动执行 `migrate down`。破坏性 schema 变更必须拆成兼容的 expand → deploy → contract 多次发布。

## 3. 回滚

### 应用镜像回滚

如果新版本 API/Worker 错误率升高但 migration 向后兼容：

```bash
export BLOG_BACKEND_IMAGE_TAG=<previous-known-good-tag>
export BLOG_FRONTEND_IMAGE_TAG=<previous-known-good-tag>
docker compose --env-file .env -f deploy/compose.yaml up -d api worker frontend proxy
```

检查旧版本的配置是否兼容当前 migration；不能仅凭镜像回滚命令解决 schema 不兼容。

### Migration 回滚

默认不执行生产 `down`。如果必须恢复：

1. 停止写流量并保留数据库快照；
2. 由负责人确认 migration 的 down SQL 不会丢失不可恢复数据；
3. 在 staging 使用同一备份和版本完整演练；
4. 维护窗口内人工执行，并记录 migration version/dirty 状态；
5. 恢复服务后验证 health、认证和队列。

## 4. MySQL 备份与恢复

### 逻辑备份

使用与生产兼容的 MySQL client，并把备份写入受控、加密的备份存储：

```bash
mysqldump --single-transaction --routines --triggers --events \
  --hex-blob --set-gtid-purged=OFF \
  -h "$MYSQL_HOST" -u "$MYSQL_USER" -p "$MYSQL_DATABASE" \
  | gzip > "blog-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
```

不要把密码写入命令行历史；使用受控的 client option file、Secret Manager 或交互式输入。

### 恢复演练

1. 启动隔离 MySQL 实例；
2. 导入备份：

   ```bash
   gunzip -c blog-<timestamp>.sql.gz | mysql -h <restore-host> -u <user> -p <database>
   ```

3. 执行 `migrate version` 和核心只读查询；
4. 验证用户、文章、评论、`background_jobs`、`ai_documents` 数量；
5. 运行 `make verify-integration` 或等价 smoke；
6. 记录恢复耗时（RTO）和可接受的数据时间点（RPO）。

恢复演练不能直接覆盖生产数据。

## 5. Redis 故障

- `/health/ready` 在 MySQL 正常时可以继续返回 200，但 Redis 应显示 degraded。
- 认证和评论限流 fail-open 时必须确保入口 WAF/API gateway 仍有基础保护，并立即告警。
- AI 限流 fail-closed，Redis 不可用时 AI 请求应返回 503，避免成本失控。
- Redis 恢复后验证 `PING`、key prefix、限流计数和 AOF/持久化状态；不要把 Redis 当作可靠任务队列。

## 6. Worker/队列故障

重点观察 Worker 日志中的：

- `queue stats`：`pending`、`running`、`dead`、`completed`；
- `oldest_pending_age_seconds`；
- `stale job recovery failed`；
- `job failed` 和 `complete job failed`；
- heartbeat 文件健康检查。

处理步骤：

1. 如果 heartbeat 过期，先检查数据库连通性、Worker 日志和容器资源；
2. 不要直接删除 `background_jobs`；任务是 MySQL 持久状态；
3. Worker 恢复后确认 stale lock 被重新放回 pending 或进入 dead；
4. 对 dead job 先分析 `last_error`，修复根因后再设计受控 replay，不要盲目批量重置；
5. 检查最老 pending 年龄是否恢复下降。

## 7. 密钥轮换

1. 在 Secret Manager 创建新 JWT、MySQL、Redis 或 AI provider 密钥；
2. 对 JWT secret，预期轮换会使现有 Access Token 失效，提前安排重新登录窗口；
3. 对 MySQL/Redis 密码，先配置服务端兼容凭据，再滚动更新应用，最后删除旧凭据；
4. AI provider key 轮换后确认 API 不记录 key；
5. 在日志、Compose inspect、CI 输出和备份中搜索是否出现旧密钥；
6. 记录轮换时间和验证结果。

## 8. 事件升级阈值

立即升级：

- MySQL readiness 连续失败；
- Worker heartbeat 连续过期；
- dead job 数持续增长；
- 最老 pending 年龄超过业务 SLO；
- Refresh replay、认证失败或限流 fail-open 告警异常增长；
- 备份失败或恢复演练超过 RTO。

Stage 5 的目标不是隐藏故障，而是让故障能被及时发现、隔离、恢复并留下可审计记录。
