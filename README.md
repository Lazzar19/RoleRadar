# ScoutAI

ScoutAI watches internship and job sources and sends a Telegram message when a posting fits the candidate profile. One process polls the sources, scores each job, and delivers notifications. PostgreSQL stores the jobs, the queue, and the notifications.

## Architecture

The process applies database migrations, then starts the scheduler, the queue workers, the Telegram long poll, and the health server. The scheduler only enqueues work. A worker claims one task, fetches a board or mailbox, or scores one job, and then commits the result. Run a single copy of this process. A second copy schedules the same boards twice.

Greenhouse boards and LinkedIn alert mail are polled on a timer. `/add` stores a link without opening it. A preview has no description and is not sent to a model. A full description is scored with rules. When `LLM_API_KEY` is set, a long description is sent to one OpenAI-compatible model first. The model returns quoted facts. The rules score those facts. The model does not send a notification.

`GET /health` reports that the process is up. `GET /ready` pings PostgreSQL.

## Configure

The process reads configuration from the environment. `DATABASE_URL` is required. The other secrets stay empty until that feature is turned on.

| Variable | Required | Default |
| --- | --- | --- |
| `DATABASE_URL` | yes | |
| `PORT` | no | `8080` |
| `TELEGRAM_TOKEN` | only for Telegram | empty, bot stays off |
| `TELEGRAM_USER_ID` | when the token is set | |
| `MAILBOX_URL` | when an enabled email source names it in `secret_env` | empty |
| `SOURCES` | no | empty JSON array |
| `LLM_API_KEY` | only for extraction | empty, rules score the description |
| `LLM_API_BASE` | no | `https://api.openai.com/v1` |
| `LLM_MODEL` | no | `gpt-4o-mini` |
| `EXTRACT_MIN_CHARS` | no | `80` |
| `EXTRACT_MAX_CHARS` | no | `12000` |
| `EXTRACT_DAILY_TOKENS` | no | `200000` (`0` removes the daily cap) |
| `POSTGRES_PASSWORD` | for Compose | `scout` on a local machine |

Put secrets in an untracked `.env` next to `compose.yaml`. Do not commit that file. `POSTGRES_PASSWORD=scout` is a local fallback. Set a different password before anyone else can reach the host.

`SOURCES` is a JSON array. A Greenhouse source needs `board_token`. An email source needs `secret_env`, and that variable must be set while the source is enabled.

## Run

On a machine that stays up while the development computer is off:

```sh
docker compose up -d --build
```

The application port is published on `127.0.0.1:8080` only. Telegram long polling does not need a public webhook or a TLS certificate. Check the process with:

```sh
curl -fsS http://127.0.0.1:8080/ready
```

Migrations run inside the process, with a database lock, before it accepts work. `depends_on` only waits until PostgreSQL accepts connections. It is not the migration lock.

Logs go to stdout as JSON. A failed source run includes `run_id`, `task_id`, and `source`. A task that has used its attempts includes `task_id` and `type` at error level. A notification that cannot be delivered includes `notification_id`. The logs do not include mailbox URLs, message bodies, API keys, or posting text.

## Backups

The `backup` service writes one custom-format `pg_dump` per day into the `backups` volume and deletes dumps older than 14 days. A failed dump is removed and tried again five minutes later.

Copy a dump off the host on a schedule you control. The volume survives a container restart. It does not survive deletion of the volume.

## Recover

Stop the app and the backup loop, restore one dump into the existing database, then start them again:

```sh
docker compose cp backup:/backups/scout-TIMESTAMP.dump ./scout.dump
docker compose stop app backup
docker compose exec -T postgres pg_restore --clean --if-exists --no-owner -U scout -d scout < scout.dump
docker compose start app backup
```

Replace `scout-TIMESTAMP.dump` with the file you want. `pg_restore` replaces objects inside the `scout` database. Queue rows that were locked by a stopped process are returned to the queue after the task timeout.

Prometheus is not part of this setup.
