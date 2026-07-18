# Stage 5 架构说明：生产化验收与运营

## 1. 判断

Stage 0–4 的主要产品能力已经存在：后端 API、React SPA、Embedding/Milvus 索引和 RAG 问答均已实现，且本地静态质量检查通过。但项目当前还不能仅凭 `make check` 宣称可稳定生产运行：缺少真实 MySQL/Redis/Milvus 的 Compose 集成验收、CI 门禁、备份恢复演练、运行时指标和安全运营闭环。

因此下一阶段不再盲目增加业务功能，而是新增 **Stage 5：Production Hardening & Operations**，目标是把“功能完成”提升为“可验证、可观测、可恢复、可安全发布”。

## 2. 目标与边界

Stage 5 交付：

- 可重复的 CI 质量门禁和 Compose smoke/integration tests；
- 认证、限流、请求体和外部 AI 调用的安全边界；
- Worker 心跳、队列积压、失败/死信和 AI 索引状态指标；
- MySQL/Redis/Milvus 备份、恢复和升级操作手册（见 `docs/operations-runbook.md`）；
- 生产 secrets 不通过普通 Compose environment 长期暴露的部署方案；
- 发布、回滚和故障处置 runbook；
- 明确的 SLO、告警阈值和容量基线。

Stage 5 不包含：图片上传、私信、复杂 CMS、多人协作、会话历史或微服务拆分。只有在真实流量和运营数据证明需要时再单独规划这些能力。

## 3. 验收矩阵

### 3.1 CI 与集成

- `make check`、`make test-race` 和 `make verify` 在干净环境通过；
- `make verify-integration` 启动临时 MySQL 8.4 和 Redis 7.4，重复执行 migration 并验证数据库保持 latest/clean；
- 两个独立 Consumer 对同一批任务并发领取，真实验证 MySQL `FOR UPDATE SKIP LOCKED` 不重复执行，CI 自动运行；
- HTTP 闭环覆盖注册、Refresh rotation/replay、token family 撤销、封禁、角色即时生效、文章写入、评论入队与 Worker moderation；
- Redis 闭环覆盖真实 429、认证/评论限流 fail-open 和 AI 严格限流 fail-closed；
- 后续集成环境补充 Milvus 和完整 API/Worker 进程级闭环；
- Stage 3 覆盖发布/更新/删除文章到向量任务的事务原子性、旧版本 no-op、维度不匹配和重试；
- Stage 4 覆盖公开文章检索、过期版本/私有文章过滤、AI 限流 fail-closed、上游超时/429/5xx 与 prompt injection 样例；
- Playwright smoke 覆盖首页、deep link、登录恢复、写作、评论、管理员 taxonomy、Ask 页面和错误状态。

### 3.2 安全

- 密码输入同时有字节上限和 Argon2 参数上限；
- 认证/评论限流的 Redis fail-open 行为有监控、告警和入口 WAF/网关补偿；AI 限流继续 fail-closed；
- 生产 secrets 使用 Docker secrets、平台 secret store 或等价机制，并完成密钥轮换；
- Worker 和 API 日志不包含密码、Cookie、JWT、API key、完整问题、文章上下文或回答；
- 外部 URL、cover URL、CORS、trusted proxies 和请求体上限有明确白名单/边界；
- 依赖扫描、镜像扫描和最小权限检查进入 CI。

### 3.3 可观测性与恢复

- API：请求量、延迟、状态码、限流拒绝、上游 AI 延迟/错误按类别统计；
- Worker：最后成功轮询时间、处理耗时、队列年龄、running/pending/dead 数量和 stale-lock 回收数；
- AI：索引成功率、重试率、向量维度错误、Milvus/Embedding/Chat 可用性；
- `/health/ready` 仍只把 MySQL 作为核心硬门槛，AI 外部依赖以能力状态和指标表达，不阻断博客核心流量；
- 每月至少演练一次备份恢复，并记录 RPO/RTO；
- 发布失败可回滚应用镜像和 migration，禁止生产自动执行破坏性 down migration。

## 4. 必须先修复的已知问题

1. 文章列表使用 `published_at DESC, id DESC` 的稳定排序，避免分页重复/遗漏。
2. 评论 moderation payload 必须绑定 comment revision，旧任务不能批准新编辑内容。
3. 密码在进入 Argon2 前限制最大字节数，避免超大输入造成资源耗尽。
4. Worker 使用 heartbeat 文件健康检查，避免只检查 PID 导致假健康。
5. slug 分配与唯一约束之间的并发冲突要转换为可重试的业务结果，而不是 500。
6. Compose 生产 secrets 不应依赖可被 `docker inspect` 直接读取的普通 environment；迁移到 secret store 后轮换现有凭据。

## 5. 暂不新增阶段的条件

Stage 5 完成后，项目可以进入小规模生产和真实用户反馈阶段。只有以下信号出现时才规划 Stage 6：

- 需要图片/媒体存储与 CDN；
- 需要用户 profile、密码重置、邮件验证或人工审核后台；
- 需要多轮会话、流式 Chat 或个人化 RAG；
- 队列、数据库或向量库达到容量基线，单体模块成为明确瓶颈。

在这些信号出现前，继续添加更多业务阶段会扩大维护面，却不会提高当前系统的可靠性。
