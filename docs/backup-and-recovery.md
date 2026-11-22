# Backup and recovery

## What runs in Compose

The `backup` service uses the `deploy/backup.sh` script:

- Once per successful run, writes `scout-YYYYMMDDTHHMMSSZ.dump` (custom `pg_dump` format) to the `backups` Docker volume.
- Deletes dumps older than 14 days after a successful dump.
- On failure, removes the partial file and retries in 5 minutes.

The database name, user, and dump filename prefix remain `scout` so existing deployments keep working.

## Copy dumps off the host

```sh
docker compose cp backup:/backups/scout-20260101T120000Z.dump ./scout.dump
```

Automate this copy; the volume survives container restarts but not `docker volume rm`.

## Restore on the same host

```sh
docker compose stop app backup
docker compose exec -T postgres pg_restore --clean --if-exists --no-owner -U scout -d scout < scout.dump
docker compose start app backup
```

Use a Postgres 17 client for dumps taken from the Compose `postgres:17` image. Mismatched major versions can fail restore.

## Restore on a new host

1. Install Docker and clone the repository.
2. Copy `.env` (or recreate secrets: `POSTGRES_PASSWORD`, `TELEGRAM_TOKEN`, `MAILBOX_URL`, `LLM_API_KEY`).
3. Start Postgres only: `docker compose up -d postgres` and wait for health.
4. Create an empty database (Compose does this on first start) or use a fresh volume.
5. `pg_restore` into `scout` as above.
6. `docker compose up -d --build`.

Queue tasks that were locked when the old process stopped become available again after `TASK_TIMEOUT` (see [troubleshooting.md](troubleshooting.md)).

## What backups contain

Dumps include job text, mailbox-derived URLs, profile JSON, feedback, and notification metadata. Treat files like database credentials. `.env` is not inside the volume; restore it separately.

## Test restores

Periodically restore a copy into a disposable database on a non-production machine and run `curl -fsS http://127.0.0.1:8080/ready`.