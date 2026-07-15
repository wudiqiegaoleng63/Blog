# Stage 3 架构说明

## 1. 目标与边界

Stage 3 在 Stage 2 全栈博客上交付可独立启用的文章语义索引：OpenAI-compatible Embedding、Milvus 向量存储、可靠的文章索引任务和已有文章回填。Stage 3 不要求 Chat；关闭索引后 Stage 0–2 行为保持不变。

## 2. 能力开关与依赖

- `AI_INDEXING_ENABLED` 独立控制 Embedding、Milvus 和索引任务。
- `AI_RAG_ENABLED` 属于 Stage 4；启用 RAG 隐含要求索引配置完整。
- `AI_ENABLED` 仅作为兼容总开关；未显式设置两个细分开关时同时启用它们。
- MySQL 仍是核心硬依赖。Embedding/Milvus 是 AI 能力依赖，不改变核心 `/health/ready` 契约；故障通过任务重试、API 错误和日志暴露。
- SQL migration 仍是 schema 唯一来源，不使用 `AutoMigrate`。

## 3. 可索引文档与版本

只有同时满足以下条件的文章可检索：

- `status = published`
- `visibility = public`
- `deleted_at IS NULL`

`content_version` 是“可检索文档版本”，而不再仅表示 Markdown 正文版本。标题、摘要、正文、发布状态、可见性或 taxonomy 发生写入时均产生新版本。索引任务载荷包含文章 `public_id` 和版本；Worker 在外部调用前后重新读取文章，旧版本任务安全地变成 no-op。

MySQL 的 `ai_documents` 记录每篇文章最新成功索引的版本、chunk 数、状态和错误摘要。它是运营状态，不代替文章表或 Milvus。

## 4. Chunk 规则

索引文本按以下确定性规则生成：

1. 标题和非空摘要作为前缀。
2. Markdown 正文规范化换行和空白。
3. 以段落为优先边界，按 Unicode rune 切分为最多 1200 字符的 chunk。
4. 相邻 chunk 保留最多 160 字符重叠。
5. 空 chunk 丢弃；chunk 顺序从 0 开始。

稳定主键由 `post_public_id:content_version:chunk_index` 推导。Milvus 字段为 `chunk_id`、`post_id`、`post_slug`、`content_version`、`chunk_index`、`text` 与 `embedding`；向量使用 COSINE + AUTOINDEX。

## 5. 任务语义

任务类型为 `post_index`，动作是 `upsert` 或 `delete`：

- 新建、更新：业务数据与版本化任务在同一个 MySQL transaction 中提交。
- 删除：软删除与 delete 任务原子提交。
- 每个动作/版本使用永久唯一 dedup key，适配现有任务表语义。
- upsert 任务若发现文章已删除、非公开或非 published，则执行向量删除。
- 写入当前版本时先删除该文章旧向量，再批量 upsert 新 chunk。
- Worker 至少一次投递；重复执行、旧版本、并发执行均不得把非公开内容暴露给检索。
- Stage 4 检索必须再次以 MySQL 当前状态和版本做权威校验，因此残留旧向量不会被返回给用户。

索引任务使用现有重试/dead/stale-lock 机制。Embedding 的 408/409/429/5xx 和网络错误可重试；其他 4xx 直接返回错误并最终进入 dead。外部调用均受 context 和客户端 timeout 限制。

## 6. 回填

管理员通过 `POST /api/v1/ai/reindex` 为当前全部 published/public 文章补发版本化任务。重复调用依赖 dedup key 幂等，不直接在 API 请求内调用 Embedding 或 Milvus。

## 7. Milvus 生命周期

Worker 首次处理索引任务前执行幂等初始化：

1. 检查 collection；不存在则按配置维度创建。
2. 校验现有 collection 的向量维度和字段契约。
3. 创建 COSINE AUTOINDEX（若缺失）。
4. 加载 collection。

Embedding 返回的每个向量必须与 `AI_EMBEDDING_DIMENSIONS` 完全一致，否则任务失败，禁止写入部分数据。修改模型或维度需要新 collection、全量回填和部署切换，不能原地混用。

## 8. 安全与一致性

- API key 只从环境/Secret 读取，不进入日志、任务载荷、数据库或前端。
- 发送给 Embedding 供应商的内容仅来自公开文章；草稿和私有文章只触发删除。
- 检索永远以 MySQL 当前状态校验候选，Milvus metadata 不是授权真相。
- 供应商响应体有大小上限，错误只记录截断摘要。

## 9. 验收

- 配置测试证明 Stage 3 可在不配置 Chat 时独立启用。
- chunker 对 Unicode、长段落、重叠和稳定结果有单元测试。
- OpenAI-compatible Embedding 的请求、响应排序、维度校验、超时与重试有契约测试。
- 文章创建/更新/删除与任务入队保持事务原子性；重复和旧任务幂等。
- Milvus 初始化、替换、删除和 COSINE 检索在 Compose 环境完成真实闭环。
- 管理员回填可覆盖已有公开文章，非管理员被拒绝。
- `make check`、`go test -race ./...`、三个 Go 命令构建、Compose config/build 均通过，Stage 0–2 不回归。
