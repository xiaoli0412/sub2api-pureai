# Database Migrations

## Overview

This directory contains SQL migration files for database schema changes. The migration system uses SHA256 checksums to ensure migration immutability and consistency across environments.

## Migration File Naming

Format: `NNN_description.sql`
- `NNN`: Sequential number (e.g., 001, 002, 003)
- `description`: Brief description in snake_case

Example: `017_add_gemini_tier_id.sql`

### `_notx.sql` 命名与执行语义（并发索引专用）

当迁移包含 `CREATE INDEX CONCURRENTLY` 或 `DROP INDEX CONCURRENTLY` 时，必须使用 `_notx.sql` 后缀，例如：

- `062_add_accounts_priority_indexes_notx.sql`
- `063_drop_legacy_indexes_notx.sql`

运行规则：

1. `*.sql`（不带 `_notx`）按事务执行。
2. `*_notx.sql` 按非事务执行，不会包裹在 `BEGIN/COMMIT` 中。
3. `*_notx.sql` 仅允许并发索引语句，不允许混入事务控制语句或其他 DDL/DML。

幂等要求（必须）：

- 创建索引：`CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`
- 删除索引：`DROP INDEX CONCURRENTLY IF EXISTS ...`

这样可以保证灾备重放、重复执行时不会因对象已存在/不存在而失败。

## Migration File Structure

This project uses a custom migration runner (`internal/repository/migrations_runner.go`) that executes the full SQL file content as-is.

- Regular migrations (`*.sql`): executed in a transaction.
- Non-transactional migrations (`*_notx.sql`): split by statement and executed without transaction (for `CONCURRENTLY`).

```sql
-- Forward-only migration (recommended)
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS example_column VARCHAR(100);
```

> ⚠️ Do **not** place executable "Down" SQL in the same file. The runner does not parse goose Up/Down sections and will execute all SQL statements in the file.

## Important Rules

### ⚠️ Immutability Principle

**Once a migration is applied to ANY environment (dev, staging, production), it MUST NOT be modified.**

Why?
- Each migration has a SHA256 checksum stored in the `schema_migrations` table
- Modifying an applied migration causes checksum mismatch errors
- Different environments would have inconsistent database states
- Breaks audit trail and reproducibility

### ✅ Correct Workflow

1. **Create new migration**
   ```bash
   # Create new file with next sequential number
   touch migrations/018_your_change.sql
   ```

2. **Write forward-only migration SQL**
   - Put only the intended schema change in the file
   - If rollback is needed, create a new migration file to revert

3. **Test locally**
   ```bash
   # Start the local Compose stack; AUTO_SETUP applies pending migrations.
   docker compose -f deploy/docker-compose.local.yml up -d

   # Check migration status through the host-published PostgreSQL port.
   export PGPASSWORD="$POSTGRES_PASSWORD"
   psql -h 127.0.0.1 -p "${POSTGRES_HOST_PORT:-5433}" \
     -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" \
     -c "SELECT filename, applied_at FROM schema_migrations ORDER BY applied_at DESC;"
   ```

4. **Commit and deploy**
   ```bash
   git add migrations/018_your_change.sql
   git commit -m "feat(db): add your change"
   ```

### ❌ What NOT to Do

- ❌ Modify an already-applied migration file
- ❌ Delete migration files
- ❌ Change migration file names
- ❌ Reorder migration numbers

### 🔧 If You Accidentally Modified an Applied Migration

**Error message:**
```
migration 017_add_gemini_tier_id.sql checksum mismatch (db=abc123... file=def456...)
```

**Solution:**
```bash
# 1. Find the original version
git log --oneline -- migrations/017_add_gemini_tier_id.sql

# 2. Revert to the commit when it was first applied
git checkout <commit-hash> -- migrations/017_add_gemini_tier_id.sql

# 3. Create a NEW migration for your changes
touch migrations/018_your_new_change.sql
```

## Running In Deployments

The application migration runner is embedded in the server binary. For Docker
Compose deployments, `AUTO_SETUP=true` is the supported startup path: on the
first start it reads the `DATABASE_*` settings, connects to PostgreSQL, applies
embedded migrations, and records checksums in `schema_migrations`. It also
performs setup tasks such as creating the initial admin account.

