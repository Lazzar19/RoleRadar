# Troubleshooting

| Symptom | Likely cause | Diagnostic |
| --- | --- | --- |
| `/ready` returns 503 or fails | Postgres down or wrong `DATABASE_URL` | `docker compose ps`; `docker compose logs postgres`; `curl -v http://127.0.0.1:8080/ready` |
| `/health` unreachable | App not running or wrong port | `docker compose ps app`; `docker compose logs app`; confirm `127.0.0.1:8080` in compose |
| Bot silent | Missing token, wrong user, or notifications disabled | Check `TELEGRAM_TOKEN` and `TELEGRAM_USER_ID`; `NOTIFY_PREVIEW` / `NOTIFY_EVALUATED`; logs for `telegram`; message `/status` |
| Source returns nothing | Disabled source, API error, or empty board | `/sources` in Telegram; logs with `source` and `run_id`; test API URL manually |
| No match notifications | Score below `MATCH_THRESHOLD` or incomplete job | `/status`; compare with `go run . compare`; confirm job `CompletenessFull` |
| Email source fails at startup | Enabled `email` source without secret env value | Config error names `secret_env`; set `MAILBOX_URL` (or the named variable) |
| Tasks repeat after restart | Expected: expired task locks recovered | `TASK_TIMEOUT` in config; worker calls `RecoverExpired` on start |
| LLM never runs | No key or description too short | Set `LLM_API_KEY`; check length against `EXTRACT_MIN_CHARS`; daily cap `EXTRACT_DAILY_TOKENS` |
| Career feed rejected | Bad URL or disallowed host | Config error on startup; host must not be `linkedin.com` or `helloworld.rs` |
| `/add` rejected | Host not on allowlist | Allowed: `linkedin.com`, `helloworld.rs`, and board hosts for enabled greenhouse/lever/ashby/smartrecruiters sources |

## Queue lock behavior

Workers claim tasks with a timeout. If a process dies mid-task, `RecoverExpired` returns the task to the queue when `TASK_TIMEOUT` elapses, until `TASK_MAX_ATTEMPTS` is reached.

## Database connection from the host

Compose maps Postgres to port **5433**:

```sh
psql "postgres://scout:scout@127.0.0.1:5433/scout?sslmode=disable"
```

Use the password from your `.env`.