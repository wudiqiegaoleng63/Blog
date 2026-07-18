# Blog. 📖✨

**Blog.** 是一个全栈个人博客平台 —— 语义索引、AI 问答、温暖而有编辑感的阅读体验，一次 `docker compose up` 即可全部交付。

> 🎉 **阶段 4 完成！** 基于已发布文章的 RAG 问答，带可点击来源。每篇发布的文章自动索引，随时用自然语言提问。

[English docs](README.md)

---

## 🧭 现在能做什么

| 能力 | 阶段 | 细节 |
|---|---|---|
| 🔐 认证 | 1 | 注册、登录、Refresh 轮换、注销、`/me`；Argon2id + 短期 JWT + HttpOnly Refresh Cookie |
| ✍️ 博客领域 | 1 | 文章、分类、标签、一层回复评论；Goldmark 渲染 → Bluemonday 清洗 HTML |
| 🛡️ 限流 | 1 | 注册/登录/刷新按 IP 限流，评论写入按用户限流；Redis 故障时 fail-open |
| ⚙️ 后台任务 | 1 | MySQL 任务队列：领取、重试、死信、stale-lock 恢复；评论自动 moderation |
| 🎨 React SPA | 2 | 公开阅读、会话恢复、写作/评论、管理员 taxonomy；Nginx 同源交付 |
| 🔮 语义索引 | 3 | OpenAI-compatible Embedding、段落感知分块、Milvus COSINE 向量、回填索引 |
| 🤖 RAG 问答 | 4 | `/api/v1/ai/ask` —— 基于文章的答案，带可点击来源链接，MySQL 实时校验 |

---

## 🏗️ 技术栈

| 层 | 技术 |
|---|---|
| 🖥️ 前端 | React 19, TypeScript, Vite 8, React Router 7 |
| ⚡ 后端 | Go 1.25, Gin, GORM |
| 🔑 认证 | Argon2id, JWT (HS256), HttpOnly Refresh Cookie |
| ✏️ 内容 | Goldmark (Markdown), Bluemonday (HTML 清洗) |
| 🗄️ 存储 | MySQL 8.4（领域数据 + 任务队列）, Redis 7.4（AOF + 限流） |
| 🔎 向量库 | Milvus 2.5（独立模式）, COSINE + AUTOINDEX |
| 🧠 AI | OpenAI-compatible Embedding 与 Chat 接口 |
| 🐳 部署 | Docker Compose v2, Nginx 1.28, `golang-migrate` |

---

## 📁 目录

```text
Blog/
├── frontend/              # React SPA、测试与 production Dockerfile
├── backend/
│   ├── cmd/{api,worker,migrate}/
│   ├── internal/
│   │   ├── bootstrap/      # 依赖装配
│   │   ├── config/          # 环境变量配置与校验
│   │   ├── domain/          # 框架无关的领域模型
│   │   ├── modules/{auth,posts,comments,ai,operations}/
│   │   ├── platform/{cache,database,httpserver,ids,jobs,markdown,
│   │   │            migrations,observability,openaicompat,ratelimit}/
│   │   └── shared/apperr/   # 稳定错误码
│   ├── migrations/          # 内嵌 SQL（0001 核心表, 0002 AI 索引）
│   └── Dockerfile
├── deploy/
│   ├── compose.yaml
│   ├── compose.dev.yaml
│   └── proxy/nginx.conf
├── docs/
│   ├── architecture/{stage-0,stage-1,stage-2,stage-3,stage-4,stage-5}.md
│   ├── operations-runbook.md
│   └── adr/0001-modular-monolith.md
├── .env.example
└── Makefile
```

---

## 🚀 快速开始

需要 Docker Engine、Docker Compose v2 和 GNU Make。所有命令从项目根目录执行。

### 1. 创建配置

```bash
cp .env.example .env
```

### 2. 替换密钥

至少替换 `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`REDIS_PASSWORD` 和 `JWT_SECRET`（至少 32 字节）。确保 `MYSQL_DSN` 与 MySQL 初始化值一致——它是 `go-sql-driver/mysql` DSN，不是 `mysql://` URL。

### 3. 启动

```bash
make compose-config   # 静默验证 Compose 配置
make up               # 构建并启动全栈
make ps               # 查看服务状态
```

### 4. 验证

```bash
curl -i http://127.0.0.1:8080/                 # SPA 页面
curl -i http://127.0.0.1:8080/health/live       # 存活检查
curl -i http://127.0.0.1:8080/health/ready      # 就绪检查
curl -i http://127.0.0.1:8080/api/v1/posts      # 公开文章
```

启动顺序：MySQL/Redis 健康 → 一次性 migration → API + Worker 启动 → Proxy 上线。

---

## 🔌 API 概览

基础路径：`/api/v1`

```text
🔐 认证 ──────────────────────────────────────────
POST   /auth/register          📝 公开
POST   /auth/login             🔑 公开
POST   /auth/refresh           🔄 Refresh Cookie
POST   /auth/logout            👋 幂等
GET    /auth/me                👤 Access Token

📰 文章 ──────────────────────────────────────────
GET    /posts                  📚 公开（仅 published + public）
GET    /posts/:slug            📖 公开或作者/管理员
POST   /posts                  ✏️  登录用户
PUT    /posts/:slug             🖊️  作者或管理员
DELETE /posts/:slug             🗑️  作者或管理员

🏷️  分类与标签 ───────────────────────────────────
GET    /categories             📂 公开
POST   /categories             ➕ 管理员
PUT    /categories/:slug        ✏️  管理员
DELETE /categories/:slug        🗑️  管理员
GET    /tags                   🏷️  公开
POST   /tags                   ➕ 管理员
PUT    /tags/:slug              ✏️  管理员
DELETE /tags/:slug              🗑️  管理员

💬 评论 ──────────────────────────────────────────
GET    /posts/:slug/comments   💭 公开（仅 approved）
POST   /posts/:slug/comments   ✍️  登录用户，按用户限流
PUT    /comments/:id            🖊️  作者或管理员
DELETE /comments/:id            🗑️  作者或管理员

🤖 AI ────────────────────────────────────────────
POST   /ai/ask                 🧠 公开，按 IP 限流，fail-closed
POST   /ai/reindex             🔄 管理员
```