`AUTO_MIGRATE` is not currently read by the application and must not be used as
a migration switch. `SETUP_MIGRATION_TIMEOUT_SECONDS` controls the setup
migration timeout; `0` uses the built-in default.

For `deploy/docker-compose.local.yml`, the application connects to
`postgres:5432` over the private Compose network. Host-side `pg_dump`/`psql`
commands use `127.0.0.1:5433` by default (`POSTGRES_HOST_PORT` can change that
loopback port). A host cannot resolve the Compose service name `postgres`, and a
container cannot use the host's `127.0.0.1` for the database.

The repository migration runner is forward-only. A database restore is a
recovery operation, not a rollback mechanism for an already-applied migration.
Keep a verified backup before upgrades and retain the previous image until the
new schema has been validated.

### Bundle Export and Import

From a host with PostgreSQL client tools installed:

```bash
# Configure deploy/.env and set a real password locally; do not commit it.
export PGHOST=127.0.0.1 PGPORT=5433 PGUSER=sub2api PGDATABASE=sub2api
export PGPASSWORD='your-local-password'
./scripts/migrate.sh export ./backups
./scripts/migrate.sh import ./backups/migration-bundle-<timestamp>.tar.gz
```

On Windows, use `./scripts/migrate.ps1` with the same `PG*` variables. The
PowerShell script uses .NET gzip streams, so `gzip` and `gunzip` are not
required. Both scripts validate archive member paths before extraction and stop
`psql` at the first error with `ON_ERROR_STOP=1` and a single transaction.

For a script run inside the application container, set `PGHOST=postgres` and
`PGPORT=5432` explicitly. The scripts require PostgreSQL client tools and a
non-interactive password source (`PGPASSWORD` or `PGPASSFILE`).

## Migration System Details

- **Checksum Algorithm**: SHA256 of trimmed file content
- **Tracking Table**: `schema_migrations` (filename, checksum, applied_at)
- **Runner**: `internal/repository/migrations_runner.go`
- **Auto-run**: Enabled by the application's `AUTO_SETUP=true` startup path; `AUTO_MIGRATE` is not supported


1. **Keep migrations small and focused**
   - One logical change per migration
   - Easier to review and rollback

2. **Write reversible migrations**
   - Always provide a working Down migration
   - Test rollback before committing

3. **Use transactions**
   - Wrap DDL statements in transactions when possible
   - Ensures atomicity

4. **Add comments**
   - Explain WHY the change is needed
   - Document any special considerations

5. **Test in development first**
   - Start the deployment and let `AUTO_SETUP` apply pending migrations.
   - Verify data integrity and inspect `schema_migrations`.
   - For rollback or disaster recovery, restore a verified database backup; do not expect an image rollback to reverse schema changes.

## Example Migration

```sql
-- Add tier_id field to Gemini OAuth accounts for quota tracking
UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{tier_id}',
    '"LEGACY"',
    true
)
WHERE platform = 'gemini'
  AND type = 'oauth'
  AND credentials->>'tier_id' IS NULL;
```

## Troubleshooting

### Checksum Mismatch
See "If You Accidentally Modified an Applied Migration" above.

### Migration Failed
```bash
# Check migration status through the host-published PostgreSQL port.
export PGPASSWORD="$POSTGRES_PASSWORD"
psql -h 127.0.0.1 -p "${POSTGRES_HOST_PORT:-5433}" \
  -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" \
  -c "SELECT filename, applied_at FROM schema_migrations ORDER BY applied_at DESC;"
```

A failed migration should be fixed by creating a new forward migration or
restoring a verified backup. Do not mark a migration as applied without
reviewing the resulting schema.

### Need to Skip a Migration (Emergency Only)
```sql
-- DANGEROUS: Only use in development or with extreme caution
INSERT INTO schema_migrations (filename, checksum, applied_at)
VALUES ('NNN_migration.sql', 'calculated_checksum', NOW());
```

## References

- Migration runner: `internal/repository/migrations_runner.go`
- PostgreSQL docs: https://www.postgresql.org/docs/
