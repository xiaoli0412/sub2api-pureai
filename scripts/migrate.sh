#!/usr/bin/env bash
# =============================================================================
# Sub2API migration helper (export + import)
# =============================================================================
# Usage:
#   ./scripts/migrate.sh export [output-directory]
#   ./scripts/migrate.sh import [bundle.tar.gz]
#
# Run this on the host with PGHOST set to a host-reachable database address, or
# inside a container with PGHOST set to the Compose service name (for example,
# postgres). PGPORT must likewise be 5433 on the host by default and 5432 inside
# the Compose network.
# =============================================================================

set -Eeuo pipefail
umask 077

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()    { printf '%b\n' "${BLUE}[INFO]${NC} $1"; }
success() { printf '%b\n' "${GREEN}[SUCCESS]${NC} $1"; }
warn()    { printf '%b\n' "${YELLOW}[WARNING]${NC} $1"; }
error()   { printf '%b\n' "${RED}[ERROR]${NC} $1" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
if [ -n "${SUB2API_ENV_FILE:-}" ]; then
  ENV_FILE="$SUB2API_ENV_FILE"
elif [ -f .env ]; then
  ENV_FILE=".env"
elif [ -f "$REPO_ROOT/deploy/.env" ]; then
  ENV_FILE="$REPO_ROOT/deploy/.env"
else
  ENV_FILE=".env"
fi
if [ -d "$REPO_ROOT/deploy" ]; then
  ENV_TARGET_FILE="$REPO_ROOT/deploy/.env"
  if [ -f "$REPO_ROOT/deploy/config.yaml" ]; then
    CONFIG_FILE="$REPO_ROOT/deploy/config.yaml"
  else
    CONFIG_FILE="config.yaml"
  fi
else
  ENV_TARGET_FILE=".env"
  CONFIG_FILE="config.yaml"
fi
WORK_DIR=''

cleanup() {
  if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
    rm -rf -- "$WORK_DIR"
  fi
}
trap cleanup EXIT

# Read only connection keys from .env. Do not evaluate arbitrary shell content,
# and do not replace values supplied by the calling environment.
load_pg_env() {
  if [ -f "$ENV_FILE" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
      line="${line#"${line%%[![:space:]]*}"}"
      case "$line" in
        ''|'#'*) continue ;;
      esac
      if [[ "$line" =~ ^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=[[:space:]]*(.*)$ ]]; then
        key="${BASH_REMATCH[2]}"
        case "$key" in
          PGHOST|PGPORT|PGUSER|PGDATABASE|PGPASSWORD|PGPASSFILE|MIGRATION_DB_HOST|MIGRATION_DB_PORT|POSTGRES_HOST_PORT|DATABASE_HOST|DATABASE_PORT|DATABASE_USER|DATABASE_DBNAME|DATABASE_PASSWORD|POSTGRES_USER|POSTGRES_DB|POSTGRES_PASSWORD) ;;
          *) continue ;;
        esac
        if [[ ${!key+x} ]]; then
          continue
        fi
        value="${BASH_REMATCH[3]}"
        if [[ "$value" == '"'*'"' || "$value" == "'"*"'" ]]; then
          value="${value:1:${#value}-2}"
        fi
        export "$key=$value"
      fi
    done < "$ENV_FILE"
  fi

  : "${PGHOST:=${MIGRATION_DB_HOST:-127.0.0.1}}"
  : "${PGPORT:=${MIGRATION_DB_PORT:-${POSTGRES_HOST_PORT:-5433}}}"
  : "${PGUSER:=${DATABASE_USER:-${POSTGRES_USER:-postgres}}}"
  : "${PGDATABASE:=${DATABASE_DBNAME:-${POSTGRES_DB:-sub2api}}}"
  if [ -z "${PGPASSWORD:-}" ] && [ -n "${POSTGRES_PASSWORD:-}" ]; then
    PGPASSWORD="$POSTGRES_PASSWORD"
  elif [ -z "${PGPASSWORD:-}" ] && [ -n "${DATABASE_PASSWORD:-}" ]; then
    PGPASSWORD="$DATABASE_PASSWORD"
  fi
  : "${PGPASSWORD:=}"
  export PGHOST PGPORT PGUSER PGDATABASE PGPASSWORD
}

