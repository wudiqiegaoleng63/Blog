# Blog. 📖✨

**Blog.** is a full-stack personal blog platform — semantic indexing, AI-powered question answering, and a warm, editorial reading experience, all shipping from a single `docker compose up`.

> 🎉 **Stage 4 complete!** RAG Q&A grounded in published stories, with linked sources. Every article you publish is automatically indexed and ready for natural-language questions.

[中文文档](README.zh-CN.md)

---

## 🧭 What it does

| Capability | Stage | Detail |
|---|---|---|
| 🔐 Auth | 1 | Register, login, refresh rotation, logout, `/me`; Argon2id + short-lived JWT + HttpOnly refresh cookie |
| ✍️ Blog domain | 1 | Posts, categories, tags, nested replies; Goldmark → Bluemonday sanitised HTML |
| 🛡️ Rate limiting | 1 | IP-gated register/login/refresh & user-gated comment writes; fail-open on Redis loss |
| ⚙️ Background worker | 1 | MySQL-backed job queue: claim, retry, dead-letter, stale-lock recovery; comment auto-moderation |
| 🎨 React SPA | 2 | Public reading, session restoration, writing/comments, admin taxonomy; same-origin served by Nginx |
| 🔮 Semantic index | 3 | OpenAI-compatible embeddings, paragraph-aware chunking, Milvus COSINE vectors, reindex backfill |
| 🤖 RAG Q&A | 4 | `/api/v1/ai/ask` — grounded answers with linked sources, MySQL-authorised retrieval, streaming-ready Chat |

---

## 🏗️ Stack

| Layer | What |
|---|---|
| 🖥️ Frontend | React 19, TypeScript, Vite 8, React Router 7 |
| ⚡ Backend | Go 1.25, Gin, GORM |
| 🔑 Auth | Argon2id, JWT (HS256), HttpOnly refresh cookie |
| ✏️ Content | Goldmark (Markdown), Bluemonday (HTML sanitizer) |
| 🗄️ Storage | MySQL 8.4 (domain data + job queue), Redis 7.4 (AOF + rate limiting) |
| 🔎 Vector DB | Milvus 2.5 (standalone), COSINE + AUTOINDEX |
| 🧠 AI | OpenAI-compatible Embedding & Chat endpoints |
| 🐳 Deploy | Docker Compose v2, Nginx 1.28, `golang-migrate` |

---

## 📁 Directory

```text
Blog/
├── frontend/              # React SPA, tests & production Dockerfile
├── backend/
│   ├── cmd/{api,worker,migrate}/
│   ├── internal/
│   │   ├── bootstrap/      # composition root
│   │   ├── config/          # env-based config with validation
│   │   ├── domain/          # framework-independent models
│   │   ├── modules/{auth,posts,comments,ai,operations}/
│   │   ├── platform/{cache,database,httpserver,ids,jobs,markdown,
│   │   │            migrations,observability,openaicompat,ratelimit}/
│   │   └── shared/apperr/   # stable error codes
│   ├── migrations/          # embedded SQL (0001 core, 0002 ai indexing)
│   └── Dockerfile
├── deploy/
│   ├── compose.yaml
│   ├── compose.dev.yaml
│   └── proxy/nginx.conf
├── docs/
│   ├── architecture/{stage-0,stage-1,stage-2,stage-3,stage-4}.md
│   └── adr/0001-modular-monolith.md
├── .env.example
└── Makefile
```

---

## 🚀 Quick start

You need Docker Engine, Docker Compose v2 and GNU Make. All commands from the repo root.

### 1. Create config

```bash
cp .env.example .env
```

### 2. Replace secrets

At minimum replace `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `REDIS_PASSWORD` and `JWT_SECRET` (≥ 32 bytes). Keep `MYSQL_DSN` in sync with the MySQL init values — it's a `go-sql-driver/mysql` DSN, not a `mysql://` URL.

### 3. Launch

```bash
make compose-config   # validate without printing secrets
make up               # build & start the full stack
make ps               # check service status
```

### 4. Verify

```bash
curl -i http://127.0.0.1:8080/                 # SPA
curl -i http://127.0.0.1:8080/health/live       # liveness
curl -i http://127.0.0.1:8080/health/ready      # readiness
curl -i http://127.0.0.1:8080/api/v1/posts      # public posts
```

The startup sequence: MySQL/Redis healthy → one-shot migration → API + Worker up → Proxy starts.

---

## 🔌 API overview

Base path: `/api/v1`

