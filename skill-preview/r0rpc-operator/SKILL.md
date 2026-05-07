---
name: r0rpc-operator
description: R0RPC project skill for Linux deployment, WebSocket client integration, RPC invoke routing, and troubleshooting. Use when working on this repo's deployment flow, client login and heartbeat, Java/Xposed/Python clients, group/clientId routing, request payloads, admin console, or retention/metrics issues.
---

# R0rpc Operator

## Overview

Use this skill to work on the R0RPC relay stack without re-deriving the project-specific rules each time.

## Workflow

1. Identify the task type first.
2. For deployment work, start with `deploy/linux/README.md` and the `.env.docker` flow.
3. For client work, check `clients/java/`, `clients/xposed-demo/`, or `examples/python/`.
4. For request routing, assume the current transport is WebSocket and `clientId` is optional unless targeting a specific device.
5. For debugging, use the references in `references/` before changing code.

## Operating Rules

- Prefer Linux-first deployment paths.
- Prefer WebSocket for the current production flow.
- Keep invoke payloads flat unless the repo already uses a different shape.
- Treat `group` as the normal routing key and `clientId` as an override.
- Check the admin console and request logs before guessing.

## References

- [Repo map](references/repo-map.md)
- [Troubleshooting](references/troubleshooting.md)
