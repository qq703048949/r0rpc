# Connection And Transport

## Client connection parameters

For the current project, a client only needs these fields to connect:

- server base URL, for example `http://YOUR_SERVER_IP:8080`
- `username`
- `password`
- `clientId`
- `group`

You do not need to provide a `server_id` to the client for the current single-node design.

## What `server_id` is for

`SERVER_ID` is a backend node label. It is useful when you later deploy multiple R0RPC nodes behind a load balancer and want to know which node handled a request.

So the split is:

- client connects by `base URL + account + client identity`
- server node identity is tracked separately by `SERVER_ID`

## Current transport

The current production transport in this repo is WebSocket.

Client flow:

1. call `POST /api/client/login`
2. read `token` and `wsUrl`
3. connect to `GET /api/client/ws?token=...`
4. keep the connection alive with heartbeat messages
5. receive jobs and send results back on the same socket

The old long-poll endpoints are still kept as fallback compatibility endpoints, but the main path is now WebSocket.

## Long-poll vs WebSocket

### Long-poll strengths

- easier to implement and debug
- pure HTTP, friendlier to many gateways and reverse proxies
- reconnect logic is simple because each poll is a fresh request

### Long-poll weaknesses

- more HTTP overhead
- server and client create many repeated requests while idle
- real-time latency is slightly worse than a hot WebSocket connection
- at large device counts it wastes more CPU and bandwidth

### WebSocket strengths

- lower idle overhead after the connection is established
- lower end-to-end latency for pushing jobs to devices
- better fit for large-scale always-online device fleets
- heartbeat and request/result traffic share one connection

### WebSocket weaknesses

- connection lifecycle is more complex
- reconnect, heartbeat, and broken half-open connection handling need extra care
- reverse proxy configuration needs to allow websocket upgrade

## Why WebSocket fits this project now

You asked for high performance and lower idle waste across many devices.

That makes WebSocket a better fit than long-poll because:

- each device holds one steady connection instead of repeatedly polling
- the server can push jobs immediately
- idle traffic drops significantly
- heartbeat and online-state freshness become easier to model

## Practical deployment note

If you deploy behind Nginx or another proxy, make sure websocket upgrade headers are forwarded correctly.