```text
🔐 Auth ──────────────────────────────────────────
POST   /auth/register          📝 public
POST   /auth/login             🔑 public
POST   /auth/refresh           🔄 refresh cookie
POST   /auth/logout            👋 idempotent
GET    /auth/me                👤 access token

📰 Posts ─────────────────────────────────────────
GET    /posts                  📚 public (published + public)
GET    /posts/:slug            📖 public or owner/admin
POST   /posts                  ✏️  authenticated
PUT    /posts/:slug             🖊️  author or admin
DELETE /posts/:slug             🗑️  author or admin

🏷️  Taxonomy ─────────────────────────────────────
GET    /categories             📂 public
POST   /categories             ➕ admin
PUT    /categories/:slug        ✏️  admin
DELETE /categories/:slug        🗑️  admin
GET    /tags                   🏷️  public
POST   /tags                   ➕ admin
PUT    /tags/:slug              ✏️  admin
DELETE /tags/:slug              🗑️  admin

💬 Comments ──────────────────────────────────────
GET    /posts/:slug/comments   💭 public (approved only)
POST   /posts/:slug/comments   ✍️  authenticated, user-rate-limited
PUT    /comments/:id            🖊️  author or admin
DELETE /comments/:id            🗑️  author or admin

🤖 AI ────────────────────────────────────────────
POST   /ai/ask                 🧠 public, IP-rate-limited, fail-closed
POST   /ai/reindex             🔄 admin only
```

New comments return `pending`; a `comment_moderation` job runs atomically in the same transaction, and the Worker approves sanitised non-empty content. This is not a human or AI safety review.

The RAG endpoint embeds your question, retrieves relevant chunks from Milvus, verifies every candidate against MySQL (published, public, matching content_version), assembles a bounded context, and returns an answer with linked sources.

---

## 🫀 Health & degradation

- `/health/live` — process is responsive.
- `/health/ready` — MySQL down → 503; Redis down → `degraded` but still 200 if MySQL is healthy.
- Auth/comment rate limits fail **open** (soft dependency); AI rate limits fail **closed** (cost-sensitive).
- The Worker doesn't listen on HTTP; Compose monitors it via `kill -0 1`.

---

## 🗄️ Migrations

SQL migrations are the sole source of schema truth. No GORM `AutoMigrate`.

```bash
make migrate-list
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate version
docker compose --env-file .env -f deploy/compose.yaml run --rm migrate migrate up
```

Rollback only in `APP_ENV=dev`:

```bash
docker compose --env-file .env -f deploy/compose.yaml run --rm -e APP_ENV=dev migrate migrate down
```

- `0001` — core schema (users, posts, comments, jobs, audit).
- `0002` — AI document index state (`ai_documents`).

Production and staging must never run `down`; investigate dirty state before touching the version table.

---

## 🛠️ Dev & quality

```bash
make help
make fmt              # gofmt
make test             # go test ./...
make vet              # go vet
make build            # api, worker, migrate → ./bin
make frontend-check   # lint + test + production build
make check            # all of the above
go -C backend test -race ./...
```

Dev port overlay:

```bash
make dev-up           # exposes 127.0.0.1:8081 (API), :3306 (MySQL), :6379 (Redis)
```

No source mounts, hot-reload or secret mutations.

---

## 🤖 AI configuration

Enable indexing and/or RAG independently:

```bash
AI_INDEXING_ENABLED=true    # Milvus + embedding + index jobs
AI_RAG_ENABLED=true         # also requires Chat model config
AI_ENABLED=true             # umbrella: enables both if individual flags absent
```

Both modes need: `AI_EMBEDDING_BASE_URL`, `AI_EMBEDDING_API_KEY`, `AI_EMBEDDING_MODEL`, `AI_EMBEDDING_DIMENSIONS`, `MILVUS_ADDR`, `MILVUS_COLLECTION_NAME`.

RAG additionally needs: `AI_CHAT_BASE_URL`, `AI_CHAT_API_KEY`, `AI_CHAT_MODEL`.

---

## 🔒 Security & deploy notes

- `.env` is gitignored; `.env.example` holds harmless placeholders only.
- Production requires `AUTH_COOKIE_SECURE=true`; terminate TLS at a trusted ingress.
- CORS origin must not be `*` when credentials are enabled.
- Set `HTTP_TRUSTED_PROXIES` correctly or IP-based rate limits will see the wrong address.
- Nginx and app body-size limits should be adjusted together; proxy default is 2 MiB.
- MySQL is the shared source of truth for domain data and jobs; Redis carries no durable tasks.
- Embedding/Chat API keys live only in environment variables; they never enter logs, job payloads, DB rows, or the frontend.
- RAG only retrieves from published public articles; MySQL re-verifies every candidate before answering.
- The Chat system prompt marks article content as untrusted data — not instructions.

---

## 🛑 Shutdown

```bash
make down             # stops containers, keeps MySQL/Redis/Milvus volumes
```

This preserves data volumes. To irreversibly delete everything:

```bash
docker compose --env-file .env -f deploy/compose.yaml down --volumes
```

---

## 🗺️ Roadmap

| Stage | What | Status |
|-------|------|--------|
| 0 | Config, logging, MySQL/Redis, health, Compose | ✅ |
| 1 | Auth, posts, taxonomy, comments, rate limiting, worker | ✅ |
| 2 | React SPA: reading, writing, session recovery, admin | ✅ |
| 3 | OpenAI-compatible Embedding, Milvus, article indexing | ✅ |
| 4 | Chat, semantic retrieval, grounded RAG Q&A | ✅ |

---

Built with ☕, Go, React, and a fondness for well-lit prose.