validate_pg_env() {
  [ -n "$PGHOST" ] || error "PGHOST/MIGRATION_DB_HOST is empty"
  [ -n "$PGUSER" ] || error "PGUSER/DATABASE_USER or POSTGRES_USER is required"
  [ -n "$PGDATABASE" ] || error "PGDATABASE/DATABASE_DBNAME or POSTGRES_DB is required"
  case "$PGPORT" in
    ''|*[!0-9]*) error "PGPORT/MIGRATION_DB_PORT must be a number" ;;
  esac
  if [ -z "${PGPASSWORD:-}" ] && [ -z "${PGPASSFILE:-}" ]; then
    error "set PGPASSWORD, PGPASSFILE, or a password variable before running the migration"
  fi
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || error "$1 not found; install PostgreSQL client tools and tar/gzip as applicable"
}

compress_file() {
  local input="$1"
  local output="$2"
  if command -v gzip >/dev/null 2>&1; then
    gzip -c "$input" > "$output"
  elif command -v pigz >/dev/null 2>&1; then
    pigz -c "$input" > "$output"
  else
    error "gzip or pigz not found; install a gzip-compatible compressor"
  fi
}

decompress_file() {
  local input="$1"
  local output="$2"
  if command -v gzip >/dev/null 2>&1; then
    gzip -dc "$input" > "$output"
  elif command -v pigz >/dev/null 2>&1; then
    pigz -dc "$input" > "$output"
  else
    error "gzip or pigz not found; install a gzip-compatible decompressor"
  fi
}

run_pg_dump() {
  local output="$1"
  if ! PGPASSWORD="$PGPASSWORD" pg_dump \
    -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" --no-password "$PGDATABASE" \
    --format=plain --no-owner --no-acl --clean --if-exists > "$output"; then
    error "pg_dump failed; check PGHOST=$PGHOST, PGPORT=$PGPORT, credentials, and database availability"
  fi
}

run_psql_restore() {
  local input="$1"
  if ! PGPASSWORD="$PGPASSWORD" psql \
    -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" --no-password "$PGDATABASE" \
    -v ON_ERROR_STOP=1 --single-transaction -f "$input"; then
    error "database restore failed; SQL execution stopped at the first error"
  fi
}

validate_archive() {
  local archive="$1"
  local members_file="$WORK_DIR/archive.members"
  local types_file="$WORK_DIR/archive.types"
  local member normalized mode
  local db_seen=0

  tar -tf "$archive" > "$members_file" || error "unable to read archive: $archive"
  tar -tvf "$archive" > "$types_file" || error "unable to inspect archive: $archive"

  while IFS= read -r member || [ -n "$member" ]; do
    normalized="${member#./}"
    case "$normalized" in
      ''|.) continue ;;
      /*|[A-Za-z]:/*|[A-Za-z]:\\*|..|../*|*/../*|*/..|*\\*)
        error "unsafe archive path: $member"
        ;;
      db.sql.gz|.env|config.yaml|README.md)
        [ "$normalized" = 'db.sql.gz' ] && db_seen=1
        ;;
      *)
        error "unexpected archive member: $member"
        ;;
    esac
  done < "$members_file"

  while IFS= read -r mode || [ -n "$mode" ]; do
    case "${mode:0:1}" in
      d|l|h|p|c|b|s) error "archive contains a non-regular file" ;;
    esac
  done < "$types_file"

  [ "$db_seen" -eq 1 ] || error "archive does not contain db.sql.gz"
}

