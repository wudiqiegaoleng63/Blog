#!/bin/sh
set -eu

: "${RESTORE_FILE:?RESTORE_FILE is required}"
: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${MYSQL_PASSWORD_FILE:?MYSQL_PASSWORD_FILE is required}"

case "$RESTORE_FILE" in
  *.sql.gz) ;;
  *) printf 'RESTORE_FILE must end in .sql.gz\n' >&2; exit 2 ;;
esac
test -r "$RESTORE_FILE"
test -r "$MYSQL_PASSWORD_FILE"
if [ "${CONFIRM_RESTORE:-}" != "restore-$MYSQL_DATABASE" ]; then
  printf 'Refusing restore. Set CONFIRM_RESTORE=restore-%s for this isolated target.\n' "$MYSQL_DATABASE" >&2
  exit 2
fi
if [ -f "$RESTORE_FILE.sha256" ]; then
  sha256sum -c "$RESTORE_FILE.sha256"
fi

umask 077
option_file=$(mktemp)
trap 'rm -f "$option_file"' EXIT INT TERM
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
gunzip -c "$RESTORE_FILE" | mysql --defaults-extra-file="$option_file" --protocol=TCP $port_args \
  -h "$MYSQL_HOST" -u "$MYSQL_USER" "$MYSQL_DATABASE"

# shellcheck disable=SC2086
mysql --defaults-extra-file="$option_file" --protocol=TCP $port_args -h "$MYSQL_HOST" -u "$MYSQL_USER" "$MYSQL_DATABASE" \
  --batch --skip-column-names -e '
    SELECT CONCAT("users=", COUNT(*)) FROM users;
    SELECT CONCAT("posts=", COUNT(*)) FROM posts;
    SELECT CONCAT("comments=", COUNT(*)) FROM comments;
    SELECT CONCAT("background_jobs=", COUNT(*)) FROM background_jobs;
    SELECT CONCAT("ai_documents=", COUNT(*)) FROM ai_documents;
    SELECT CONCAT("migration_version=", version, ",dirty=", dirty) FROM schema_migrations;
  '
