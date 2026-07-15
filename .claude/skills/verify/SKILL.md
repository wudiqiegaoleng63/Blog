---
name: verify
summary: Build and exercise the Stage 0 Compose stack through its public HTTP and migration surfaces.
---

# Verify Stage 0

Use a disposable environment file; `.env.example` is acceptable only for local disposable verification.

```bash
ENV_FILE=/absolute/path/to/test.env
PROJECT=blog-stage0-verify
COMPOSE='docker compose -p '"$PROJECT"' --env-file '"$ENV_FILE"' -f /home/lsy/桌面/Blog/deploy/compose.yaml'

$COMPOSE config --quiet
PROXY_PORT=18080 $COMPOSE up -d --build
$COMPOSE ps -a
```

Drive the public surface at `http://127.0.0.1:18080`:

- `/` returns the Stage 0 JSON.
- `/health/live` returns 200.
- `/health/ready` returns 200 with MySQL `ok` and Redis `ok`.
- `/api/v1` returns the API's unified 404.
- An unknown non-API path returns Nginx's local JSON 404.

Probe dependencies:

```bash
$COMPOSE stop redis
curl -i http://127.0.0.1:18080/health/ready  # 200, redis=degraded
$COMPOSE start redis
$COMPOSE stop mysql
curl -i http://127.0.0.1:18080/health/ready  # 503, mysql=down
$COMPOSE start mysql
```

Verify migration and shutdown:

```bash
$COMPOSE run --rm migrate migrate version
$COMPOSE run --rm migrate migrate up
$COMPOSE stop -t 20 api
$COMPOSE logs --no-color api
```

Always clean up containers after verification. Preserve named volumes unless the test explicitly owns disposable volumes:

```bash
$COMPOSE down
```

Gotcha: use a non-default `PROXY_PORT` when port 8080 may be occupied. If an initial proxy create fails, remove/recreate that container rather than `docker compose start`; a failed create may not have joined the Compose network.
