# PureAI deployment upgrade guide

This guide covers a one-time switch from an existing upstream `Wei-Shaw/sub2api` deployment to the PureAI release identity `xiaoli0412/sub2api-pureai`. It describes the change only; do not run these steps without first taking backups and checking the release you intend to use.

## Before changing anything

1. Record the current application version and deployment method.
2. Back up the existing `.env` or `/etc/sub2api/config.yaml`.
3. Back up application data and databases. For Docker, back up `data/`, `postgres_data/`, and `redis_data/` (or export named volumes). For systemd, back up the configured data directory and database separately.
4. Confirm that the target PureAI release has the archive for your platform and a matching `checksums.txt` entry.
5. Keep the current upstream binary/image available until the new deployment has passed health, login, API, and background-job checks.

This is not a database rollback procedure. A binary or image rollback does not undo migrations, settings changes, issued keys, usage records, or other writes made by the newer application. Restore a database backup only after confirming the compatibility and data-loss implications.

## Systemd or binary deployment: one-time binary switch

An older upstream binary discovers releases under `Wei-Shaw/sub2api`. It cannot discover the PureAI fork by itself. The first switch must therefore be performed with the PureAI installer or by downloading a PureAI release manually.

After backing up configuration and data, run the PureAI installer in the existing installation directory:

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoli0412/sub2api-pureai/main/deploy/install.sh -o /tmp/sub2api-install.sh
sudo bash /tmp/sub2api-install.sh upgrade
rm -f /tmp/sub2api-install.sh
```

The installer keeps `/opt/sub2api`, the `sub2api` systemd service, configuration directory, and data directory unchanged. It downloads the release archive and checksum from the PureAI repository, refuses HTTP failures or missing/mismatched checksums, and retains the previous binary as `/opt/sub2api/sub2api.backup`. If download, verification, extraction, replacement, or startup fails, it attempts to restore the previous binary and service state.

Check the result before deleting the backup:

```bash
sudo systemctl status sub2api --no-pager
sudo journalctl -u sub2api -n 100 --no-pager
curl -fsS http://127.0.0.1:8080/health
```

After the first successful switch, future online updates and `install.sh upgrade` operations use PureAI releases. The administrator update UI also uses the PureAI repository for release metadata and rollback assets.

## Docker or Compose deployment: one-time image switch

An existing upstream Compose file points at the upstream image. Change only the application image reference while preserving the existing environment, service name, and data mounts/volumes:

```yaml
services:
  sub2api:
    image: ghcr.io/xiaoli0412/sub2api:latest
```

Prefer pinning the exact PureAI release tag or digest after selecting it. Do not rename the `sub2api` service and do not remove or recreate `data/`, `postgres_data/`, or `redis_data` unless you have a deliberate backup and migration plan. Existing container names and persistent mounts are part of the deployment contract.

For a local-directory Compose deployment, from the directory containing the existing `.env` and data directories:

```bash
# edit docker-compose.yml or docker-compose.local.yml first
# keep the existing .env and all persistent mounts

docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs --tail=100 sub2api
```

If the deployment was created by `docker-deploy.sh`, the downloaded local-directory file is named `docker-compose.yml`. A manual repository checkout retains the source filename `docker-compose.local.yml`; use the filename that actually exists, not both at once.

For Apple `container`, set `APPLE_CONTAINER_SUB2API_IMAGE=ghcr.io/xiaoli0412/sub2api:<tag>` in the existing `.env`, preserve the named volumes and service/container names, then follow the normal `apple-container.sh` lifecycle commands. Do not use a fresh `init` flow over an existing data directory.

## Compatibility and migration cautions

The current upstream baseline includes operational defaults that matter during an in-place update:

- API Key creation limits default to **200 active keys per user** and **60 creations per user per hour**. Deleting a key does not refund the hourly creation count.
- Redis in-flight balance reservation is enabled by default. The default reservation TTL is **900 seconds**, renewed every TTL/3 while a request runs; the default estimate is **8192 output tokens**, capped at **128000 output tokens** and **200000 input tokens**. An unpriced request or unavailable Redis fails open by default unless `fail_closed_on_unpriced` is enabled.
- Existing Redis and PostgreSQL data are reused. Ensure Redis has enough capacity and that all application instances use compatible configuration before switching a multi-instance deployment.
- Review streaming cancellation, billing failures, Redis outages, reservation release/renewal, and duplicate-release behavior after the update.

There is no promise of a full rollback by swapping a binary or image. Keep database and filesystem backups until the new release is validated, and use a database restore or a planned compensating migration only when you explicitly accept the resulting data loss or reconciliation work.
