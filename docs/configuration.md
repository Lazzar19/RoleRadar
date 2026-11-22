# Configuration

RoleRadar reads all settings from the environment. `DATABASE_URL` is required. Everything else has defaults in `internal/config/config.go`.

Copy [.env.example](../.env.example) to `.env` for Docker Compose. The app container receives variables from `compose.yaml`, which can forward values from `.env`.

## Summary

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | — | PostgreSQL connection string |
| `PORT` | no | `8080` | HTTP listen port (binds `0.0.0.0`) |
| `TELEGRAM_TOKEN` | for bot | empty | Bot token from BotFather |
| `TELEGRAM_USER_ID` | if token set | empty | Sole allowed Telegram user |
| `TELEGRAM_API_BASE` | no | `https://api.telegram.org` | Telegram Bot API base URL |
| `MAILBOX_URL` | if email source enabled | empty | IMAP URL for LinkedIn alerts |
| `SOURCES` | no | empty array | JSON list of job sources |
| `GREENHOUSE_API_BASE` | no | `https://boards-api.greenhouse.io` | Greenhouse API base |
| `LEVER_API_BASE` | no | `https://api.lever.co` | Lever API base |
| `GREENHOUSE_INTERVAL` | no | `1h` | Poll interval for greenhouse, lever, ashby, smartrecruiters |
| `GREENHOUSE_JITTER` | no | `10m` | Per-source jitter for those board types |
| `MAILBOX_INTERVAL` | no | `5m` | Email source poll interval |
| `MAILBOX_JITTER` | no | `1m` | Email source jitter |
| `CAREER_INTERVAL` | no | `6h` | Career JSON feed interval |
| `CAREER_JITTER` | no | `30m` | Career feed jitter |
| `WORKER_COUNT` | no | `1` | Concurrent queue workers in one process |
| `TASK_TIMEOUT` | no | `2m` | Claim lock / handler timeout |
| `TASK_MAX_ATTEMPTS` | no | `5` | Queue task attempts before failure |
| `WORKER_POLL_INTERVAL` | no | `2s` | Idle wait between claim attempts |
| `TASK_RETRY_BASE` | no | `5s` | Base delay for retries |
| `MATCH_WEIGHT_ROLE` | no | `30` | Max points for role match |
| `MATCH_WEIGHT_SKILLS` | no | `30` | Max points for skills |
| `MATCH_WEIGHT_EXPERIENCE` | no | `20` | Max points for experience |
| `MATCH_WEIGHT_LOCATION` | no | `10` | Max points for location |
| `MATCH_WEIGHT_COMPANY` | no | `10` | Max points for company |
| `MATCH_THRESHOLD` | no | `70` | Minimum overall score to notify |
| `LLM_API_KEY` | for extraction | empty | OpenAI-compatible API key |
| `LLM_API_BASE` | no | `https://api.openai.com/v1` | Chat completions base URL |
| `LLM_MODEL` | no | `gpt-4o-mini` | Model name |
| `EXTRACT_MIN_CHARS` | no | `80` | Minimum description length to call the model |
| `EXTRACT_MAX_CHARS` | no | `12000` | Cap on posting text sent to the model |
| `EXTRACT_DAILY_TOKENS` | no | `200000` | UTC daily token cap (`0` = no cap) |
| `NOTIFY_PREVIEW` | no | `true` | Deliver preview notifications |
| `NOTIFY_EVALUATED` | no | `true` | Deliver scored match notifications |
| `POSTGRES_PASSWORD` | Compose only | `scout` | Postgres password in `compose.yaml` |

## Compose-only notes

`compose.yaml` still uses the database user and database name `scout` for backward compatibility with existing volumes. Changing them requires a new database and restore. See [backup-and-recovery.md](backup-and-recovery.md).

`SOURCES` in the sample `compose.yaml` is an embedded JSON block. Override it in `.env` only if you replace the whole array.

## SOURCES JSON

`SOURCES` must be a JSON array. Each object needs `name` and `type`. Names must be unique.

`enabled` defaults to `true` when omitted. Set `"enabled": false` to keep a row without polling.

| type | Required fields | Optional fields | Notes |
| --- | --- | --- | --- |
| `greenhouse` | `board_token` | `enabled` | Public Greenhouse Job Board API |
| `lever` | `site` | `enabled` | Lever postings API for `site` |
| `ashby` | `site` | `enabled` | Ashby public job board for `site` |
| `smartrecruiters` | `site` | `enabled` | Company name on SmartRecruiters |
| `career` | `feed_url` | `enabled` | HTTPS JSON feed; see README |
| `email` | `secret_env` | `enabled` | `secret_env` names an env var (usually `MAILBOX_URL`) that must be set while enabled |

Example with every supported `type`:

```json
[
  {"name": "acme-gh", "type": "greenhouse", "board_token": "acme", "enabled": true},
  {"name": "acme-lv", "type": "lever", "site": "acme", "enabled": true},
  {"name": "acme-ab", "type": "ashby", "site": "acme", "enabled": true},
  {"name": "acme-sr", "type": "smartrecruiters", "site": "Acme Corp", "enabled": true},
  {
    "name": "acme-feed",
    "type": "career",
    "feed_url": "https://careers.example.com/jobs.json",
    "enabled": true
  },
  {"name": "linkedin-alerts", "type": "email", "secret_env": "MAILBOX_URL", "enabled": true}
]
```

Career `feed_url` must be `https` (or `http` to localhost in tests). Hosts `helloworld.rs` and `linkedin.com` are rejected for automatic feeds. Use Telegram `/add` for those links.

### Career feed body

```json
{
  "jobs": [
    {
      "id": "job-1",
      "title": "Software Engineer Intern",
      "url": "https://careers.example.com/jobs/1",
      "description": "Full posting text or HTML",
      "company": "Example Inc",
      "location": "Remote",
      "employment_type": "Internship",
      "posted_at": "2026-01-15T10:00:00Z"
    }
  ]
}
```

Each job needs `title`, `url`, and `description`. `posted_at` uses RFC3339 when present.

### Polling cadence

| Source types | Interval env | Default | Jitter env | Default |
| --- | --- | --- | --- | --- |
| `greenhouse`, `lever`, `ashby`, `smartrecruiters` | `GREENHOUSE_INTERVAL` | `1h` | `GREENHOUSE_JITTER` | `10m` |
| `email` | `MAILBOX_INTERVAL` | `5m` | `MAILBOX_JITTER` | `1m` |
| `career` | `CAREER_INTERVAL` | `6h` | `CAREER_JITTER` | `30m` |
| `manual` (from `/add`) | — | not scheduled | — | — |

The scheduler adds a stable per-source jitter so boards do not all fire at once.

## Weight comparison (CLI)

The `compare` subcommand reads labeled feedback from the database. Set at least one of:

- `PROPOSED_MATCH_WEIGHT_ROLE`
- `PROPOSED_MATCH_WEIGHT_SKILLS`
- `PROPOSED_MATCH_WEIGHT_EXPERIENCE`
- `PROPOSED_MATCH_WEIGHT_LOCATION`
- `PROPOSED_MATCH_WEIGHT_COMPANY`
- `PROPOSED_MATCH_THRESHOLD`

Unset variables keep the current `MATCH_*` values. See [README](../README.md#matching-and-weights).

## Privacy-related settings

See the Privacy section in [README](../README.md#privacy).