#!/bin/sh
set -eu

: "${RESTORE_FILE:?RESTORE_FILE is required}"
: "${MYSQL_PASSWORD_FILE:?MYSQL_PASSWORD_FILE is required}"

port=${RESTORE_MYSQL_PORT:-43306}
project=${RESTORE_PROJECT_NAME:-blog-restore-drill}
container="${project}-mysql"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
cleanup

password=$(tr -d '\r\n' < "$MYSQL_PASSWORD_FILE")
docker run -d --rm --name "$container" \
  -e MYSQL_DATABASE=blog_restore \
  -e MYSQL_USER=blog_restore \
  -e MYSQL_PASSWORD="$password" \
  -e MYSQL_ROOT_PASSWORD="restore-root-$password" \
  -p "127.0.0.1:$port:3306" \
  mysql:8.4.6 \
  --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci --default-time-zone=+00:00 >/dev/null

attempt=0
until docker exec "$container" mysqladmin ping -h 127.0.0.1 -ublog_restore -p"$password" --silent; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    printf 'restore MySQL did not become healthy\n' >&2
    exit 1
  fi
  sleep 2
done

CONFIRM_RESTORE=restore-blog_restore \
MYSQL_HOST=127.0.0.1 MYSQL_PORT="$port" MYSQL_USER=blog_restore MYSQL_DATABASE=blog_restore \
MYSQL_PASSWORD_FILE="$MYSQL_PASSWORD_FILE" RESTORE_FILE="$RESTORE_FILE" \
  "$(dirname "$0")/restore-mysql.sh"

printf 'Backup restore drill passed for %s\n' "$RESTORE_FILE"
