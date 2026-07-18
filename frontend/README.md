# Blog Frontend

Stage 2 React SPA for the Blog project.

## Commands

```bash
npm ci
npm run dev
npm run lint
npm run test
npm run test:e2e
npm run build
```

The development server proxies `/api` and `/health` to `VITE_DEV_API_TARGET` (default `http://127.0.0.1:8081`). Production is built into the frontend Nginx container and exposed through the project proxy.

Access tokens remain in memory. Refresh tokens are HttpOnly cookies managed by the browser and scoped to `/api/v1/auth`.
