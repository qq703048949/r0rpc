# Troubleshooting

## If Invoke Fails

- `no_client`: no online client matched the `group` or `clientId`.
- `timeout`: a client was found, but the job did not finish before the deadline.
- `rejected`: the client or group was saturated.

## If Client Looks Online But Does Not Receive Jobs

- Confirm the client logged in with the right `group`.
- Confirm the caller did not pin the wrong `clientId`.
- Check WebSocket connectivity and heartbeat.
- Check the admin console for online state and last seen time.

## If Deployment Feels Off

- Start from `deploy/linux/README.md`.
- Check `.env.docker` values first.
- Verify MySQL and Redis mode before touching application code.

## If Client Integration Feels Off

- Keep the invoke payload flat.
- Use the current WebSocket flow instead of old long-poll paths.
- Check the client SDK examples before inventing a new shape.
