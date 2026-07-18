# Stage 5.1 架构说明：真实依赖闭环与上线验收

## 1. 判断

Stage 0–4 的核心产品能力已经交付，Stage 5 已补齐 MySQL/Redis 集成验收、认证闭环、双 Worker 并发领取、限流降级、Worker heartbeat、队列统计、CI 门禁和基础运维手册。

但当前仍有几项关键能力只完成了单元测试、契约测试或纸面运维说明，尚未在真实依赖和真实浏览器环境中验收。Stage 5.1 不新增博客业务功能，目标是把 AI、前端、可观测性、密钥和恢复流程从“代码存在”推进到“可重复验证、可观测、可恢复”。

## 2. 目标与边界

Stage 5.1 交付：

- 真实 MySQL + Redis + Milvus 的可重复集成环境；
- OpenAI-compatible Embedding/Chat mock contract，覆盖成功、重试、超时、429、5xx 和维度错误；
- 文章发布、更新、删除到索引任务、Milvus 向量和 RAG 授权过滤的闭环验收；
- Playwright 浏览器 smoke，覆盖公开阅读、登录恢复、写作、评论、管理员和 Ask 页面；
- API、Worker、AI 的正式运行时指标和最小告警阈值；
- 生产 secrets 的 Docker secrets 或平台 Secret Manager 迁移方案与轮换演练；
- MySQL 备份恢复自动化、Milvus 重建策略和 RPO/RTO 记录；
- 发布、回滚和故障注入演练记录。

Stage 5.1 不包含：

- 图片上传、私信、复杂 CMS、多人协作；
- 多轮会话、个人化 RAG 或微服务拆分；
- 在没有容量数据支持时进行分库分表、复杂缓存或过早扩容；
- 将 Milvus 作为业务数据唯一真相。文章与索引状态仍以 MySQL 为准，Milvus 是可重建派生索引。

## 3. 实施顺序

按风险和依赖顺序执行，不在真实 AI 闭环之前扩展新的业务 Stage：

| 顺序 | 项目 | 优先级 | 验收结果 |
|---|---|---:|---|
| 5.1-A | Milvus + Mock AI 集成 | P0 | ✅ 真实 Milvus 完成索引、替换、删除、检索；上游 mock contract 已验收 |
| 5.1-B | 浏览器 Smoke | P0 | ✅ Playwright 完成核心用户流程、deep link 和错误状态验收 |
| 5.1-C | 运行时指标 | P1 | ✅ API、限流、AI 上游和 Worker 队列指标及告警基线已定义 |
| 5.1-D | Secrets 迁移 | P1 | ✅ `*_FILE` 与 Compose secrets overlay 已交付；生产轮换仍按 runbook 执行 |
| 5.1-E | 备份恢复 | P0 | ✅ MySQL 备份/恢复/临时容器演练脚本与 Milvus 重建策略已交付 |
| 5.1-F | 发布与故障演练 | P1 | ✅ 发布、镜像回滚和依赖故障矩阵已写入 runbook |

本次先实施 5.1-A。后续每一项必须同时提交实现、自动化验收和文档更新，不以“本地手工成功”作为完成标准。

## 4. 5.1-A 真实 AI 集成验收

### 4.1 集成拓扑

```text
Go integration test
  ├── MySQL 8.4        domain data + ai_documents + background_jobs
  ├── Milvus 2.5       derived vector index
  └── httptest mock     /embeddings + /chat/completions
```

Embedding 和 Chat 使用固定 API key、固定模型名和确定性向量/回答。测试不得调用真实付费上游，不得把 API key 写入任务、数据库、日志或测试输出。

### 4.2 必须覆盖的场景

1. 重复执行 migration 后数据库保持 latest/clean；
2. 发布公开文章后，索引任务能够被领取并写入真实 Milvus；
3. RAG 通过 Embedding → Milvus Search → MySQL 当前版本校验 → Chat 返回答案；
4. 私有、未发布、已删除文章不能出现在最终来源；
5. 文章更新后旧 `content_version` 的向量不会被当作当前来源；
6. 文章删除会删除该文章的向量并将 `ai_documents` 标记为 `removed`；
7. 重复执行同一索引任务不会产生错误结果，向量集合保持单一当前版本；
8. Embedding 维度不匹配、Milvus 错误、上游超时、429、5xx 进入任务失败/重试路径；
9. mock server 收到的请求包含正确模型、维度、输入、消息和 Bearer token；
10. prompt injection 文本只能作为不可信文章数据，不能改变 RAG system prompt 的约束。

