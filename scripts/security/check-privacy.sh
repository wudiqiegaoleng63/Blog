#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"

failed=0
report() {
  printf 'privacy-check: %s\n' "$*" >&2
  failed=1
}

candidate_files=$(mktemp)
scan_files=$(mktemp)
findings=$(mktemp)
trap 'rm -f "$candidate_files" "$scan_files" "$findings"' EXIT INT TERM
# Include tracked and visible untracked files so the check catches a sensitive
# artifact before its first commit, not only after it enters the index.
git ls-files --cached --others --exclude-standard | sort -u > "$candidate_files"

tracked_sensitive=$(grep -E '(^|/)(\.env($|\.)|secrets?|credentials?|backups?|dumps?|data)(/|$)|(^|/)(id_rsa|id_dsa|id_ecdsa|id_ed25519)(\.|$)|\.(pem|key|private|p12|pfx|jks|keystore|kdb|sqlite|sqlite3|db|dump|backup|bak|sql\.gz|sql\.zst)$' "$candidate_files" || true)
if [ -n "$tracked_sensitive" ]; then
  unexpected=$(printf '%s\n' "$tracked_sensitive" | grep -Ev '(^|/)\.env\.example$|^deploy/compose\.secrets\.yaml$' || true)
  if [ -n "$unexpected" ]; then
    report "sensitive-looking files are tracked:\n$unexpected"
  fi
fi

tracked_ignored=$(git ls-files -ci --exclude-standard || true)
if [ -n "$tracked_ignored" ]; then
  report "ignored files are already tracked:\n$tracked_ignored"
fi

for sample in \
  .env \
  .env.production \
  secrets/jwt_secret \
  .secrets/mysql_dsn \
  credentials/cloud.json \
  backups/blog.sql.gz \
  dumps/mysql.dump \
  data/blog.sqlite3 \
  server.pem \
  id_ed25519; do
  if ! git check-ignore -q --no-index "$sample"; then
    report ".gitignore does not protect $sample"
  fi
done
if git check-ignore -q --no-index .env.example; then
  report ".env.example must remain trackable"
fi

if [ -f .env ]; then
  mode=$(stat -c '%a' .env 2>/dev/null || true)
  case "$mode" in
    *00) ;;
    *) report "local .env permissions are $mode; use chmod 600 .env" ;;
  esac
fi

grep -Ev '^(frontend/package-lock\.json|scripts/security/check-privacy\.sh)$' "$candidate_files" > "$scan_files"

if [ -s "$scan_files" ]; then
  # Definite private keys and commonly structured provider credentials.
  xargs grep -nH -I -E \
    'BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{50,}|xox[baprs]-[A-Za-z0-9-]{20,}|AIza[0-9A-Za-z_-]{30,}|sk-(proj-)?[A-Za-z0-9_-]{20,}' \
    < "$scan_files" > "$findings" 2>/dev/null || true
  if [ -s "$findings" ]; then
    report "credential-like content detected:\n$(cat "$findings")"
  fi

  # Secret assignments are allowed only when clearly marked as test/example placeholders.
  xargs grep -nH -I -E \
    '(PASSWORD|SECRET|API_KEY|ACCESS_KEY|PRIVATE_KEY|TOKEN)[[:space:]]*[:=][[:space:]]*[^$<{[:space:]]{8,}' \
    < "$scan_files" > "$findings" 2>/dev/null || true
  if [ -s "$findings" ]; then
    suspicious=$(grep -Evi 'development-only|integration-only|placeholder|change-me|example|invalid|fake|dummy|test-only|smoke|restore-root|<[^>]+>' "$findings" || true)
    if [ -n "$suspicious" ]; then
      report "unmarked secret assignments detected:\n$suspicious"
    fi
  fi
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi
printf 'Privacy check passed: tracked files, ignore rules, local permissions, and credential patterns are clean.\n'
