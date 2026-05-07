# Heartbeat Tuning 2026-03-31

## Goal

Make websocket client presence close to Sekiro-style behavior:

- if the server does not receive any heartbeat or other client message within 20 seconds, treat the client as disconnected
- reflect offline state in backend pages faster
- avoid false offline during long-running handler execution

## Design

Instead of relying on server-initiated websocket ping frames, keep the dedicated client heartbeat thread and tighten the timeout window:

- client sends app-level `heartbeat` every 5 seconds
- server websocket read deadline is 20 seconds
- any received client frame refreshes presence
- when read timeout or socket close happens, server immediately unregisters the client and marks the device offline
- stale-device cleanup now runs on a short interval derived from the offline grace window instead of once per minute

## Why not pure server ping

The current Java relay client processes business jobs and websocket reads on the same main socket loop.
A pure server-ping requirement can falsely mark a client offline when a handler is still executing but the independent heartbeat thread is healthy.
Using a separate heartbeat sender thread is safer for this codebase.

## Files changed

- `internal/config/config.go`
- `deploy/linux/.env.docker`
- `internal/app/app.go`
- `internal/web/client_ws.go`
- `internal/web/http.go`
- `internal/store/store.go`
- `clients/java/src/com/r0rpc/client/RelayClient.java`
- `examples/python/client_websocket.py`

## Active defaults after this change

- `DEVICE_OFFLINE_SECONDS=20`
- `HEARTBEAT_INTERVAL_SECONDS=5`
- `PRESENCE_FLUSH_SECONDS=5`

## Expected behavior

- abnormal disconnect: usually visible offline in about 20 seconds
- normal websocket close: usually visible offline almost immediately
- group/client realtime state and backend list state use the same second-level threshold