cmd_export() {
  local out_dir="${1:-.}"
  local ts work_tar bundle work_db

  load_pg_env
  validate_pg_env
  require_command pg_dump
  require_command tar
  command -v gzip >/dev/null 2>&1 || command -v pigz >/dev/null 2>&1 || error "gzip or pigz not found"

  mkdir -p -- "$out_dir"
  ts="$(date +%Y%m%d_%H%M%S)"
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sub2api-migrate.XXXXXX")"
  work_db="$WORK_DIR/db.sql"
  work_tar="$WORK_DIR/migration-bundle-$ts.tar"
  bundle="$out_dir/migration-bundle-$ts.tar.gz"

  info "dumping database $PGDATABASE from $PGHOST ..."
  run_pg_dump "$work_db"
  compress_file "$work_db" "$WORK_DIR/db.sql.gz"

  [ -f "$ENV_FILE" ] && cp -- "$ENV_FILE" "$WORK_DIR/.env"
  [ -f "$CONFIG_FILE" ] && cp -- "$CONFIG_FILE" "$WORK_DIR/config.yaml"
  {
    printf '%s\n' '# Sub2API migration bundle'
    printf '%s\n' 'Restore requirements:'
    printf '%s\n' '  - PostgreSQL client tools (pg_dump/psql) and tar/gzip-compatible tools'
    printf '%s\n' '  - Set target PGHOST/PGPORT/PGUSER/PGDATABASE and password before import'
    printf '%s\n' '  - Run: ./scripts/migrate.sh import migration-bundle-<timestamp>.tar.gz'
    printf '%s\n' '  - Start: docker compose -f deploy/docker-compose.local.yml up -d'
  } > "$WORK_DIR/README.md"

  tar -cf "$work_tar" -C "$WORK_DIR" db.sql.gz README.md
  [ -f "$WORK_DIR/.env" ] && tar -rf "$work_tar" -C "$WORK_DIR" .env
  [ -f "$WORK_DIR/config.yaml" ] && tar -rf "$work_tar" -C "$WORK_DIR" config.yaml
  compress_file "$work_tar" "$bundle"
  success "exported bundle: $bundle"
}

cmd_import() {
  local bundle="${1:-}"
  local archive extract_dir sql_file

  [ -n "$bundle" ] || error "usage: $0 import <bundle.tar.gz>"
  [ -f "$bundle" ] || error "bundle not found: $bundle"
  load_pg_env
  validate_pg_env
  require_command psql
  require_command tar
  command -v gzip >/dev/null 2>&1 || command -v pigz >/dev/null 2>&1 || error "gzip or pigz not found"

  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sub2api-restore.XXXXXX")"
  archive="$WORK_DIR/migration-bundle.tar"
  extract_dir="$WORK_DIR/files"
  mkdir -p "$extract_dir"

  decompress_file "$bundle" "$archive"
  validate_archive "$archive"
  tar -xf "$archive" -C "$extract_dir"
  sql_file="$WORK_DIR/db.sql"
  [ -f "$extract_dir/db.sql.gz" ] || error "archive does not contain a regular db.sql.gz file"
  [ ! -L "$extract_dir/db.sql.gz" ] || error "archive member is a symlink: db.sql.gz"
  decompress_file "$extract_dir/db.sql.gz" "$sql_file"

  if [ -f "$extract_dir/.env" ]; then
    [ ! -L "$extract_dir/.env" ] || error "archive member is a symlink: .env"
    cp -- "$extract_dir/.env" "$ENV_TARGET_FILE"
    info "restored $ENV_TARGET_FILE"
  fi
  if [ -f "$extract_dir/config.yaml" ]; then
    [ ! -L "$extract_dir/config.yaml" ] || error "archive member is a symlink: config.yaml"
    cp -- "$extract_dir/config.yaml" "$CONFIG_FILE"
    info "restored $CONFIG_FILE"
  fi

  info "restoring database $PGDATABASE to $PGHOST ..."
  run_psql_restore "$sql_file"
  success "database restored"
  warn "now run: docker compose -f deploy/docker-compose.local.yml up -d"
}

case "${1:-}" in
  export) cmd_export "${2:-.}" ;;
  import) cmd_import "${2:-}" ;;
  *)
    printf '%s\n' "usage: $0 {export [out_dir] | import [bundle.tar.gz]}"
    exit 1
    ;;
esac