新评论先返回 `pending`；同一事务写入 moderation 任务，Worker 对清洗后非空内容自动批准，不等同于内容安全审核。

RAG 端点会将你的问题向量化、从 Milvus 检索相关 chunk、逐一用 MySQL 校验候选（published、public、content_version 一致），组装有界上下文后返回带来源链接的答案。

---

## 🫀 健康与降级

- `/health/live` — 进程存活即可。
- `/health/ready` — MySQL 故障返回 503；Redis 故障标记 `degraded`，MySQL 正常时仍返回 200。
- 认证与评论限流 fail-**open**（软依赖）；AI 限流 fail-**closed**（成本敏感）。
- Worker 不监听 HTTP；Compose 通过 heartbeat 文件检测 Worker 是否仍在轮询，避免只检查 PID 导致假健康。Worker 周期性记录队列 pending/running/dead/completed 与最老任务年龄。

---

## 🗄️ 数据迁移

SQL migration 是 schema 唯一来源，不使用 GORM `AutoMigrate`。

```bash
make migrate-list
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate version
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate up
```

回滚仅限 `APP_ENV=dev`：

```bash
docker compose --env-file .env -f deploy/compose.yaml run --rm -e APP_ENV=dev migrate migrate down
```

- `0001` — 核心表（用户、文章、评论、任务、审计）。
- `0002` — AI 索引状态表（`ai_documents`）。

生产和 staging 禁止 `down`；dirty 状态必须先调查失败 SQL。

---

## 🛠️ 开发与质量检查

```bash
make help
make fmt              # gofmt
make test             # go test ./...
make vet              # go vet
make build            # api, worker, migrate → ./bin
make frontend-check   # lint + test + production build
make check            # 以上全部
make verify           # check + race detector + Compose 校验
make verify-integration # 临时 MySQL/Redis：migration、认证、限流、双 Worker SKIP LOCKED
```

开发端口覆盖：

```bash
make dev-up           # 额外发布 127.0.0.1:8081 (API), :3306 (MySQL), :6379 (Redis)
```

不挂载源码、不热重载、不修改 Secret。

---

## 🤖 AI 配置

索引和 RAG 可独立启用：

```bash
AI_INDEXING_ENABLED=true    # Milvus + Embedding + 索引任务
AI_RAG_ENABLED=true         # 还需配置 Chat 模型
AI_ENABLED=true             # 总开关：未设置细分开关时同时启用两者
```

两种模式均需配置：`AI_EMBEDDING_BASE_URL`、`AI_EMBEDDING_API_KEY`、`AI_EMBEDDING_MODEL`、`AI_EMBEDDING_DIMENSIONS`、`MILVUS_ADDR`、`MILVUS_COLLECTION_NAME`。

RAG 额外需要：`AI_CHAT_BASE_URL`、`AI_CHAT_API_KEY`、`AI_CHAT_MODEL`。

---

## 🔒 安全与部署注意

- `.env` 已忽略；`.env.example` 只能保存无害占位值。
- 生产必须 `AUTH_COOKIE_SECURE=true`；应在可信入口终止 TLS。
- 启用 credentials 时 CORS origin 不能是 `*`。
- 正确设置 `HTTP_TRUSTED_PROXIES`，否则按 IP 限流可能使用错误地址。
- Nginx 与应用请求体上限应一起调整；代理默认上限为 2 MiB。
- MySQL 是领域数据和任务的共同真相；Redis 不承载持久任务。
- Embedding/Chat API key 只存在于环境变量中，不进入日志、任务载荷、数据库或前端。
- RAG 只检索 published + public 的文章；每次回答前用 MySQL 重新校验每个候选。
- Chat system prompt 将文章内容标记为不可信数据——而非指令。

---

## 🛑 停机

```bash
make down             # 停止容器，保留 MySQL/Redis/Milvus 数据卷
```

保留数据卷。以下命令会不可恢复地删除所有数据：

```bash
docker compose --env-file .env -f deploy/compose.yaml down --volumes
```

---

## 🗺️ 路线图

| 阶段 | 内容 | 状态 |
|------|------|------|
| 0 | 配置、日志、MySQL/Redis、健康检查、Compose | ✅ |
| 1 | 认证、文章、分类/标签、评论、限流、Worker | ✅ |
| 2 | React SPA：阅读、写作、会话恢复、管理员 | ✅ |
| 3 | OpenAI-compatible Embedding、Milvus、文章索引 | ✅ |
| 4 | Chat、语义检索、基于文章的 RAG 问答 | ✅ |
| 5 | 生产化加固、集成验收、可观测性、备份恢复 | 🚧 |
| 5.1 | 真实 Milvus/AI 集成、浏览器 Smoke、指标、Secrets、恢复与发布演练 | 🚧 |

---

以 ☕、Go、React 和对优美文字的偏爱构建。
