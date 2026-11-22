# Development and testing

## Requirements

- Go **1.27.1** (`go.mod`)
- Docker and Compose for integration tests and the full stack
- PostgreSQL 17 for tests that use `DATABASE_URL`

## Setup

```sh
git clone https://github.com/Lazzar19/RoleRadar.git
cd RoleRadar
cp .env.example .env
```

For local Go runs, set `DATABASE_URL` to a reachable Postgres instance. Integration tests skip when `DATABASE_URL` is unset.

## Commands

```sh
go mod tidy
go build ./...
go vet ./...
go test ./...
```

Single package:

```sh
go test ./internal/match/...
```

Format check (matches CI):

```sh
gofmt -l .
```

Weight comparison against labeled feedback (needs DB and `PROPOSED_*` env):

```sh
PROPOSED_MATCH_THRESHOLD=80 go run . compare
```

## Compose dev loop

```sh
docker compose up --build
```

Postgres is on `127.0.0.1:5433`. App on `127.0.0.1:8080`.

## Adding a source adapter

1. **Domain** — Add a `SourceType` constant in `internal/domain/source.go` if the type string is new.
2. **Config** — Validate required JSON fields in `parseSources` in `internal/config/config.go`.
3. **Adapter** — Implement fetch in `internal/source/` returning `Snapshot` (see `greenhouse.go`, `lever.go`, `ashby.go`, `smartrecruiters.go`, `career.go`, `linkedin.go` / `mailbox.go`).
4. **Pipeline** — Wire the adapter in `internal/pipeline/poll.go` `runPoll` switch and in `main.go` `Handler(...)` construction.
5. **Schedule** — If the type needs a poll interval, extend `Scheduler.cadence` in `internal/schedule/schedule.go`.
6. **Tests** — Table-driven HTTP tests with `httptest` in `internal/source/*_test.go`; pipeline tests with a test database when behavior touches the queue.

Manual `/add` paths use `source.AcceptURL` and do not require a new poll adapter.

## Migrations

SQL files live in `internal/store/migrations/`. Goose runs them at startup. Add a new numbered file; do not edit applied migrations in place.

## CI

`.github/workflows/ci.yml` runs `go test`, `go vet`, `gofmt -l`, `docker compose config`, and `docker compose build`.