### 4.3 完成标准

- 本地 `make verify-integration` 可在临时 MySQL/Redis/Milvus 上重复运行并自动清理；
- CI 有独立的 AI integration job，失败时能区分 MySQL、Milvus、Embedding、Chat 和断言错误；
- 真实 Milvus REST 响应格式有自动化覆盖，不能只依赖 mock Milvus；
- 所有测试在无外部 AI 账号和无生产数据的环境运行；
- 集成测试失败时不得留下持久卷或泄露敏感配置。

## 5. 5.1-B 浏览器 Smoke 验收

核心流程：

```text
注册 → 登录 → 会话恢复 → 创建并发布文章 → 公开查看 → 评论 → Ask
```

同时覆盖：deep link 刷新、管理员 taxonomy、未登录保护、空状态、后端 4xx/5xx、AI 不可用提示和移动端基本布局。浏览器测试使用独立测试数据和 mock AI，不访问生产服务。

## 6. 5.1-C 运行时指标与告警

### API

- 请求总数、路由、状态码和延迟；
- 429 限流拒绝；
- MySQL/Redis 错误；
- Embedding、Milvus、Chat 上游调用延迟和错误类别。

### Worker

- 成功、失败、重试、dead-letter 数量；
- 任务处理耗时；
- stale-lock 回收数；
- pending/running/dead 数量和最老 pending 年龄；
- 最后成功轮询时间和 heartbeat 新鲜度。

### 最小告警基线

```text
oldest pending age > 5m       warning
pending age > 15m             critical
死信任务 > 0                  warning
Worker heartbeat > 90s        critical
API 5xx > 2%                  warning
AI upstream error > 10%       warning
MySQL readiness failure       critical
```

指标不能把文章正文、问题、回答、Cookie、JWT 或 API key 作为 label 或 value 输出。

## 7. 5.1-D Secrets 迁移

生产环境优先使用 Docker secrets；若部署平台不支持，则使用平台 Secret Manager 等价实现。覆盖 MySQL、Redis、JWT、Embedding 和 Chat 密钥。

验收要求：

- `docker inspect` 和普通容器环境查询不暴露密钥；
- 应用日志、任务 payload、数据库和前端 bundle 不包含密钥；
- 支持轮换窗口和失效旧密钥；
- 至少完成一次 JWT 或 AI key 轮换演练并记录结果。

## 8. 5.1-E 备份恢复

恢复真相分层：

1. MySQL 备份保存用户、文章、评论、任务和 `ai_documents`；
2. Milvus 视为可重建派生数据，不把它作为唯一业务备份；
3. 恢复 MySQL 后，使用 reindex/backfill 重新生成 Milvus collection；
4. 恢复后检查 migration version、数据数量、索引状态和公开 RAG 结果。

每次演练记录：备份时间、恢复时间、数据时间点、RPO、RTO、失败步骤和改进项。生产禁止自动执行破坏性 `down` migration。

## 9. 5.1-F 发布与故障演练

至少验证：

- 新镜像发布前质量门禁和集成门禁；
- migration up 成功后再切换 API/Worker；
- 应用镜像回滚不覆盖数据库数据；
- dirty migration 的诊断和人工处置；
- MySQL、Redis、Milvus、Embedding、Chat 分别不可用时的降级行为；
- Worker 停止、任务积压、stale-lock 和 dead-letter 的恢复流程。

## 10. 暂不进入 Stage 6 的条件

Stage 5.1 完成后，项目可进入小规模生产和真实用户反馈阶段。只有出现以下信号时才规划 Stage 6：

- 需要图片/媒体存储与 CDN；
- 需要密码找回、邮箱验证、用户 profile 或人工审核后台；
- 需要流式 Chat、多轮会话或个人化 RAG；
- MySQL、任务队列或 Milvus 达到容量基线；
- 单体模块成为明确的性能或团队协作瓶颈。

在这些信号出现前，继续增加业务阶段会扩大维护面，却不会优先提高可靠性。
