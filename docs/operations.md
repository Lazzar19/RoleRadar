# Operations

## Run one process

Start a single `app` container or one native binary per database. Multiple processes duplicate polls, queue work, and Telegram sends.

## Port bindings

In `compose.yaml`:

| Service | Host binding | Container port |
| --- | --- | --- |
| `app` | `127.0.0.1:8080` | `8080` |
| `postgres` | `127.0.0.1:5433` | `5432` |

The app listens on `0.0.0.0` inside the container; only localhost on the host can reach it unless you change the publish rule.

## Health checks

| Endpoint | Meaning |
| --- | --- |
| `GET /health` | Process is up (`{"status":"ok"}`) |
| `GET /ready` | PostgreSQL ping succeeded (`{"status":"ready"}`) |

Compose healthcheck uses `/ready` on port 8080 inside the app container.

## Logs

Structured JSON on stdout. Failed polls include `run_id`, `task_id`, and `source`. Exhausted tasks log at error with `task_id` and `type`. Failed notifications include `notification_id`. Logs omit mailbox URLs, API keys, and posting text.

## Configuration changes

Environment variables are read at process start. After changing `.env` or `compose.yaml` environment:

```sh
docker compose up -d --build
```

For a native binary, restart the process.

## Rotating secrets

1. Update `TELEGRAM_TOKEN`, `LLM_API_KEY`, `MAILBOX_URL`, or `POSTGRES_PASSWORD` in `.env`.
2. Recreate containers: `docker compose up -d --force-recreate`.
3. For Postgres password changes on an existing volume, alter the role inside the database; changing only `.env` without updating Postgres breaks `DATABASE_URL`.

## Telegram networking

The bot uses long polling. It does not need a public URL, webhook, or TLS certificate on your side.

## Candidate profile

The active profile lives in `candidate_profiles`. Telegram `/profile` displays it. There is no bot command to edit it; update the row in PostgreSQL (or use store APIs in tests). New profile versions trigger re-scoring for changed content hashes.

## Backups

See [backup-and-recovery.md](backup-and-recovery.md).

## Stop and teardown

```sh
docker compose stop
```

Remove containers and default networks but keep volumes:

```sh
docker compose down
```

Delete Postgres and backup data:

```sh
docker compose down -v
```

On PowerShell, the same `docker compose` commands apply; paths use backslashes only when required by the shell.