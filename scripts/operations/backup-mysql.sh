#!/bin/sh
set -eu

: "${BACKUP_DIR:?BACKUP_DIR is required}"
: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${MYSQL_PASSWORD_FILE:?MYSQL_PASSWORD_FILE is required}"

test -r "$MYSQL_PASSWORD_FILE"
umask 077
mkdir -p "$BACKUP_DIR"
timestamp=${BACKUP_TIMESTAMP:-$(date -u +%Y%m%dT%H%M%SZ)}
target="$BACKUP_DIR/blog-$timestamp.sql.gz"
tmp="$target.tmp"
option_file=$(mktemp)
trap 'rm -f "$option_file" "$tmp"' EXIT INT TERM
{
  printf '[client]\n'
  printf 'password=%s\n' "$(tr -d '\r\n' < "$MYSQL_PASSWORD_FILE")"
} > "$option_file"
chmod 600 "$option_file"

port_args=
if [ -n "${MYSQL_PORT:-}" ]; then
  port_args="--port=$MYSQL_PORT"
fi
# shellcheck disable=SC2086
mysqldump --defaults-extra-file="$option_file" --protocol=TCP $port_args --single-transaction \
  --routines --triggers --events --hex-blob --no-tablespaces --set-gtid-purged=OFF \
  -h "$MYSQL_HOST" -u "$MYSQL_USER" "$MYSQL_DATABASE" | gzip -9 > "$tmp"
test -s "$tmp"
mv "$tmp" "$target"
sha256sum "$target" > "$target.sha256"
printf '%s\n' "$target"
