# Repo Map

## Main Entry Points

- `cmd/server/main.go`: server bootstrap, schema init, admin bootstrap, background jobs.
- `internal/app/app.go`: login, invoke routing, persistence, presence, metrics.
- `internal/web/`: HTTP and WebSocket handlers.

## Key Paths

- `deploy/linux/`: one-click deployment flow and Docker compose setup.
- `clients/java/`: Java relay client SDK.
- `clients/xposed-demo/`: Android/Xposed demo client.
- `examples/python/`: Python invoke and client demos.
- `docs/`: architecture and transport notes.

## Useful Defaults

- Current production transport: WebSocket.
- Deployment focus: Linux + Docker.
- Common routing key: `group`.
- Specific target override: `clientId`.
