# Stage 2 架构说明

## 1. 目标

Stage 2 在 Stage 1 API 上交付可部署的 React SPA：公开文章阅读、认证与会话恢复、作者写作和评论、管理员分类/标签管理，以及 Nginx/Compose 同源集成。

## 2. 前端架构

- React 19 + TypeScript + Vite 8。
- React Router 7 declarative SPA routing。
- `/api/v1` 相对路径访问后端，生产环境与页面同源。
- Access Token 只保存在内存；HttpOnly Refresh Cookie 由浏览器管理。
- 首次加载和 401 时使用 single-flight refresh，成功后重放原请求一次。
- 后端清洗过的 `content_html`/`body_html` 是唯一注入 DOM 的 HTML。

## 3. 页面

| 路径 | 能力 |
|---|---|
| `/` | 已发布文章网格、分页、空/错/加载状态 |
| `/posts/:slug` | 正文、作者、taxonomy、评论与回复 |
| `/login`, `/register` | 登录和注册 |
| `/write` | 登录用户新建文章；`?edit=slug` 编辑已知文章 |
| `/admin/taxonomy` | 管理员分类和标签 CRUD |
| `*` | SPA 404 |

当前后端没有“我的文章/草稿列表”API，因此 Stage 2 不虚构该页面；草稿通过创建响应保存的 slug 或已知 slug 编辑。

## 4. 部署

Frontend Dockerfile 在 Node 阶段执行 lint/test/build，再将 `dist` 放进独立 Nginx 容器。入口 Nginx：

- `/api/`、`/health/` 代理 Go API；
- `/assets/` 代理 hashed assets 并设置长期 immutable cache；
- 其他路径代理 Frontend，由 Frontend Nginx `try_files` 提供 SPA fallback。

外部仍只暴露 Proxy。Frontend 是 Compose 长期服务并有 HTTP healthcheck。

## 5. 验收

- `npm run lint`、`npm run test`、`npm run build` 通过，production dependencies 无 audit 漏洞。
- `make check` 同时覆盖后端和前端。
- Compose 静态配置、全栈 build 和 healthcheck 通过。
- 浏览器真实验证公开阅读、deep link、注册/登录/刷新恢复、写文章、评论 moderation 闭环和管理员 taxonomy。
- Stage 0/1 的 migration、health、限流和 Worker 不回归。

## 6. 边界

Frontend 不包含 AI/RAG、图片上传、人工评论审核、用户管理、profile 编辑、密码重置或完整草稿后台。这些能力需要对应后端契约后再交付。
