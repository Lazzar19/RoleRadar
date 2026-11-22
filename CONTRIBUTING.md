# Contributing

Thanks for improving RoleRadar. The project is small and self-hosted; keep changes focused and documented.

## Setup

Follow [docs/development.md](docs/development.md). Use `.env.example` as a template; never commit secrets.

## Checks before a pull request

```sh
go test ./...
go vet ./...
gofmt -l .   # must print nothing
docker compose config
```

## Adding a feature

- Match existing package layout and naming.
- Prefer tests that mirror `internal/source/*_test.go` or store integration tests with `DATABASE_URL`.
- Document new environment variables in `.env.example` and [docs/configuration.md](docs/configuration.md).
- Add migrations for schema changes; never rewrite old migration files.

## Source adapters

See [docs/development.md#adding-a-source-adapter](docs/development.md#adding-a-source-adapter).

## Pull requests

Use the PR template. Describe behavior changes and how you tested them. Link issues when applicable.

## Security

See [SECURITY.md](SECURITY.md).