# Sub2API Docker Image

Sub2API is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_HOST="host" \
  -e DATABASE_PORT="5432" \
  -e DATABASE_USER="user" \
  -e DATABASE_PASSWORD="pass" \
  -e DATABASE_DBNAME="sub2api" \
  -e REDIS_HOST="host" \
  -e REDIS_PORT="6379" \
  ghcr.io/xiaoli0412/sub2api:latest
```

## Docker Compose

```yaml
version: '3.8'

services:
  sub2api:
    image: ghcr.io/xiaoli0412/sub2api:latest
    ports:
      - "8080:8080"
    environment:
      - AUTO_SETUP=true
      - DATABASE_HOST=db
      - DATABASE_PORT=5432
      - DATABASE_USER=postgres
      - DATABASE_PASSWORD=postgres
      - DATABASE_DBNAME=sub2api
      - DATABASE_SSLMODE=disable
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_DB=0
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_healthy

  db:
    image: postgres:18-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Startup and Database Recovery

Sub2API runs database migrations while starting. PostgreSQL may still be
recovering briefly after a host or Docker daemon restart. The application
retries transient PostgreSQL startup and connection errors with bounded
exponential backoff, then continues startup when the database is ready.
Permanent errors such as invalid credentials, migration checksum mismatches,
SQL errors, and incompatible data fail immediately.

The Compose deployment also checks PostgreSQL readiness with both `pg_isready`
and a simple SQL query. `depends_on: condition: service_healthy` helps order a
fresh Compose start, but application-level retries are still required when
Docker restores existing containers after a host restart.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_HOST` | PostgreSQL host | Yes | - |
| `DATABASE_PORT` | PostgreSQL port | No | `5432` |
| `DATABASE_USER` | PostgreSQL user | Yes | - |
| `DATABASE_PASSWORD` | PostgreSQL password | Yes | - |
| `DATABASE_DBNAME` | PostgreSQL database name | Yes | - |
| `REDIS_HOST` | Redis host | Yes | - |
| `REDIS_PORT` | Redis port | No | `6379` |
| `REDIS_DB` | Redis database number | No | `0` |
| `SERVER_PORT` | Published host port (container listens on `8080`) | No | `8080` |
| `SERVER_MODE` | Application mode (`debug`/`release`) | No | `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `x.y.z` - Specific version
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

## Links

- [GitHub Repository](https://github.com/xiaoli0412/sub2api-pureai)
- [Documentation](https://github.com/xiaoli0412/sub2api-pureai#readme)
