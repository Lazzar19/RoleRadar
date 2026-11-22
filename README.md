# RoleRadar

[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://img.shields.io/badge/CI-GitHub_Actions-blue)](.github/workflows/ci.yml)

**Self-hosted job alerts that score postings against your profile and ping you on Telegram.**

RoleRadar is a single Go process with PostgreSQL. It polls public job boards and optional mail feeds, scores each posting with configurable rules (and optional LLM fact extraction), and sends Telegram messages to one user. Status: **self-hosted**, **single-user**, **experimental** — built for personal use, not a managed SaaS.

### Supported sources

| Mechanism | Types |
| --- | --- |
| Scheduled polling | `greenhouse`, `lever`, `ashby`, `smartrecruiters`, `career` (HTTPS JSON feed) |
| LinkedIn job alert email | `email` (IMAP via `MAILBOX_URL`) |
| Manual link entry | Telegram `/add` (stores URL without fetching; includes LinkedIn and sites without a public feed) |

Some career sites have no public API or permitted automated feed. RoleRadar does not crawl them. Use `/add` and paste the posting text in a reply.

## Features

| Feature | Benefit |
| --- | --- |
| Multi-source ingestion | One queue for Greenhouse, Lever, Ashby, SmartRecruiters, JSON feeds, and mail |
| Candidate profile in Postgres | Versioned preferences drive scoring |
| Rule-based matching | Works without `LLM_API_KEY`; weighted role, skills, experience, location, company |
| Optional LLM extraction | OpenAI-compatible API returns quoted facts; rules still decide the score |
| Daily token cap | `EXTRACT_DAILY_TOKENS` limits model spend per UTC day |
| Telegram workflow | Previews for link-only jobs; scored messages with inline feedback |
| Feedback labels | Interesting, not relevant, save for later, already applied — stored only, no auto-tuning |
| Postgres task queue | Retries, lock recovery, single-process workers |
| Compose backups | Daily `pg_dump` to a volume, 14-day retention |

## How it works

1. Migrations run, then sources from `SOURCES` upsert into the database.
2. The scheduler enqueues board and mailbox polls on configured intervals.
3. Workers fetch postings and upsert jobs; full descriptions enqueue match tasks.
4. Matching optionally extracts facts, then applies rule weights and `MATCH_THRESHOLD`.
5. Notifications queue for Telegram; the bot long-polls and delivers to `TELEGRAM_USER_ID`.
6. You reply with text on previews or tap feedback buttons; labels feed `go run . compare`.

Details: [docs/architecture.md](docs/architecture.md).

## Quick start

### Prerequisites

- Docker with Compose v2
- A Telegram bot token ([@BotFather](https://t.me/BotFather))
- Your numeric Telegram user id (e.g. from `@userinfobot`)

### Linux / macOS

```sh
git clone https://github.com/Lazzar19/RoleRadar.git
cd RoleRadar
cp .env.example .env
```

Edit `.env`: set `TELEGRAM_TOKEN`, `TELEGRAM_USER_ID`, and a non-default `POSTGRES_PASSWORD` if the host is shared.

```sh
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
docker compose logs -f app
```

Message your bot on Telegram, run `/help`, try `/add` with an allowed URL. Insert an active row in `candidate_profiles` before match notifications make sense (see [docs/operations.md](docs/operations.md#candidate-profile)).

Stop:

```sh
docker compose stop
```

Teardown (keeps volumes):

```sh
docker compose down
```

### Windows (PowerShell)

```powershell
git clone https://github.com/Lazzar19/RoleRadar.git
cd RoleRadar
Copy-Item .env.example .env
```

Edit `.env`, then:

```powershell
docker compose up -d --build
curl.exe -fsS http://127.0.0.1:8080/ready
```

## Telegram setup

1. Create a bot with BotFather; copy the token into `TELEGRAM_TOKEN`.
2. Set `TELEGRAM_USER_ID` to your numeric id. Only that user and chat receive commands and notifications.
3. Optional: set `TELEGRAM_API_BASE` if you use a Telegram API proxy (default `https://api.telegram.org`).

### Commands

| Command | Action |
| --- | --- |
| `/start`, `/help` | Short usage text |
| `/profile` | Show active candidate profile |
| `/status` | Open jobs, pending notifications, profile name |
| `/sources` | List configured sources and last check time |
| `/add <url>` | Save one allowed http(s) URL as a preview job (no HTTP fetch) |
| `/feedback …` | Record feedback when replying to a notification message |

Reply to a **preview** message with the full posting text to score it.

### Buttons

Interesting, Not relevant, Save for later, Already applied — each stores one feedback row.

### Preview vs evaluated

- **Preview** (`NOTIFY_PREVIEW`, default on): sent when the job has no full description (e.g. after `/add`). HTML body from `PreviewText` in `internal/notify/message.go`.
- **Evaluated** (`NOTIFY_EVALUATED`, default on): sent when rules produce a score at or above `MATCH_THRESHOLD`.

Example evaluated message (format only; not a live screenshot):

```text
MATCH 82/100

<title as link>
Company: …
Location: …

<posting summary>
Posted: YYYY-MM-DD

Source: …
Open posting
```

<!-- Screenshot placeholder: add docs/images/telegram-notification.png when available -->

Long polling is used; no webhook or TLS setup is required on your server.

## Configuration

Environment variables are listed in [.env.example](.env.example). Full tables and `SOURCES` schema: [docs/configuration.md](docs/configuration.md).

| Group | Key variables |
| --- | --- |
| Database | `DATABASE_URL`, `POSTGRES_PASSWORD` (Compose) |
| HTTP | `PORT` (default `8080`) |
| Telegram | `TELEGRAM_TOKEN`, `TELEGRAM_USER_ID`, `TELEGRAM_API_BASE` |
| Sources | `SOURCES`, `MAILBOX_URL`, `*_INTERVAL`, `*_JITTER`, API bases |
| Workers | `WORKER_COUNT`, `TASK_TIMEOUT`, `TASK_MAX_ATTEMPTS`, … |
| Matching | `MATCH_WEIGHT_*`, `MATCH_THRESHOLD` |
| LLM | `LLM_API_KEY`, `LLM_API_BASE`, `LLM_MODEL`, `EXTRACT_*` |
| Notifications | `NOTIFY_PREVIEW`, `NOTIFY_EVALUATED` |

`.env` is gitignored; never commit secrets.

## Sources

Configure boards in `SOURCES` JSON. Polling intervals:

| Types | Default interval |
| --- | --- |
| `greenhouse`, `lever`, `ashby`, `smartrecruiters` | 1h (+ jitter) |
| `email` | 5m |
| `career` | 6h |

`/add` uses the internal `manual` source and is not polled. See [docs/configuration.md](docs/configuration.md#sources-json) for field requirements and a full JSON example.

## Matching and weights

Default weights (each criterion is 0–max, summed to 100 max): role 30, skills 30, experience 20, location 10, company 10. Default `MATCH_THRESHOLD` is **70**.

Feedback does not change weights. To experiment:

```sh
PROPOSED_MATCH_THRESHOLD=80 go run . compare
```

The command prints how many labeled jobs current vs proposed rules would notify. It does not write to the database. After reviewing output, set `MATCH_*` env vars and restart.

## Operations

- Run **one** app instance per database.
- Published ports: app `127.0.0.1:8080`, Postgres `127.0.0.1:5433`.
- Default `POSTGRES_PASSWORD=scout` is for local use only.

[docs/operations.md](docs/operations.md) · [docs/backup-and-recovery.md](docs/backup-and-recovery.md)

## Development and testing

Go 1.27.1, `go test ./...`, `go vet ./...`, `gofmt`, Compose build — see [docs/development.md](docs/development.md).

## Troubleshooting

[docs/troubleshooting.md](docs/troubleshooting.md) — `/ready`, bot delivery, sources, mailbox secrets, queue locks.

## Privacy

| Topic | Behavior |
| --- | --- |
| When the LLM runs | Only if `LLM_API_KEY` is set and description length ≥ `EXTRACT_MIN_CHARS` (and ≤ cap) |
| What is sent | Title plus capped posting text; system prompt from `prompts/extract-posting-v1.md` — no profile or threshold |
| Token cap | `EXTRACT_DAILY_TOKENS` per UTC day in Postgres; `0` disables the cap |
| Stored data | Jobs, evaluations, optional extraction JSON, notifications, feedback, profiles, queue, raw mail metadata |
| Mailbox | IMAP credentials from `MAILBOX_URL` or `secret_env`; message bodies processed for LinkedIn alerts |
| Backups | Custom-format dumps include the same sensitive data as the database |

## Project status

| Implemented | Not implemented |
| --- | --- |
| Board and mail polling, `/add`, scoring, Telegram, feedback, compare CLI, Compose backups | Multi-user auth, web UI, profile editing in Telegram, automatic weight learning from feedback, Prometheus metrics |

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md)

## License

[MIT](LICENSE) — Copyright 2026 Lazar Nikolic