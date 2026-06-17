# AGENTS.md

## Quick reference

```bash
make dev        # run server (hot-reload not configured)
make build      # compile binary to ./mealPlanner
make test       # run all tests
make lint       # golangci-lint (must be installed)
make swagger    # regenerate Swagger docs (after annotation changes)
```

## Architecture

Standard Go layout:

- `cmd/server/main.go` — entrypoint, wires Gin router + routes
- `internal/config/` — env-based config (PORT, DB_PATH, JWT_SECRET)
- `internal/db/` — SQLite connection + schema migrations
- `internal/model/` — data structs with Gin binding tags
- `internal/handler/` — HTTP handlers, each takes `*sql.DB`

## Key conventions

- **Database**: SQLite with WAL mode. DB file defaults to `mealplanner.db` in working dir.
- **Migrations**: Inline in `internal/db/sqlite.go` `migrate()` — add new tables/indexes there.
- **Routing**: All meal routes under `/meals` group defined in `cmd/server/main.go`.
- **Swagger**: Live docs at `GET /docs/index.html`. Annotations in handler comments; regenerate with `make swagger`.
- **Validation**: Uses Gin's `binding` struct tags (e.g., `binding:"required,oneof=breakfast lunch dinner snack"`).
- **Error responses**: Always `gin.H{"error": "message"}` with appropriate HTTP status.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server listen port |
| `DB_PATH` | `mealplanner.db` | SQLite file path |
| `JWT_SECRET` | `dev-secret-change-me` | Auth secret (not yet used) |

## Gotchas

- `mattn/go-sqlite3` requires CGO. Build with `CGO_ENABLED=1`.
- The binary must be built before `make run`; use `make dev` for quick iteration.
- `go-sqlite3` busy timeout is set to 5000ms in `db.Open()` — adjust if writes conflict.
