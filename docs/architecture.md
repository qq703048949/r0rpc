# R0RPC Demo Architecture

## Goal

This project is a Go implementation inspired by Sekiro RPC.

It keeps the core "server routes a request to an online device client" idea, and adds:

- admin login
- user/account management
- per-account access control
- request audit with filters by group/action/client
- raw request retention for 3 days
- aggregate metric retention for 30 days
- weekly charts and trends based on aggregated daily metrics
- group list with device health markers
- built-in multi-page management console
- Linux one-key Docker deployment
- WebSocket device transport for lower latency and lower idle overhead
- unified `Asia/Shanghai` time zone handling

## Core Design

The project is split into two layers:

1. RPC routing layer
   - Accepts invoke requests from the backend.
   - Chooses an online client by `group` and optional `clientId`.
   - Uses round-robin inside the same `group` when `clientId` is not specified.
   - Delivers the job to the client through a WebSocket transport.
   - Waits for the device result and returns it to the caller.

2. Governance layer
   - Handles admin login and JWT authentication.
   - Manages accounts and whether they can use RPC.
   - Persists request logs and aggregated metrics.
   - Exposes monitoring APIs and a built-in admin console.

## Retention Strategy

- `rpc_requests` keeps raw request details for 3 days.
- `device_daily_metrics` keeps aggregate device metrics for 30 days.
- `rpc_daily_metrics` keeps aggregate group/action/client metrics for 30 days.

This lets you inspect recent request detail while still keeping enough history for weekly or monthly trend analysis.
